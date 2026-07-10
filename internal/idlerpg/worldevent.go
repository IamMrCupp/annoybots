package idlerpg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IamMrCupp/annoybots/internal/state"
)

// World events are periodic realm-wide modifiers — a Blood Moon, a Harvest
// Festival, a Storm of Fate — that change the rules for everyone for a while.
// They add rhythm to the long idle loop. Persisted like a quest or a raid, so
// they survive a restart, and surfaced in !rpg info and on the dashboard.

const (
	worldEventOdds     = 400  // ~1-in-N chance per tick one begins (when none is active)
	worldEventDuration = 1800 // seconds a world event lasts
)

func eventKey() string { return "rpg:wevent" }

// worldEvent is the active realm modifier. Exported fields round-trip through JSON.
type worldEvent struct {
	Kind     string `json:"kind"` // "bloodmoon" | "harvest" | "tempest"
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Deadline int64  `json:"deadline"` // unix seconds
	Network  string `json:"net"`
	Channel  string `json:"chan"`
}

// worldEventKinds are the modifiers a world event draws from.
var worldEventKinds = []worldEvent{
	{Kind: "bloodmoon", Name: "a Blood Moon", Desc: "monsters prowl in far greater numbers"},
	{Kind: "harvest", Name: "the Harvest Festival", Desc: "every coin earned is worth half again"},
	{Kind: "tempest", Name: "a Storm of Fate", Desc: "the gods meddle far more often"},
}

// WorldEventView is the dashboard's read-only view of an active world event.
type WorldEventView struct {
	Name string
	Desc string
	Left int64 // seconds remaining
}

// ReadWorldEvent returns the active world event, or nil if none.
func ReadWorldEvent(ctx context.Context, store state.Store, now int64) (*WorldEventView, error) {
	blob, err := store.GetStr(ctx, eventKey())
	if err != nil || blob == "" {
		return nil, err
	}
	var e worldEvent
	if json.Unmarshal([]byte(blob), &e) != nil {
		return nil, nil
	}
	left := e.Deadline - now
	if left < 0 {
		left = 0
	}
	return &WorldEventView{Name: e.Name, Desc: e.Desc, Left: left}, nil
}

func (m *Manager) loadWorldEvent(ctx context.Context) {
	blob, err := m.store.GetStr(ctx, eventKey())
	if err != nil || blob == "" {
		return
	}
	var e worldEvent
	if json.Unmarshal([]byte(blob), &e) != nil {
		return
	}
	m.emu.Lock()
	m.wevent = &e
	m.emu.Unlock()
}

func (m *Manager) saveWorldEvent(ctx context.Context, e *worldEvent) {
	if blob, err := json.Marshal(e); err == nil {
		_ = m.store.SetStr(ctx, eventKey(), string(blob))
	}
}

func (m *Manager) clearWorldEvent(ctx context.Context) {
	m.emu.Lock()
	m.wevent = nil
	m.emu.Unlock()
	_ = m.store.Del(ctx, eventKey())
}

// eventKind returns the active world event's kind, or "" when none is running.
func (m *Manager) eventKind() string {
	m.emu.Lock()
	defer m.emu.Unlock()
	if m.wevent == nil {
		return ""
	}
	return m.wevent.Kind
}

// worldEventTick ends an expired world event, or rarely begins a new one.
func (m *Manager) worldEventTick(ctx context.Context) {
	m.emu.Lock()
	e := m.wevent
	m.emu.Unlock()
	if e != nil {
		if m.now().Unix() >= e.Deadline {
			m.drama(e.Network, e.Channel, fmt.Sprintf("🌙 %s passes. the realm returns to its usual cruelty.", e.Name))
			m.clearWorldEvent(ctx)
		}
		return
	}
	if m.roll(worldEventOdds) != 0 {
		return
	}
	p, ok := m.randomOnline()
	if !ok {
		return
	}
	pick := worldEventKinds[m.roll(len(worldEventKinds))]
	ev := &worldEvent{
		Kind: pick.Kind, Name: pick.Name, Desc: pick.Desc,
		Deadline: m.now().Unix() + worldEventDuration,
		Network:  p.network, Channel: p.channel,
	}
	m.emu.Lock()
	m.wevent = ev
	m.emu.Unlock()
	m.saveWorldEvent(ctx, ev)
	m.drama(ev.Network, ev.Channel, fmt.Sprintf("🌕 %s falls over the realm — %s! (for the next %s)", ev.Name, ev.Desc, dur(worldEventDuration)))
}

// harvestGold applies the Harvest Festival bonus to a gold reward.
func (m *Manager) harvestGold(g int64) int64 {
	if m.eventKind() == "harvest" {
		return g * 3 / 2
	}
	return g
}
