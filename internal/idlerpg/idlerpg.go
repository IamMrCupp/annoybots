// Package idlerpg is the classic IRC idle RPG: you "play" by being present and
// QUIET in a channel — every tick you idle, you advance toward the next level;
// talking or leaving sets you back. State lives in the F3 store, so a character
// persists across restarts and is shared across the botnet. Opt-in via !rpg.
package idlerpg

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/event"
	"github.com/IamMrCupp/annoybots/internal/state"
)

// itemSlots are the 10 equipment slots (idlerpg.net). Each holds a level; the sum
// is the character's power, used by battles. Stored as "item:<slot>" sheet fields.
var itemSlots = []string{"ring", "amulet", "charm", "weapon", "helm", "tunic", "gloves", "leggings", "shield", "boots"}

func itemField(slot string) string { return "item:" + slot }

const (
	growth      = 1.16           // time-to-level multiplier per level
	ttlCap      = 30 * 24 * 3600 // never make a single level take more than 30 days
	talkCharCap = 40             // a single message's character contribution is capped here
)

// player is who's currently online (present + enrolled), where to announce, and
// the canonical character key (an account if linked, else the network identity).
type player struct {
	network string
	nick    string
	channel string
	key     string // resolved character key (state is stored under this)
}

// Resolver maps a sender's (network, account, nick) to their canonical player key
// — the account/identity system, so a linked person is one character everywhere.
type Resolver func(network, account, nick string) string

// Manager runs the game for one bot.
type Manager struct {
	store    state.Store
	out      engine.Sender
	resolve  Resolver
	log      *slog.Logger
	interval time.Duration
	baseTTL  time.Duration

	questInterval time.Duration
	questDuration time.Duration
	now           func() time.Time // injectable clock (quest deadlines); time.Now in prod

	authz  func(engine.Message) bool // is the sender a bot admin? (nil = no admin verbs)
	paused atomic.Bool               // when set, Tick freezes the whole game

	mu     sync.Mutex
	online map[string]player // network|nick -> online player

	qmu   sync.Mutex
	quest *quest // the active quest, nil when none

	bmu  sync.Mutex
	boss *worldBoss // the active world-boss raid, nil when none

	emu    sync.Mutex
	wevent *worldEvent // the active realm-wide world event, nil when none

	wmu     sync.Mutex
	weather *weatherState // per-biome sky, rotated periodically

	rmu sync.Mutex
	rng *rand.Rand
}

// New builds a Manager. interval is the tick period; baseTTL is the level 0→1 time.
// questInterval/questDuration tune the quest cadence (zero → sane defaults).
// resolve maps senders to canonical player keys (cross-network when linked).
func New(store state.Store, out engine.Sender, resolve Resolver, interval, baseTTL, questInterval, questDuration time.Duration, log *slog.Logger) *Manager {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if baseTTL <= 0 {
		baseTTL = 10 * time.Minute
	}
	if questInterval <= 0 {
		questInterval = defaultQuestInterval
	}
	if questDuration <= 0 {
		questDuration = defaultQuestDuration
	}
	if resolve == nil {
		resolve = func(network, _, nick string) string { return strings.ToLower(network) + "|" + strings.ToLower(nick) }
	}
	m := &Manager{
		store: store, out: out, resolve: resolve, log: log,
		interval: interval, baseTTL: baseTTL,
		questInterval: questInterval, questDuration: questDuration,
		now:    time.Now,
		online: map[string]player{},
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	m.loadQuest(context.Background())
	m.loadBoss(context.Background())
	m.loadWorldEvent(context.Background())
	m.loadWeather(context.Background())
	return m
}

// roll returns a value in [0,n). Guarded so battles/events can roll concurrently.
func (m *Manager) roll(n int) int {
	if n <= 0 {
		return 0
	}
	m.rmu.Lock()
	defer m.rmu.Unlock()
	return m.rng.Intn(n)
}

// Interval is how often the caller should invoke Tick.
func (m *Manager) Interval() time.Duration { return m.interval }

// SetAuthz wires an admin-authorization predicate, enabling the privileged !rpg
// verbs (pause/resume/push/hog). Without it those commands are inert.
func (m *Manager) SetAuthz(fn func(engine.Message) bool) { m.authz = fn }

// authorized reports whether the sender may run privileged !rpg commands.
func (m *Manager) authorized(msg engine.Message) bool {
	return m.authz != nil && m.authz(msg)
}

func okey(network, nick string) string { return strings.ToLower(network) + "|" + strings.ToLower(nick) }

// sheetKey/boardKey are keyed by the resolved player key (account or identity),
// so linked players share one character + one global leaderboard across networks.
func sheetKey(player string) string { return "rpg:p:" + player }
func boardKey() string              { return "rpg:lvl" }

// Handle processes a channel message: !rpg commands, and a talk-penalty for
// anyone currently in the game. Returns true only when it consumed a !rpg command.
func (m *Manager) Handle(msg engine.Message) bool {
	if msg.Private || msg.Text == "" {
		return false
	}
	fields := strings.Fields(msg.Text)
	if strings.ToLower(fields[0]) == "!rpg" {
		m.command(msg, fields)
		return true
	}
	// Talking is the cardinal sin — penalize online players. The penalty notice
	// goes privately to the offender (classic IdleRPG behaviour) so it doesn't
	// spam the channel; talking during a quest still blows the whole quest.
	if p, ok := m.onlinePlayer(msg.Network, msg.Nick); ok {
		ctx := context.Background()
		sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
		pen := m.talkPenalty(sheet["level"], int64(len(msg.Text)))
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", pen)
		m.notify(msg.Network, msg.Nick, fmt.Sprintf("🤐 quiet — you broke the silence; +%s added to your next level.", dur(pen)))
		m.questViolation(ctx, p.key, p.nick, "spoke up")
	}
	return false
}

// notify sends a private message to a nick — a true IRC NOTICE when the transport
// supports one (classic IdleRPG behaviour), else a plain private message.
func (m *Manager) notify(network, nick, text string) {
	if n, ok := m.out.(engine.Noticer); ok {
		n.Notice(network, nick, text)
		return
	}
	m.out.Say(network, nick, text)
}

// talkPenalty is the time (seconds) added to a talker's clock for a message of
// msgLen chars at the given level. It scales with the level's own duration, so it
// bites proportionally at every level — a few seconds for a newbie, hours for a
// legend — floored and capped to a small slice of the level so a single slip
// stings without instantly delevelling you.
func (m *Manager) talkPenalty(level, msgLen int64) int64 {
	if msgLen > talkCharCap {
		msgLen = talkCharCap
	}
	if msgLen < 1 {
		msgLen = 1
	}
	pen := msgLen * (1 + level/4)
	levelTime := m.ttlFor(level)
	if floor := levelTime / 300; pen < floor { // at least ~0.33% of the level
		pen = floor
	}
	if ceil := levelTime / 20; ceil > 0 && pen > ceil { // never more than ~5% of the level
		pen = ceil
	}
	if pen < 1 {
		pen = 1
	}
	return pen
}

func (m *Manager) command(msg engine.Message, fields []string) {
	if len(fields) >= 2 {
		switch strings.ToLower(fields[1]) {
		case "top":
			arg := ""
			if len(fields) >= 3 {
				arg = fields[2]
			}
			m.out.Say(msg.Network, msg.Channel, m.topBoard(arg))
			return
		case "items", "gear":
			m.out.Say(msg.Network, msg.Channel, m.items(msg))
			return
		case "stash":
			m.stash(msg, fields)
			return
		case "equip":
			m.equip(msg, fields)
			return
		case "align":
			m.setAlign(msg, fields)
			return
		case "class":
			m.setClass(msg, fields)
			return
		case "race":
			m.setRace(msg, fields)
			return
		case "help", "commands", "?":
			m.help(msg)
			return
		case "info":
			m.out.Say(msg.Network, msg.Channel, m.info())
			return
		case "who", "online":
			m.out.Say(msg.Network, msg.Channel, m.who(msg))
			return
		case "weather", "sky":
			m.out.Say(msg.Network, msg.Channel, m.weatherStatus())
			return
		case "dungeon", "delve":
			m.out.Say(msg.Network, msg.Channel, m.dungeonStatus(msg))
			return
		case "quest":
			m.out.Say(msg.Network, msg.Channel, m.questStatus())
			return
		case "status":
			m.out.Say(msg.Network, msg.Channel, m.status(msg, fields))
			return
		case "sheet", "stats":
			m.out.Say(msg.Network, msg.Channel, m.sheet(msg, fields))
			return
		case "travel":
			m.setTravel(msg, fields)
			return
		case "town", "where":
			m.out.Say(msg.Network, msg.Channel, m.townStatus(msg))
			return
		case "pet", "companion":
			m.out.Say(msg.Network, msg.Channel, m.petStatus(msg, fields))
			return
		case "mount", "steed":
			m.out.Say(msg.Network, msg.Channel, m.mountStatus(msg))
			return
		case "duel", "spar":
			m.duel(msg, fields)
			return
		case "give", "gift":
			m.give(msg, fields)
			return
		case "quaff", "drink":
			m.quaff(msg)
			return
		case "feats", "achievements":
			m.out.Say(msg.Network, msg.Channel, m.featsStatus(msg, fields))
			return
		case "rest":
			m.rest(msg)
			return
		case "shop":
			m.shop(msg)
			return
		case "buy":
			m.buy(msg, fields)
			return
		case "enchant":
			m.enchant(msg, fields)
			return
		case "revive":
			m.revive(msg)
			return
		case "bless":
			m.bless(msg)
			return
		case "rebirth", "ascend", "prestige":
			m.rebirth(msg)
			return
		case "pause", "resume", "push", "hog", "reset", "setlevel", "gold", "raid":
			m.adminVerb(msg, fields)
			return
		}
	}
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	key := sheetKey(pkey)
	sheet, err := m.store.HGetAll(ctx, key)
	if err != nil {
		m.log.Warn("idlerpg read failed", "err", err)
		m.out.Say(msg.Network, msg.Channel, "the realm is unreachable right now.")
		return
	}
	if _, enrolled := sheet["level"]; !enrolled {
		_ = m.store.HSet(ctx, key, "level", 0)
		_ = m.store.HSet(ctx, key, "ttl", m.ttlFor(0))
		_, _ = m.store.ZIncr(ctx, boardKey(), pkey, 0)
		m.ensureAbilities(ctx, pkey) // roll the D&D ability scores at creation
		m.setOnline(msg.Network, msg.Nick, msg.Channel, pkey)
		m.out.Say(msg.Network, msg.Channel, "welcome to the grind, "+msg.Nick+". you're level 0 — roll for stats and hush. (!rpg sheet)")
		return
	}
	m.setOnline(msg.Network, msg.Nick, msg.Channel, pkey)
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("%s — level %d, %s to the next. (stop talking, it hurts.)",
		m.charLine(ctx, msg.Nick, pkey, sheet), sheet["level"], dur(sheet["ttl"])))
}

// charDesc builds a character's title, e.g. "evil dwarf fighter" — alignment, then
// race, then class, skipping any that are unset.
func (m *Manager) charDesc(ctx context.Context, pkey string, sheet map[string]int64) string {
	desc := fullAlign(sheet["law"], sheet["align"])
	if race, _ := m.store.GetStr(ctx, raceKey(pkey)); race != "" {
		desc += " " + race
	}
	if class, _ := m.store.GetStr(ctx, classKey(pkey)); class != "" {
		desc += " " + class
	}
	return desc
}

func classKey(player string) string { return "rpg:class:" + player }

func alignName(v int64) string {
	switch v {
	case 1:
		return "good"
	case 2:
		return "evil"
	default:
		return "neutral"
	}
}

// setAlign sets the player's alignment: good fights at +11% power, evil crits
// twice as often, neutral is baseline.
func (m *Manager) setAlign(msg engine.Message, fields []string) {
	const usage = "usage: !rpg align <good|neutral|evil>, or <lawful|neutral|chaotic> <good|neutral|evil>"
	if len(fields) < 3 {
		m.out.Say(msg.Network, msg.Channel, usage)
		return
	}
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(sheet) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	if len(fields) >= 4 { // two axes: <ethic> <moral>
		law, lok := parseEthic(fields[2])
		moral, mok := parseMoral(fields[3])
		if !lok || !mok {
			m.out.Say(msg.Network, msg.Channel, usage)
			return
		}
		_ = m.store.HSet(ctx, sheetKey(pkey), "law", law)
		_ = m.store.HSet(ctx, sheetKey(pkey), "align", moral)
	} else if moral, ok := parseMoral(fields[2]); ok { // single moral word (back-compat)
		_ = m.store.HSet(ctx, sheetKey(pkey), "align", moral)
	} else if law, ok := parseEthic(fields[2]); ok { // single ethical word
		_ = m.store.HSet(ctx, sheetKey(pkey), "law", law)
	} else {
		m.out.Say(msg.Network, msg.Channel, usage)
		return
	}
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	m.out.Say(msg.Network, msg.Channel, msg.Nick+" is now "+fullAlign(sheet["law"], sheet["align"])+".")
}

// setClass sets the player's class (flavor text shown in status).
func (m *Manager) setClass(msg engine.Message, fields []string) {
	if len(fields) < 3 {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg class <"+strings.Join(classNames(), "|")+">")
		return
	}
	c, ok := classOf(fields[2])
	if !ok {
		m.out.Say(msg.Network, msg.Channel, "no such class. pick one: "+strings.Join(classNames(), ", "))
		return
	}
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(sheet) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	_ = m.store.SetStr(ctx, classKey(pkey), c.Name)
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("%s is now a %s — %s. ability: %s (%s).",
		msg.Nick, c.Name, c.Blurb, c.Ability, c.AbilDsc))
}

// setRace chooses a character's race once, baking its ability bonuses permanently
// into the rolled scores.
func (m *Manager) setRace(msg engine.Message, fields []string) {
	if len(fields) < 3 {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg race <"+strings.Join(raceNames(), "|")+">")
		return
	}
	r, ok := raceOf(fields[2])
	if !ok {
		m.out.Say(msg.Network, msg.Channel, "no such race. pick one: "+strings.Join(raceNames(), ", "))
		return
	}
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(sheet) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	if cur, _ := m.store.GetStr(ctx, raceKey(pkey)); cur != "" {
		m.out.Say(msg.Network, msg.Channel, "your heritage is already set — you can't change your blood.")
		return
	}
	m.ensureAbilities(ctx, pkey) // make sure base scores exist before adding bonuses
	for field, bonus := range r.Mods {
		_, _ = m.store.HIncr(ctx, sheetKey(pkey), field, bonus)
	}
	_ = m.store.SetStr(ctx, raceKey(pkey), r.Name)
	m.out.Say(msg.Network, msg.Channel, msg.Nick+" is now "+article(r.Name)+" "+r.Name+" — "+r.Blurb+". (stats adjusted)")
}

// article returns "an" before a vowel-ish word, else "a".
func article(s string) string {
	if s == "" {
		return "a"
	}
	if strings.ContainsRune("aeiou", rune(s[0])) {
		return "an"
	}
	return "a"
}

func (m *Manager) leaderboard() string {
	top, err := m.store.ZTop(context.Background(), boardKey(), 5)
	if err != nil {
		return "the realm is unreachable right now."
	}
	if len(top) == 0 {
		return "no idlers yet. !rpg to begin the grind."
	}
	parts := make([]string, 0, len(top))
	for _, e := range top {
		parts = append(parts, fmt.Sprintf("%s (lvl %d)", e.Member, e.Score))
	}
	return "top idlers: " + strings.Join(parts, ", ")
}

// info renders a one-line realm summary: how many idlers are online, who's on
// top, and whether a quest is afoot.
func (m *Manager) info() string {
	m.mu.Lock()
	online := len(m.online)
	m.mu.Unlock()

	lead := "nobody yet"
	if top, err := m.store.ZTop(context.Background(), boardKey(), 1); err == nil && len(top) > 0 {
		lead = fmt.Sprintf("%s (lvl %d)", top[0].Member, top[0].Score)
	}

	quest := "no quest underway"
	m.qmu.Lock()
	if q := m.quest; q != nil {
		left := q.Deadline - m.now().Unix()
		quest = fmt.Sprintf("a quest is underway — %d on it, %s left", len(q.Members), dur(left))
	}
	m.qmu.Unlock()

	wev := ""
	m.emu.Lock()
	if e := m.wevent; e != nil {
		wev = fmt.Sprintf(" · 🌕 %s — %s (%s left)", e.Name, e.Desc, dur(e.Deadline-m.now().Unix()))
	}
	m.emu.Unlock()

	raid := ""
	m.bmu.Lock()
	if b := m.boss; b != nil {
		pct := int64(0)
		if b.MaxHP > 0 && b.HP > 0 {
			pct = b.HP * 100 / b.MaxHP
		}
		raid = fmt.Sprintf(" · 🐲 WORLD BOSS: %s at %d%% HP — to arms!", b.Name, pct)
	}
	m.bmu.Unlock()

	return fmt.Sprintf("the realm: %d idling now · top idler %s · %s%s%s.", online, lead, quest, wev, raid)
}

// questStatus describes the active quest, or says there isn't one.
func (m *Manager) questStatus() string {
	m.qmu.Lock()
	q := m.quest
	m.qmu.Unlock()
	if q == nil {
		return "no quest underway. keep idling — the gods call the worthy when they please."
	}
	if q.Kind == "hunt" {
		left := q.Deadline - m.now().Unix()
		return fmt.Sprintf("🏹 hunt in progress: %s must slay %d foes — %d down, %s left. one peep or departure and it's ruined.",
			strings.Join(questNicks(q), ", "), q.Target, q.Progress, dur(left))
	}
	if q.Kind == "map" {
		return fmt.Sprintf("🗺️ map quest in progress: %s journey to %s (leg %d of 2). one stray word or step and it's ruined.",
			strings.Join(questNicks(q), ", "), q.Desc, q.Stage+1)
	}
	left := q.Deadline - m.now().Unix()
	return fmt.Sprintf("⚔️ quest in progress: %s must %s. %s remaining — one peep or departure and it's ruined.",
		strings.Join(questNicks(q), ", "), q.Desc, dur(left))
}

// status reports a player's sheet — the sender's own, or a named other's.
func (m *Manager) status(msg engine.Message, fields []string) string {
	ctx := context.Background()
	name := msg.Nick
	var pkey string
	if len(fields) >= 3 {
		name = fields[2]
		pkey = m.resolve(msg.Network, "", name) // a named other: resolve by nick
	} else {
		pkey = m.resolve(msg.Network, msg.Account, msg.Nick) // self
	}
	sheet, err := m.store.HGetAll(ctx, sheetKey(pkey))
	if err != nil {
		return "the realm is unreachable right now."
	}
	if _, ok := sheet["level"]; !ok {
		return name + " isn't playing. !rpg to start the grind."
	}
	return fmt.Sprintf("%s — level %d, %s to the next · power %d.",
		m.charLine(ctx, name, pkey, sheet), sheet["level"], dur(sheet["ttl"]), itemSum(sheet))
}

// sheet renders a character's D&D ability block — the sender's own or a named
// other's. Lazily rolls scores for characters created before abilities existed.
func (m *Manager) sheet(msg engine.Message, fields []string) string {
	ctx := context.Background()
	name := msg.Nick
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if len(fields) >= 3 {
		name = fields[2]
		pkey = m.resolve(msg.Network, "", name)
	}
	if s, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(s) == 0 {
		return name + " isn't playing. !rpg to start the grind."
	}
	m.ensureAbilities(ctx, pkey)
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	class, _ := m.store.GetStr(ctx, classKey(pkey))
	abil := ""
	if c, ok := classOf(class); ok {
		abil = " · " + c.Ability
	}
	pots := ""
	if sheet["pots"] > 0 {
		pots = fmt.Sprintf(" · %d🧪", sheet["pots"])
	}
	hp := fmt.Sprintf("%d/%d", curHP(sheet, class), maxHP(sheet, class))
	if poisoned(sheet) {
		hp += "☠️"
	}
	if blessed(sheet) {
		hp += "🕊️"
	}
	return fmt.Sprintf("%s (lvl %d) — HP %s · %dg · %d kills%s%s · %s",
		m.charLine(ctx, name, pkey, sheet), sheet["level"], hp,
		sheet["gold"], sheet["kills"], pots, abil, abilityLine(sheet))
}

// adminVerb handles the privileged !rpg commands (pause/resume/push/hog). They are
// gated by the authz hook, so a non-admin just gets a brush-off.
func (m *Manager) adminVerb(msg engine.Message, fields []string) {
	if !m.authorized(msg) {
		m.out.Say(msg.Network, msg.Channel, "the gods do not heed you.")
		return
	}
	ctx := context.Background()
	switch strings.ToLower(fields[1]) {
	case "pause":
		m.paused.Store(true)
		m.out.Say(msg.Network, msg.Channel, "⏸ the realm freezes — idling is suspended.")
	case "resume":
		m.paused.Store(false)
		m.out.Say(msg.Network, msg.Channel, "▶ the realm stirs back to life. idle on.")
	case "push":
		// !rpg push <name> <seconds> — move a player's clock (negative = forward).
		if len(fields) < 4 {
			m.out.Say(msg.Network, msg.Channel, "usage: !rpg push <name> <seconds> (negative = toward the next level)")
			return
		}
		secs, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			m.out.Say(msg.Network, msg.Channel, "that's not a number of seconds.")
			return
		}
		pkey := m.resolve(msg.Network, "", fields[2])
		if sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(sheet) == 0 {
			m.out.Say(msg.Network, msg.Channel, fields[2]+" isn't playing.")
			return
		}
		_, _ = m.store.HIncr(ctx, sheetKey(pkey), "ttl", secs)
		dir := "toward"
		if secs > 0 {
			dir = "away from"
		}
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("✋ the gods shove %s %ds %s the next level.", fields[2], abs64(secs), dir))
	case "hog":
		// !rpg hog [name] — invoke the Hand of God on a named player or a random one.
		var p player
		ok := false
		if len(fields) >= 3 {
			pkey := m.resolve(msg.Network, "", fields[2])
			if sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(sheet) > 0 {
				p, ok = player{network: msg.Network, nick: fields[2], channel: msg.Channel, key: pkey}, true
			}
		} else {
			p, ok = m.randomOnline()
		}
		if !ok {
			m.out.Say(msg.Network, msg.Channel, "there's no one for the Hand of God to find.")
			return
		}
		m.handOfGod(ctx, p)

	case "raid":
		// !rpg raid — summon a world boss on demand (for an event, or to test).
		m.bmu.Lock()
		active := m.boss != nil
		m.bmu.Unlock()
		if active {
			m.out.Say(msg.Network, msg.Channel, "a world boss already stalks the realm.")
			return
		}
		m.spawnWorldBoss(ctx, msg.Network, msg.Channel)

	case "reset":
		// !rpg reset <name>  — wipe one character.
		// !rpg reset all yes — wipe the entire realm.
		if len(fields) < 3 {
			m.out.Say(msg.Network, msg.Channel, "usage: !rpg reset <name>, or !rpg reset all yes")
			return
		}
		if strings.EqualFold(fields[2], "all") {
			if len(fields) < 4 || strings.ToLower(fields[3]) != "yes" {
				m.out.Say(msg.Network, msg.Channel, "this wipes EVERY character and the active quest. confirm with: !rpg reset all yes")
				return
			}
			n := m.wipeAll(ctx)
			m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("💥 the realm is wiped clean — %d characters gone. a new age begins.", n))
			return
		}
		pkey := m.resolve(msg.Network, "", fields[2])
		if sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(sheet) == 0 {
			m.out.Say(msg.Network, msg.Channel, fields[2]+" isn't playing.")
			return
		}
		m.wipeChar(ctx, pkey)
		m.out.Say(msg.Network, msg.Channel, fields[2]+"'s character has been erased.")

	case "setlevel":
		if len(fields) < 4 {
			m.out.Say(msg.Network, msg.Channel, "usage: !rpg setlevel <name> <level>")
			return
		}
		lvl, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil || lvl < 0 {
			m.out.Say(msg.Network, msg.Channel, "level must be a non-negative number.")
			return
		}
		pkey := m.resolve(msg.Network, "", fields[2])
		sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
		if len(sheet) == 0 {
			m.out.Say(msg.Network, msg.Channel, fields[2]+" isn't playing.")
			return
		}
		_, _ = m.store.ZIncr(ctx, boardKey(), pkey, lvl-sheet["level"]) // keep the board in sync
		_ = m.store.HSet(ctx, sheetKey(pkey), "level", lvl)
		_ = m.store.HSet(ctx, sheetKey(pkey), "ttl", m.ttlFor(lvl))
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("✨ %s is now level %d.", fields[2], lvl))

	case "gold":
		if len(fields) < 4 {
			m.out.Say(msg.Network, msg.Channel, "usage: !rpg gold <name> <amount> (negative to remove)")
			return
		}
		amt, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			m.out.Say(msg.Network, msg.Channel, "that's not an amount of gold.")
			return
		}
		pkey := m.resolve(msg.Network, "", fields[2])
		if sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(sheet) == 0 {
			m.out.Say(msg.Network, msg.Channel, fields[2]+" isn't playing.")
			return
		}
		now, _ := m.store.HIncr(ctx, sheetKey(pkey), "gold", amt)
		if now < 0 { // never let an adjustment drive gold negative
			_ = m.store.HSet(ctx, sheetKey(pkey), "gold", 0)
			now = 0
		}
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("💰 %s now has %dg.", fields[2], now))
	}
}

// wipeChar erases a single character: its sheet, item names, class, race, and
// leaderboard entry, plus any online presence.
func (m *Manager) wipeChar(ctx context.Context, key string) {
	for _, s := range itemSlots {
		_ = m.store.Del(ctx, nameKey(key, s))
	}
	_ = m.store.Del(ctx, classKey(key))
	_ = m.store.Del(ctx, raceKey(key))
	_ = m.store.Del(ctx, stashKey(key))
	_ = m.store.Del(ctx, mountKey(key))
	_ = m.store.Del(ctx, petKey(key))
	_ = m.store.Del(ctx, dungeonKey(key))
	_ = m.store.Del(ctx, sheetKey(key))
	_ = m.store.ZRem(ctx, boardKey(), key)
	m.mu.Lock()
	for ok, p := range m.online {
		if p.key == key {
			delete(m.online, ok)
		}
	}
	m.mu.Unlock()
}

// wipeAll erases every character and clears the active quest. Returns the count.
func (m *Manager) wipeAll(ctx context.Context) int {
	top, _ := m.store.ZTop(ctx, boardKey(), 100000)
	for _, e := range top {
		for _, s := range itemSlots {
			_ = m.store.Del(ctx, nameKey(e.Member, s))
		}
		_ = m.store.Del(ctx, classKey(e.Member))
		_ = m.store.Del(ctx, raceKey(e.Member))
		_ = m.store.Del(ctx, stashKey(e.Member))
		_ = m.store.Del(ctx, mountKey(e.Member))
		_ = m.store.Del(ctx, petKey(e.Member))
		_ = m.store.Del(ctx, dungeonKey(e.Member))
		_ = m.store.Del(ctx, sheetKey(e.Member))
	}
	_ = m.store.Del(ctx, boardKey())
	_ = m.store.Del(ctx, questKey())
	m.qmu.Lock()
	m.quest = nil
	m.qmu.Unlock()
	m.clearBoss(ctx)
	m.mu.Lock()
	m.online = map[string]player{}
	m.mu.Unlock()
	m.paused.Store(false)
	return len(top)
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// OnJoin marks an enrolled player online when they (re)appear in a channel.
func (m *Manager) OnJoin(ev event.Event) {
	if ev.Kind != event.Join {
		return
	}
	m.markPresent(ev)
}

// OnPresent seeds an enrolled player online from a NAMES sweep (e.g. after a bot
// restart), so idlers already sitting in the channel keep progressing without
// having to rejoin or speak. NAMES carries no account tag, but linked players
// linked by nick, so the resolver still finds their character.
func (m *Manager) OnPresent(ev event.Event) {
	if ev.Kind != event.Present {
		return
	}
	m.markPresent(ev)
}

// markPresent marks the event's subject online iff they're an enrolled character.
func (m *Manager) markPresent(ev event.Event) {
	pkey := m.resolve(ev.Network, ev.Account, ev.Nick)
	sheet, err := m.store.HGetAll(context.Background(), sheetKey(pkey))
	if err != nil {
		return
	}
	if _, enrolled := sheet["level"]; enrolled {
		m.setOnline(ev.Network, ev.Nick, ev.Channel, pkey)
	}
}

// Penalties (seconds added to time-to-level) for abandoning the idle, idlerpg.net-style.
const (
	partPenalty = 200
	quitPenalty = 20
	nickPenalty = 30
	kickPenalty = 250
)

// penalizeOnline adds secs to an online player's clock.
func (m *Manager) penalizeOnline(network, nick string, secs int64) {
	if p, ok := m.onlinePlayer(network, nick); ok {
		_, _ = m.store.HIncr(context.Background(), sheetKey(p.key), "ttl", secs)
	}
}

// OnPart / OnQuit / OnKick penalize, break any quest they were on, then take the
// player offline.
func (m *Manager) OnPart(ev event.Event) {
	if ev.Kind == event.Part {
		m.penalizeOnline(ev.Network, ev.Nick, partPenalty)
		m.failQuestBy(ev.Network, ev.Nick, "abandoned the party")
		m.OnLeave(ev)
	}
}

func (m *Manager) OnQuit(ev event.Event) {
	if ev.Kind == event.Quit {
		m.penalizeOnline(ev.Network, ev.Nick, quitPenalty)
		m.failQuestBy(ev.Network, ev.Nick, "vanished from the realm")
		m.OnLeave(ev)
	}
}

func (m *Manager) OnKick(ev event.Event) {
	if ev.Kind == event.Kick {
		m.penalizeOnline(ev.Network, ev.Nick, kickPenalty)
		m.failQuestBy(ev.Network, ev.Nick, "was hurled from the channel")
		m.OnLeave(ev)
	}
}

// failQuestBy ruins the active quest if (network, nick) resolves to a quester.
func (m *Manager) failQuestBy(network, nick, reason string) {
	key := m.resolve(network, "", nick)
	m.questViolation(context.Background(), key, nick, reason)
}

// OnNick penalizes a nick change, breaks any quest, and follows the player to
// their new nick.
func (m *Manager) OnNick(ev event.Event) {
	if ev.Kind != event.Nick {
		return
	}
	m.penalizeOnline(ev.Network, ev.Nick, nickPenalty)
	m.failQuestBy(ev.Network, ev.Nick, "slipped away under a new name")
	m.mu.Lock()
	if p, ok := m.online[okey(ev.Network, ev.Nick)]; ok {
		delete(m.online, okey(ev.Network, ev.Nick))
		p.nick = ev.Text
		m.online[okey(ev.Network, ev.Text)] = p
	}
	m.mu.Unlock()
}

// OnLeave takes a player offline (they stop progressing) without a penalty.
func (m *Manager) OnLeave(ev event.Event) {
	m.mu.Lock()
	delete(m.online, okey(ev.Network, ev.Nick))
	m.mu.Unlock()
}

// Tick advances every online player toward their next level by one interval.
func (m *Manager) Tick() {
	if m.paused.Load() {
		return // the game is paused by an admin
	}
	step := int64(m.interval / time.Second)
	if step < 1 {
		step = 1
	}
	m.weatherTick(context.Background())    // roll a fresh sky when the old one blows out
	m.worldEventTick(context.Background()) // begin/end a realm-wide modifier
	m.maybeEvent(context.Background())
	m.maybeMonster(context.Background())
	m.questTick(context.Background())
	m.worldBossTick(context.Background())  // advance an active raid
	m.maybeWorldBoss(context.Background()) // or rarely raise one
	roster := m.snapshot()
	step = step * fellowshipPct(len(roster)) / 100 // idling together speeds everyone up
	for _, p := range roster {
		ctx := context.Background()
		key := sheetKey(p.key)
		if m.inDungeon(ctx, p.key) {
			m.dungeonTick(ctx, p) // delving: push into the next room instead of roaming
		} else {
			m.moveOnMap(ctx, p) // wander the world map (or travel to a town)
			m.maybeDiscoverDungeon(ctx, p)
		}
		m.tickStatus(ctx, p) // poison and other timed effects sap/decay
		if m.tickHP(ctx, p.key) {
			continue // downed and recovering — no progress this tick
		}
		ttl, err := m.store.HIncr(ctx, key, "ttl", -step)
		if err != nil {
			continue
		}
		if ttl > 0 {
			continue
		}
		lvl, err := m.store.HIncr(ctx, key, "level", 1)
		if err != nil {
			continue
		}
		reb := int64(0)
		if s, _ := m.store.HGetAll(ctx, key); s != nil {
			reb = s["reb"]
		}
		_ = m.store.HSet(ctx, key, "ttl", m.ttlForReb(lvl, reb))
		_, _ = m.store.ZIncr(ctx, boardKey(), p.key, 1)
		m.drama(p.network, p.channel, fmt.Sprintf("✨ %s has attained level %d! the idle is strong with this one.", p.nick, lvl))
		m.findItem(ctx, p, lvl)
		m.battle(ctx, p, lvl)
	}
}

const battleSec = 8 // seconds-per-level swing in a fight

// battle pits the just-levelled player against a random other online player,
// weighted by item power (idlerpg.net's level-up combat). Winner's clock speeds
// up, loser's slows; small chance of a critical strike for a bigger swing.
func (m *Manager) battle(ctx context.Context, p player, level int64) {
	opp, ok := m.randomOpponent(p.key)
	if !ok {
		return // no one else to fight
	}
	mine, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	theirs, _ := m.store.HGetAll(ctx, sheetKey(opp.key))
	myPow, oppPow := itemSum(mine), itemSum(theirs)

	// Alignment: good is blessed in combat (+11%); evil is chaotic (crits 2× as often).
	effPow := myPow
	if mine["align"] == 1 {
		effPow = myPow * 111 / 100
	}
	// Class: your primary ability's modifier sharpens (or dulls) your attack.
	myClass, _ := m.store.GetStr(ctx, classKey(p.key))
	effPow += classAttackMod(mine, myClass)
	if effPow < 0 {
		effPow = 0
	}
	critOdds := 10
	if mine["align"] == 2 {
		critOdds = 5
	}
	win := m.roll(int(effPow)+1) >= m.roll(int(oppPow)+1)
	amt := int64(m.roll(int(level)+1)+1) * battleSec
	crit := m.roll(critOdds) == 0
	if crit {
		amt *= 5
	}

	verb, dir, sign := "won", "sooner", int64(-1)
	downed := false
	if !win {
		verb, dir, sign = "lost", "later", int64(1)
		// Losing the bout also costs blood — and can leave you downed.
		hurt := int64(m.roll(int(level)+3) + 2)
		if crit {
			hurt *= 2
		}
		m.damage(ctx, p.key, hurt)
		after, _ := m.store.HGetAll(ctx, sheetKey(p.key))
		downed = isDowned(after, myClass)
	}
	_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", sign*amt)

	critStr := ""
	if crit {
		critStr = " a CRITICAL"
	}
	m.out.Say(p.network, p.channel, fmt.Sprintf("🗡️ %s [%d] challenged %s [%d] in combat and %s%s — %ds %s.",
		p.nick, myPow, opp.nick, oppPow, verb, critStr, amt, dir))
	if downed {
		m.out.Say(p.network, p.channel, fmt.Sprintf("💢 %s is beaten down to 0 HP and must recover before pressing on.", p.nick))
	}
}

const eventOdds = 6 // ~1-in-N chance an event fires each tick

// maybeEvent occasionally visits luck (good or bad) on a random online player —
// idlerpg.net's godsends, calamities, and the Hand of God.
func (m *Manager) maybeEvent(ctx context.Context) {
	odds := eventOdds
	if m.eventKind() == "tempest" { // a Storm of Fate: the gods meddle far more
		odds = 3
	}
	if m.roll(odds) != 0 {
		return
	}
	p, ok := m.randomOnline()
	if !ok {
		return
	}
	switch m.roll(4) {
	case 0:
		m.godsend(ctx, p)
	case 1:
		m.calamity(ctx, p)
	case 2:
		m.handOfGod(ctx, p)
	default:
		m.merchant(ctx, p)
	}
}

// pctOfTTL returns lo..hi percent of the player's current time-to-level.
func (m *Manager) pctOfTTL(ctx context.Context, key string, lo, hi int) int64 {
	sheet, _ := m.store.HGetAll(ctx, sheetKey(key))
	ttl := sheet["ttl"]
	if ttl <= 0 {
		return 0
	}
	pct := int64(lo + m.roll(hi-lo+1))
	amt := ttl * pct / 100
	if amt < 1 {
		amt = 1
	}
	return amt
}

func (m *Manager) godsend(ctx context.Context, p player) {
	amt := m.pctOfTTL(ctx, p.key, 5, 12)
	_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", -amt)
	m.drama(p.network, p.channel, fmt.Sprintf("🍀 godsend! the gods smile on %s — %ds closer to the next level.", p.nick, amt))
}

func (m *Manager) calamity(ctx context.Context, p player) {
	// half the time it's lost time, half the time an item loses its luster.
	if m.roll(2) == 0 {
		sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
		var owned []string
		for _, s := range itemSlots {
			if sheet[itemField(s)] > 0 {
				owned = append(owned, s)
			}
		}
		if len(owned) > 0 {
			slot := owned[m.roll(len(owned))]
			nl := sheet[itemField(slot)] * 9 / 10
			_ = m.store.HSet(ctx, sheetKey(p.key), itemField(slot), nl)
			m.drama(p.network, p.channel, fmt.Sprintf("💀 calamity! %s's %s loses its luster — now level %d.", p.nick, slot, nl))
			return
		}
	}
	amt := m.pctOfTTL(ctx, p.key, 5, 12)
	_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", amt)
	m.drama(p.network, p.channel, fmt.Sprintf("💀 calamity! disaster befalls %s — %ds further from the next level.", p.nick, amt))
}

func (m *Manager) handOfGod(ctx context.Context, p player) {
	amt := m.pctOfTTL(ctx, p.key, 15, 30)
	if m.roll(2) == 0 {
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", -amt)
		m.drama(p.network, p.channel, fmt.Sprintf("✋ the Hand of God carries %s %ds forward!", p.nick, amt))
		return
	}
	_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", amt)
	m.drama(p.network, p.channel, fmt.Sprintf("✋ the Hand of God flings %s %ds backward!", p.nick, amt))
}

// randomOnline picks any online player.
func (m *Manager) randomOnline() (player, bool) {
	m.mu.Lock()
	var all []player
	for _, p := range m.online {
		all = append(all, p)
	}
	m.mu.Unlock()
	if len(all) == 0 {
		return player{}, false
	}
	return all[m.roll(len(all))], true
}

// randomOpponent picks a random online player whose character differs from key.
func (m *Manager) randomOpponent(key string) (player, bool) {
	m.mu.Lock()
	var others []player
	for _, p := range m.online {
		if p.key != key {
			others = append(others, p)
		}
	}
	m.mu.Unlock()
	if len(others) == 0 {
		return player{}, false
	}
	return others[m.roll(len(others))], true
}

// findItem rolls an item drop on level-up; equips + announces it only if it beats
// what's in that slot (idlerpg.net behavior).
func (m *Manager) findItem(ctx context.Context, p player, level int64) {
	slot := itemSlots[m.roll(len(itemSlots))]
	found := int64(m.roll(int(level)+1) + 1) // 1..level+1
	rIdx := int64(m.pickRarity(level))
	newPow := found * rarityMult(rIdx) / 100

	sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	curPow := sheet[itemField(slot)] * rarityMult(sheet[rarityField(slot)]) / 100
	if newPow <= curPow {
		return // the find isn't an upgrade
	}
	// Salvage the old gear instead of discarding it — scrap gold for any item the
	// new find replaces, so every drop is worth something.
	scrap := int64(0)
	if curPow > 0 {
		scrap = curPow/3 + 1
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "gold", scrap)
		m.bumpStat("gold", scrap)
	}
	_ = m.store.HSet(ctx, sheetKey(p.key), itemField(slot), found)
	_ = m.store.HSet(ctx, sheetKey(p.key), rarityField(slot), rIdx)

	name := ""
	if rarities[rIdx].named {
		name = m.magicName(slot, rarities[rIdx].name == "legendary")
		_ = m.store.SetStr(ctx, nameKey(p.key, slot), name)
	} else {
		_ = m.store.Del(ctx, nameKey(p.key, slot)) // clear any prior name on replace
	}

	label := rarityName(rIdx)
	out := fmt.Sprintf("%s found %s %s level %d %s", p.nick, article(label), label, found, slot)
	if name != "" {
		out += " — “" + name + "”"
	}
	if scrap > 0 {
		out += fmt.Sprintf(" (salvaged the old one for %dg)", scrap)
	}
	m.out.Say(p.network, p.channel, out+"!")
	if rarities[rIdx].named { // epic & legendary finds are feed-worthy; commons aren't
		m.record(out + "!")
	}
	if rarities[rIdx].name == "legendary" {
		m.awardFeat(ctx, p, 1<<4) // Treasure Hunter
		m.bumpStat("legendaries", 1)
	}
}

// itemSum is the character's total equipment power.
func itemSum(sheet map[string]int64) int64 {
	var sum int64
	for _, s := range itemSlots {
		if lvl := sheet[itemField(s)]; lvl > 0 {
			sum += lvl * rarityMult(sheet[rarityField(s)]) / 100
		}
	}
	return sum
}

// items renders a player's equipment + power.
func (m *Manager) items(msg engine.Message) string {
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	if _, ok := sheet["level"]; !ok {
		return "you're not playing. !rpg to start the grind."
	}
	var parts []string
	for _, s := range itemSlots {
		lvl := sheet[itemField(s)]
		if lvl <= 0 {
			continue
		}
		part := fmt.Sprintf("%s %s %d", rarityName(sheet[rarityField(s)]), s, lvl)
		if name, _ := m.store.GetStr(ctx, nameKey(pkey, s)); name != "" {
			part += " “" + name + "”"
		}
		parts = append(parts, part)
	}
	gear := "nothing yet"
	if len(parts) > 0 {
		gear = strings.Join(parts, ", ")
	}
	return fmt.Sprintf("%s — power %d · %s", msg.Nick, itemSum(sheet), gear)
}

func (m *Manager) ttlFor(level int64) int64 {
	secs := m.baseTTL.Seconds() * math.Pow(growth, float64(level))
	if math.IsInf(secs, 1) || secs > ttlCap {
		return ttlCap
	}
	return int64(secs)
}

func (m *Manager) setOnline(network, nick, channel, key string) {
	m.mu.Lock()
	m.online[okey(network, nick)] = player{network: network, nick: nick, channel: channel, key: key}
	m.mu.Unlock()
}

func (m *Manager) onlinePlayer(network, nick string) (player, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.online[okey(network, nick)]
	return p, ok
}

func (m *Manager) snapshot() []player {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]player, 0, len(m.online))
	for _, p := range m.online {
		out = append(out, p)
	}
	return out
}

// dur renders a seconds count as a compact human duration.
func dur(secs int64) string {
	if secs < 0 {
		secs = 0
	}
	return (time.Duration(secs) * time.Second).Round(time.Second).String()
}
