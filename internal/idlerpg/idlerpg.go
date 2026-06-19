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
	"strings"
	"sync"
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
	growth     = 1.16           // time-to-level multiplier per level
	ttlCap     = 30 * 24 * 3600 // never make a single level take more than 30 days
	talkCapSec = 60             // max penalty seconds for one message
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

	mu     sync.Mutex
	online map[string]player // network|nick -> online player

	rmu sync.Mutex
	rng *rand.Rand
}

// New builds a Manager. interval is the tick period; baseTTL is the level 0→1 time.
// resolve maps senders to canonical player keys (cross-network when linked).
func New(store state.Store, out engine.Sender, resolve Resolver, interval, baseTTL time.Duration, log *slog.Logger) *Manager {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if baseTTL <= 0 {
		baseTTL = 10 * time.Minute
	}
	if resolve == nil {
		resolve = func(network, _, nick string) string { return strings.ToLower(network) + "|" + strings.ToLower(nick) }
	}
	return &Manager{
		store: store, out: out, resolve: resolve, log: log,
		interval: interval, baseTTL: baseTTL,
		online: map[string]player{},
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
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
	// Talking is the cardinal sin — penalize online players.
	if p, ok := m.onlinePlayer(msg.Network, msg.Nick); ok {
		pen := int64(len(msg.Text))
		if pen > talkCapSec {
			pen = talkCapSec
		}
		_, _ = m.store.HIncr(context.Background(), sheetKey(p.key), "ttl", pen)
	}
	return false
}

func (m *Manager) command(msg engine.Message, fields []string) {
	if len(fields) >= 2 {
		switch strings.ToLower(fields[1]) {
		case "top":
			m.out.Say(msg.Network, msg.Channel, m.leaderboard())
			return
		case "items", "gear":
			m.out.Say(msg.Network, msg.Channel, m.items(msg))
			return
		case "align":
			m.setAlign(msg, fields)
			return
		case "class":
			m.setClass(msg, fields)
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
		m.setOnline(msg.Network, msg.Nick, msg.Channel, pkey)
		m.out.Say(msg.Network, msg.Channel, "welcome to the grind, "+msg.Nick+". you're level 0 — now hush and idle.")
		return
	}
	m.setOnline(msg.Network, msg.Nick, msg.Channel, pkey)
	desc := alignName(sheet["align"])
	if class, _ := m.store.GetStr(ctx, classKey(pkey)); class != "" {
		desc += " " + class
	}
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("%s the %s — level %d, %s to the next. (stop talking, it hurts.)",
		msg.Nick, desc, sheet["level"], dur(sheet["ttl"])))
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
	if len(fields) < 3 {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg align good|neutral|evil")
		return
	}
	var v int64
	switch strings.ToLower(fields[2]) {
	case "good":
		v = 1
	case "evil":
		v = 2
	case "neutral":
		v = 0
	default:
		m.out.Say(msg.Network, msg.Channel, "alignment is good, neutral, or evil.")
		return
	}
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(sheet) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	_ = m.store.HSet(ctx, sheetKey(pkey), "align", v)
	m.out.Say(msg.Network, msg.Channel, msg.Nick+" is now "+alignName(v)+".")
}

// setClass sets the player's class (flavor text shown in status).
func (m *Manager) setClass(msg engine.Message, fields []string) {
	if len(fields) < 3 {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg class <name>")
		return
	}
	class := sanitizeClass(strings.Join(fields[2:], " "))
	if class == "" {
		m.out.Say(msg.Network, msg.Channel, "class name must be printable (<= 24 chars).")
		return
	}
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(sheet) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	_ = m.store.SetStr(ctx, classKey(pkey), class)
	m.out.Say(msg.Network, msg.Channel, msg.Nick+" is now a "+class+".")
}

func sanitizeClass(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 24 {
		s = s[:24]
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return s
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

// OnJoin marks an enrolled player online when they (re)appear in a channel.
func (m *Manager) OnJoin(ev event.Event) {
	if ev.Kind != event.Join {
		return
	}
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

// OnPart / OnQuit / OnKick penalize then take the player offline.
func (m *Manager) OnPart(ev event.Event) {
	if ev.Kind == event.Part {
		m.penalizeOnline(ev.Network, ev.Nick, partPenalty)
		m.OnLeave(ev)
	}
}

func (m *Manager) OnQuit(ev event.Event) {
	if ev.Kind == event.Quit {
		m.penalizeOnline(ev.Network, ev.Nick, quitPenalty)
		m.OnLeave(ev)
	}
}

func (m *Manager) OnKick(ev event.Event) {
	if ev.Kind == event.Kick {
		m.penalizeOnline(ev.Network, ev.Nick, kickPenalty)
		m.OnLeave(ev)
	}
}

// OnNick penalizes a nick change and follows the player to their new nick.
func (m *Manager) OnNick(ev event.Event) {
	if ev.Kind != event.Nick {
		return
	}
	m.penalizeOnline(ev.Network, ev.Nick, nickPenalty)
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
	step := int64(m.interval / time.Second)
	if step < 1 {
		step = 1
	}
	m.maybeEvent(context.Background())
	for _, p := range m.snapshot() {
		ctx := context.Background()
		key := sheetKey(p.key)
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
		_ = m.store.HSet(ctx, key, "ttl", m.ttlFor(lvl))
		_, _ = m.store.ZIncr(ctx, boardKey(), p.key, 1)
		m.out.Say(p.network, p.channel, fmt.Sprintf("✨ %s has attained level %d! the idle is strong with this one.", p.nick, lvl))
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
	if !win {
		verb, dir, sign = "lost", "later", int64(1)
	}
	_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", sign*amt)

	critStr := ""
	if crit {
		critStr = " a CRITICAL"
	}
	m.out.Say(p.network, p.channel, fmt.Sprintf("🗡️ %s [%d] challenged %s [%d] in combat and %s%s — %ds %s.",
		p.nick, myPow, opp.nick, oppPow, verb, critStr, amt, dir))
}

const eventOdds = 6 // ~1-in-N chance an event fires each tick

// maybeEvent occasionally visits luck (good or bad) on a random online player —
// idlerpg.net's godsends, calamities, and the Hand of God.
func (m *Manager) maybeEvent(ctx context.Context) {
	if m.roll(eventOdds) != 0 {
		return
	}
	p, ok := m.randomOnline()
	if !ok {
		return
	}
	switch m.roll(3) {
	case 0:
		m.godsend(ctx, p)
	case 1:
		m.calamity(ctx, p)
	default:
		m.handOfGod(ctx, p)
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
	m.out.Say(p.network, p.channel, fmt.Sprintf("🍀 godsend! the gods smile on %s — %ds closer to the next level.", p.nick, amt))
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
			m.out.Say(p.network, p.channel, fmt.Sprintf("💀 calamity! %s's %s loses its luster — now level %d.", p.nick, slot, nl))
			return
		}
	}
	amt := m.pctOfTTL(ctx, p.key, 5, 12)
	_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", amt)
	m.out.Say(p.network, p.channel, fmt.Sprintf("💀 calamity! disaster befalls %s — %ds further from the next level.", p.nick, amt))
}

func (m *Manager) handOfGod(ctx context.Context, p player) {
	amt := m.pctOfTTL(ctx, p.key, 15, 30)
	if m.roll(2) == 0 {
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", -amt)
		m.out.Say(p.network, p.channel, fmt.Sprintf("✋ the Hand of God carries %s %ds forward!", p.nick, amt))
		return
	}
	_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", amt)
	m.out.Say(p.network, p.channel, fmt.Sprintf("✋ the Hand of God flings %s %ds backward!", p.nick, amt))
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
	sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	if found <= sheet[itemField(slot)] {
		return
	}
	_ = m.store.HSet(ctx, sheetKey(p.key), itemField(slot), found)
	m.out.Say(p.network, p.channel, fmt.Sprintf("%s found a level %d %s!", p.nick, found, slot))
}

// itemSum is the character's total equipment power.
func itemSum(sheet map[string]int64) int64 {
	var sum int64
	for _, s := range itemSlots {
		sum += sheet[itemField(s)]
	}
	return sum
}

// items renders a player's equipment + power.
func (m *Manager) items(msg engine.Message) string {
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	sheet, _ := m.store.HGetAll(context.Background(), sheetKey(pkey))
	if _, ok := sheet["level"]; !ok {
		return "you're not playing. !rpg to start the grind."
	}
	var parts []string
	for _, s := range itemSlots {
		if lvl := sheet[itemField(s)]; lvl > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", s, lvl))
		}
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
