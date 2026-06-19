package idlerpg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Quests are idlerpg.net's marquee event: the gods periodically draft a party of
// idle players onto a timed quest. Finish it — which only means everyone stays
// present and SILENT until the timer runs out — and the whole party's clock jumps
// forward. If any quester talks, parts, quits, is kicked, or changes nick, the
// quest collapses and the party is shoved backward. Only one quest runs at a time.
//
// State persists as a JSON blob in the shared store, so an active quest survives a
// bot restart and is visible to the (future) web dashboard.

const (
	defaultQuestInterval = 6 * time.Hour // avg gap between quests
	defaultQuestDuration = time.Hour     // how long a quest lasts
	questPartyMax        = 4             // most players drafted at once
	questPartyMin        = 2             // fewest needed to start
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
	Kind     string            `json:"kind"` // "time" today; "map" later
	Network  string            `json:"net"`
	Channel  string            `json:"chan"`
	Deadline int64             `json:"deadline"` // unix seconds
	Members  map[string]string `json:"members"`  // key -> display nick
	Desc     string            `json:"desc"`
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
		if m.now().Unix() >= q.Deadline {
			m.completeQuest(ctx)
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

// startQuest drafts up to questPartyMax distinct online characters and announces
// the quest. It needs at least questPartyMin to bother.
func (m *Manager) startQuest(ctx context.Context) {
	party := m.draftParty()
	if len(party) < questPartyMin {
		return
	}
	q := &quest{
		Kind:     "time",
		Network:  party[0].network,
		Channel:  party[0].channel,
		Deadline: m.now().Add(m.questDuration).Unix(),
		Members:  make(map[string]string, len(party)),
		Desc:     questFlavors[m.roll(len(questFlavors))],
	}
	for _, p := range party {
		q.Members[p.key] = p.nick
	}
	m.qmu.Lock()
	m.quest = q
	m.qmu.Unlock()
	m.saveQuest(ctx, q)

	m.out.Say(q.Network, q.Channel, fmt.Sprintf(
		"⚔️ a quest begins! %s have been chosen by the gods to %s. they return in %s — but only if every one of them stays put and SILENT.",
		strings.Join(nicksOf(party), ", "), q.Desc, dur(int64(m.questDuration/time.Second))))
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
	m.out.Say(q.Network, q.Channel, fmt.Sprintf(
		"🏆 the quest is complete! %s %s and return triumphant — the gods speed each of them toward their next level.",
		strings.Join(questNicks(q), ", "), q.Desc))
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
