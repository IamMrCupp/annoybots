package idlerpg

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Quests are idlerpg.net's marquee event: the gods periodically draft a party of
// idle players onto a quest. There are two kinds:
//
//   - "time": survive a timer — everyone stays present and SILENT until it elapses.
//   - "map":  the party journeys across the realm to two waypoints in sequence.
//
// Either way, finishing speeds the whole party's clock; if any quester talks,
// parts, quits, is kicked, or changes nick, the quest collapses and the party is
// shoved backward. Only one quest runs at a time. State persists as a JSON blob in
// the shared store, so an active quest survives a bot restart and the web dashboard
// can draw it.

const (
	defaultQuestInterval = 6 * time.Hour // avg gap between quests
	defaultQuestDuration = time.Hour     // how long a "time" quest lasts
	questPartyMax        = 4             // most players drafted at once
	questPartyMin        = 2             // fewest needed to start

	mapSize = 500 // the realm is a mapSize×mapSize grid (map quests)
	mapStep = 70  // distance the party covers per tick on a map quest
)

// questFlavors are the (cosmetic) quest objectives announced to the channel.
var questFlavors = []string{
	"retrieve the Golden Idol of the Ancients from the Caverns of Boredom",
	"escort the Sacred Goat across the Bridge of Sighs",
	"recover the lost socks of the Demigod Lint",
	"stand very still and contemplate the Orb of Profound Idling",
	"deliver a strongly-worded letter to the Lich of Latency",
	"guard the Eternal Flamewar from being extinguished",
}

func questKey() string { return "rpg:quest" }

// quest is the active quest. Exported fields so it round-trips through JSON in the
// store. Members maps a character key to the display nick to announce.
type quest struct {
	Kind     string            `json:"kind"` // "time" or "map"
	Network  string            `json:"net"`
	Channel  string            `json:"chan"`
	Deadline int64             `json:"deadline"` // unix seconds ("time" quests)
	Members  map[string]string `json:"members"`  // key -> display nick
	Desc     string            `json:"desc"`

	// Map quests: the party travels from (X,Y) to waypoint 1 then waypoint 2.
	X     int `json:"x,omitempty"`
	Y     int `json:"y,omitempty"`
	X1    int `json:"x1,omitempty"`
	Y1    int `json:"y1,omitempty"`
	X2    int `json:"x2,omitempty"`
	Y2    int `json:"y2,omitempty"`
	Stage int `json:"stage,omitempty"` // 0 → heading to waypoint 1; 1 → heading to waypoint 2
}

// loadQuest rehydrates an in-flight quest from the store at startup.
func (m *Manager) loadQuest(ctx context.Context) {
	blob, err := m.store.GetStr(ctx, questKey())
	if err != nil || blob == "" {
		return
	}
	var q quest
	if json.Unmarshal([]byte(blob), &q) != nil {
		return
	}
	m.qmu.Lock()
	m.quest = &q
	m.qmu.Unlock()
}

func (m *Manager) saveQuest(ctx context.Context, q *quest) {
	if b, err := json.Marshal(q); err == nil {
		_ = m.store.SetStr(ctx, questKey(), string(b))
	}
}

// questTick is called every Tick: complete the quest if its timer elapsed, else
// occasionally start a new one.
func (m *Manager) questTick(ctx context.Context) {
	m.qmu.Lock()
	q := m.quest
	m.qmu.Unlock()

	if q != nil {
		switch q.Kind {
		case "map":
			m.advanceMapQuest(ctx)
		default: // "time"
			if m.now().Unix() >= q.Deadline {
				m.completeQuest(ctx)
			}
		}
		return
	}
	denom := int(m.questInterval / m.interval)
	if denom < 1 {
		denom = 1
	}
	if m.roll(denom) == 0 {
		m.startQuest(ctx)
	}
}

// startQuest drafts a party and launches a random quest kind (time or map). It
// needs at least questPartyMin online characters to bother.
func (m *Manager) startQuest(ctx context.Context) {
	party := m.draftParty()
	if len(party) < questPartyMin {
		return
	}
	if m.roll(2) == 0 {
		m.startMapQuest(ctx, party)
	} else {
		m.startTimeQuest(ctx, party)
	}
}

// newQuest builds the party-agnostic parts of a quest from a drafted party.
func (m *Manager) newQuest(kind string, party []player) *quest {
	q := &quest{
		Kind:    kind,
		Network: party[0].network,
		Channel: party[0].channel,
		Members: make(map[string]string, len(party)),
		Desc:    questFlavors[m.roll(len(questFlavors))],
	}
	for _, p := range party {
		q.Members[p.key] = p.nick
	}
	return q
}

// activate installs q as the current quest, persists it, and announces it.
func (m *Manager) activate(ctx context.Context, q *quest, announce string) {
	m.qmu.Lock()
	m.quest = q
	m.qmu.Unlock()
	m.saveQuest(ctx, q)
	m.out.Say(q.Network, q.Channel, announce)
}

// startTimeQuest launches a survive-the-timer quest.
func (m *Manager) startTimeQuest(ctx context.Context, party []player) {
	q := m.newQuest("time", party)
	q.Deadline = m.now().Add(m.questDuration).Unix()
	m.activate(ctx, q, fmt.Sprintf(
		"⚔️ a quest begins! %s have been chosen by the gods to %s. they return in %s — but only if every one of them stays put and SILENT.",
		strings.Join(nicksOf(party), ", "), q.Desc, dur(int64(m.questDuration/time.Second))))
}

// startMapQuest launches a journey across the realm to two waypoints in sequence.
func (m *Manager) startMapQuest(ctx context.Context, party []player) {
	q := m.newQuest("map", party)
	q.X, q.Y = m.roll(mapSize+1), m.roll(mapSize+1)
	q.X1, q.Y1 = m.roll(mapSize+1), m.roll(mapSize+1)
	q.X2, q.Y2 = m.roll(mapSize+1), m.roll(mapSize+1)
	q.Stage = 0
	m.activate(ctx, q, fmt.Sprintf(
		"🗺️ a quest begins! %s set out to %s — a journey from [%d,%d] to [%d,%d], then on to [%d,%d]. one stray word or wandering step and the party is lost.",
		strings.Join(nicksOf(party), ", "), q.Desc, q.X, q.Y, q.X1, q.Y1, q.X2, q.Y2))
}

// advanceMapQuest moves the party one step toward its current waypoint, advancing
// to the second leg on arrival and completing the quest when it reaches the end.
func (m *Manager) advanceMapQuest(ctx context.Context) {
	m.qmu.Lock()
	q := m.quest
	if q == nil || q.Kind != "map" {
		m.qmu.Unlock()
		return
	}
	tx, ty := q.X2, q.Y2
	if q.Stage == 0 {
		tx, ty = q.X1, q.Y1
	}
	nx, ny, reached := stepToward(q.X, q.Y, tx, ty, mapStep)
	q.X, q.Y = nx, ny
	reachedWaypoint, finished := false, false
	if reached {
		if q.Stage == 0 {
			q.Stage = 1
			reachedWaypoint = true
		} else {
			finished = true
		}
	}
	network, channel, x2, y2 := q.Network, q.Channel, q.X2, q.Y2
	m.qmu.Unlock()

	if finished {
		m.completeQuest(ctx)
		return
	}
	m.saveQuest(ctx, q)
	if reachedWaypoint {
		m.out.Say(network, channel, fmt.Sprintf(
			"🧭 the party reached the first waypoint and presses on toward [%d,%d].", x2, y2))
	}
}

// stepToward moves (x,y) up to step units toward (tx,ty); reached is true when it
// arrives (within one step).
func stepToward(x, y, tx, ty, step int) (int, int, bool) {
	dx, dy := tx-x, ty-y
	dist := math.Hypot(float64(dx), float64(dy))
	if dist <= float64(step) || dist == 0 {
		return tx, ty, true
	}
	nx := x + int(math.Round(float64(dx)*float64(step)/dist))
	ny := y + int(math.Round(float64(dy)*float64(step)/dist))
	return nx, ny, false
}

// draftParty snapshots the online set, dedups by character key, shuffles, and
// returns up to questPartyMax players.
func (m *Manager) draftParty() []player {
	seen := map[string]bool{}
	var pool []player
	for _, p := range m.snapshot() {
		if seen[p.key] {
			continue
		}
		seen[p.key] = true
		pool = append(pool, p)
	}
	// Fisher–Yates with the guarded rng, then take the head.
	for i := len(pool) - 1; i > 0; i-- {
		j := m.roll(i + 1)
		pool[i], pool[j] = pool[j], pool[i]
	}
	if len(pool) > questPartyMax {
		pool = pool[:questPartyMax]
	}
	return pool
}

func nicksOf(ps []player) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.nick
	}
	return out
}

// completeQuest rewards every quester (clock jumps forward) and clears the quest.
func (m *Manager) completeQuest(ctx context.Context) {
	m.qmu.Lock()
	q := m.quest
	m.quest = nil
	m.qmu.Unlock()
	if q == nil {
		return
	}
	_ = m.store.Del(ctx, questKey())
	for key := range q.Members {
		amt := m.pctOfTTL(ctx, key, 20, 25)
		_, _ = m.store.HIncr(ctx, sheetKey(key), "ttl", -amt)
	}
	headline := "the quest is complete"
	if q.Kind == "map" {
		headline = "the party reached its destination"
	}
	m.out.Say(q.Network, q.Channel, fmt.Sprintf(
		"🏆 %s! %s %s and return triumphant — the gods speed each of them toward their next level.",
		headline, strings.Join(questNicks(q), ", "), q.Desc))
}

// questViolation fails the active quest if key belongs to a quester. reason is a
// short verb phrase ("spoke up", "abandoned the party") for the announcement.
func (m *Manager) questViolation(ctx context.Context, key, nick, reason string) {
	m.qmu.Lock()
	q := m.quest
	if q == nil {
		m.qmu.Unlock()
		return
	}
	if _, ok := q.Members[key]; !ok {
		m.qmu.Unlock()
		return
	}
	m.quest = nil
	m.qmu.Unlock()

	_ = m.store.Del(ctx, questKey())
	for k := range q.Members {
		amt := m.pctOfTTL(ctx, k, 10, 15)
		_, _ = m.store.HIncr(ctx, sheetKey(k), "ttl", amt)
	}
	m.out.Say(q.Network, q.Channel, fmt.Sprintf(
		"💥 the quest is RUINED! %s %s — the gods are furious and fling the whole party (%s) backward.",
		nick, reason, strings.Join(questNicks(q), ", ")))
}

// questNicks lists the party's display nicks in a stable order.
func questNicks(q *quest) []string {
	out := make([]string, 0, len(q.Members))
	for _, n := range q.Members {
		out = append(out, n)
	}
	return out
}
