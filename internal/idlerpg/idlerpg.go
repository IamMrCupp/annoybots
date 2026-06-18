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
	"strings"
	"sync"
	"time"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/event"
	"github.com/IamMrCupp/annoybots/internal/state"
)

const (
	growth     = 1.16           // time-to-level multiplier per level
	ttlCap     = 30 * 24 * 3600 // never make a single level take more than 30 days
	talkCapSec = 60             // max penalty seconds for one message
)

// player is who's currently online (present + enrolled) and where to announce.
type player struct {
	network string
	nick    string
	channel string
}

// Manager runs the game for one bot.
type Manager struct {
	store    state.Store
	out      engine.Sender
	log      *slog.Logger
	interval time.Duration
	baseTTL  time.Duration

	mu     sync.Mutex
	online map[string]player // network|nick -> online player
}

// New builds a Manager. interval is the tick period; baseTTL is the level 0→1 time.
func New(store state.Store, out engine.Sender, interval, baseTTL time.Duration, log *slog.Logger) *Manager {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if baseTTL <= 0 {
		baseTTL = 10 * time.Minute
	}
	return &Manager{
		store: store, out: out, log: log,
		interval: interval, baseTTL: baseTTL,
		online: map[string]player{},
	}
}

// Interval is how often the caller should invoke Tick.
func (m *Manager) Interval() time.Duration { return m.interval }

func okey(network, nick string) string { return strings.ToLower(network) + "|" + strings.ToLower(nick) }
func sheetKey(network, nick string) string {
	return "rpg:" + strings.ToLower(network) + ":" + strings.ToLower(nick)
}
func boardKey(network string) string { return "rpg:lvl:" + strings.ToLower(network) }

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
	if m.isOnline(msg.Network, msg.Nick) {
		pen := int64(len(msg.Text))
		if pen > talkCapSec {
			pen = talkCapSec
		}
		_, _ = m.store.HIncr(context.Background(), sheetKey(msg.Network, msg.Nick), "ttl", pen)
	}
	return false
}

func (m *Manager) command(msg engine.Message, fields []string) {
	if len(fields) >= 2 && strings.ToLower(fields[1]) == "top" {
		m.out.Say(msg.Network, msg.Channel, m.leaderboard(msg.Network))
		return
	}
	ctx := context.Background()
	key := sheetKey(msg.Network, msg.Nick)
	sheet, err := m.store.HGetAll(ctx, key)
	if err != nil {
		m.log.Warn("idlerpg read failed", "err", err)
		m.out.Say(msg.Network, msg.Channel, "the realm is unreachable right now.")
		return
	}
	if _, enrolled := sheet["level"]; !enrolled {
		_ = m.store.HSet(ctx, key, "level", 0)
		_ = m.store.HSet(ctx, key, "ttl", m.ttlFor(0))
		_, _ = m.store.ZIncr(ctx, boardKey(msg.Network), strings.ToLower(msg.Nick), 0)
		m.setOnline(msg.Network, msg.Nick, msg.Channel)
		m.out.Say(msg.Network, msg.Channel, "welcome to the grind, "+msg.Nick+". you're level 0 — now hush and idle.")
		return
	}
	m.setOnline(msg.Network, msg.Nick, msg.Channel)
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("%s — level %d, %s to the next. (stop talking, it hurts.)",
		msg.Nick, sheet["level"], dur(sheet["ttl"])))
}

func (m *Manager) leaderboard(network string) string {
	top, err := m.store.ZTop(context.Background(), boardKey(network), 5)
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
	sheet, err := m.store.HGetAll(context.Background(), sheetKey(ev.Network, ev.Nick))
	if err != nil {
		return
	}
	if _, enrolled := sheet["level"]; enrolled {
		m.setOnline(ev.Network, ev.Nick, ev.Channel)
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
		key := sheetKey(p.network, p.nick)
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
		_, _ = m.store.ZIncr(ctx, boardKey(p.network), strings.ToLower(p.nick), 1)
		m.out.Say(p.network, p.channel, fmt.Sprintf("✨ %s has attained level %d! the idle is strong with this one.", p.nick, lvl))
	}
}

func (m *Manager) ttlFor(level int64) int64 {
	secs := m.baseTTL.Seconds() * math.Pow(growth, float64(level))
	if math.IsInf(secs, 1) || secs > ttlCap {
		return ttlCap
	}
	return int64(secs)
}

func (m *Manager) setOnline(network, nick, channel string) {
	m.mu.Lock()
	m.online[okey(network, nick)] = player{network: network, nick: nick, channel: channel}
	m.mu.Unlock()
}

func (m *Manager) isOnline(network, nick string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.online[okey(network, nick)]
	return ok
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
