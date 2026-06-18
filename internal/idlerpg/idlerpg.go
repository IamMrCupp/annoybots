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
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("%s — level %d, %s to the next. (stop talking, it hurts.)",
		msg.Nick, sheet["level"], dur(sheet["ttl"])))
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

// OnLeave takes a player offline (they stop progressing) on part/quit.
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

	win := m.roll(int(myPow)+1) >= m.roll(int(oppPow)+1)
	amt := int64(m.roll(int(level)+1)+1) * battleSec
	crit := m.roll(10) == 0
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
