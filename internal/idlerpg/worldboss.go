package idlerpg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IamMrCupp/annoybots/internal/state"
)

// A world boss is a realm-wide raid event: rarely a colossus appears with a huge
// shared HP pool and a time limit, and every online idler automatically chips
// damage at it each tick. Slay it before the deadline and all participants share
// the spoils; let it run out and it departs. State is persisted (like a quest) so
// it survives a restart, and the dashboard shows a live HP bar. The single
// IdleRPG game bot drives it — damage is applied once per tick from its roster.

const (
	worldBossOdds     = 250 // ~1-in-N chance per tick a world boss appears (when none is active)
	worldBossDuration = 600 // seconds a world boss lingers before departing
)

// worldBosses are the colossi a raid draws from.
var worldBosses = []string{
	"Bahamut the World-Wyrm",
	"the Kraken Sovereign",
	"Acererak the Devourer",
	"the Tarrasque Ascendant",
	"Lolth, Demon Queen of Spiders",
	"Vecna, the Whispered One",
	"the Worldsmÿður, Devourer of Stars",
	"Orcus, Prince of the Undead",
	"the Elder Brain of Xul'tharas",
}

func bossKey() string { return "rpg:wboss" }

// WorldBossView is the dashboard's read-only view of an active raid.
type WorldBossView struct {
	Name    string
	HP      int64
	MaxHP   int64
	Pct     int64  // remaining HP as a percentage, for the bar
	Players int    // how many heroes have struck it
	TopName string // the current biggest contributor
	TopDmg  int64  // their damage so far
}

// ReadWorldBoss returns the active raid, or nil if none.
func ReadWorldBoss(ctx context.Context, store state.Store) (*WorldBossView, error) {
	blob, err := store.GetStr(ctx, bossKey())
	if err != nil || blob == "" {
		return nil, err
	}
	var b worldBoss
	if json.Unmarshal([]byte(blob), &b) != nil {
		return nil, nil
	}
	pct := int64(0)
	if b.MaxHP > 0 && b.HP > 0 {
		pct = b.HP * 100 / b.MaxHP
	}
	topKey, topDmg := topDamager(b.Damage)
	return &WorldBossView{
		Name: b.Name, HP: b.HP, MaxHP: b.MaxHP, Pct: pct, Players: len(b.Players),
		TopName: b.Players[topKey], TopDmg: topDmg,
	}, nil
}

// worldBoss is the active raid. Exported fields round-trip through JSON.
type worldBoss struct {
	Name     string            `json:"name"`
	HP       int64             `json:"hp"`
	MaxHP    int64             `json:"maxhp"`
	Deadline int64             `json:"deadline"` // unix seconds
	Network  string            `json:"net"`      // where it was raised / is announced
	Channel  string            `json:"chan"`
	Players  map[string]string `json:"players"` // participant key -> display nick
	Damage   map[string]int64  `json:"dmg"`     // participant key -> total damage dealt
	LastPct  int64             `json:"lastpct"` // last announced HP percentage milestone
}

// loadBoss rehydrates an in-flight raid from the store at startup.
func (m *Manager) loadBoss(ctx context.Context) {
	blob, err := m.store.GetStr(ctx, bossKey())
	if err != nil || blob == "" {
		return
	}
	var b worldBoss
	if json.Unmarshal([]byte(blob), &b) != nil {
		return
	}
	if b.Damage == nil { // tolerate pre-damage-tracking blobs
		b.Damage = map[string]int64{}
	}
	m.bmu.Lock()
	m.boss = &b
	m.bmu.Unlock()
}

func (m *Manager) saveBoss(ctx context.Context, b *worldBoss) {
	if blob, err := json.Marshal(b); err == nil {
		_ = m.store.SetStr(ctx, bossKey(), string(blob))
	}
}

func (m *Manager) clearBoss(ctx context.Context) {
	m.bmu.Lock()
	m.boss = nil
	m.bmu.Unlock()
	_ = m.store.Del(ctx, bossKey())
}

// maybeWorldBoss rarely raises a world boss when none is active and idlers are
// present. The announcement lands in a present player's channel.
func (m *Manager) maybeWorldBoss(ctx context.Context) {
	m.bmu.Lock()
	active := m.boss != nil
	m.bmu.Unlock()
	if active || m.roll(worldBossOdds) != 0 {
		return
	}
	if p, ok := m.randomOnline(); ok {
		m.spawnWorldBoss(ctx, p.network, p.channel)
	}
}

// spawnWorldBoss raises a colossus scaled to the online crowd.
func (m *Manager) spawnWorldBoss(ctx context.Context, network, channel string) {
	online := m.snapshot()
	if len(online) == 0 {
		return
	}
	var lvlSum int64
	for _, p := range online {
		s, _ := m.store.HGetAll(ctx, sheetKey(p.key))
		lvlSum += s["level"] + 1
	}
	hp := 200 + lvlSum*40 // beatable by the present crowd over many ticks
	b := &worldBoss{
		Name:     worldBosses[m.roll(len(worldBosses))],
		HP:       hp,
		MaxHP:    hp,
		Deadline: m.now().Unix() + worldBossDuration,
		Network:  network,
		Channel:  channel,
		Players:  map[string]string{},
		Damage:   map[string]int64{},
		LastPct:  100,
	}
	m.bmu.Lock()
	m.boss = b
	m.bmu.Unlock()
	m.saveBoss(ctx, b)
	m.drama(network, channel, fmt.Sprintf(
		"🐲 a WORLD BOSS rises — %s, %d HP! every idler strikes it each tick — bring it down before it departs!", b.Name, hp))
}

// worldBossDamage is one idler's per-tick contribution to a raid.
func worldBossDamage(sheet map[string]int64) int64 {
	return 1 + sheet["level"]/2 + itemSum(sheet)/3
}

// worldBossTick advances an active raid: applies the roster's damage, announces
// milestones, and resolves victory or the deadline. Called every Tick.
func (m *Manager) worldBossTick(ctx context.Context) {
	m.bmu.Lock()
	b := m.boss
	m.bmu.Unlock()
	if b == nil {
		return
	}
	if m.now().Unix() >= b.Deadline {
		m.drama(b.Network, b.Channel, fmt.Sprintf("🌑 %s departs unbroken — the realm exhales, and lives to fight another day.", b.Name))
		m.clearBoss(ctx)
		return
	}
	var total int64
	for _, p := range m.snapshot() {
		s, _ := m.store.HGetAll(ctx, sheetKey(p.key))
		dmg := worldBossDamage(s)
		total += dmg
		b.Players[p.key] = p.nick
		if b.Damage == nil {
			b.Damage = map[string]int64{}
		}
		b.Damage[p.key] += dmg
	}
	if total == 0 {
		return // nobody around to fight this tick
	}
	b.HP -= total
	pct := int64(0)
	if b.HP > 0 {
		pct = b.HP * 100 / b.MaxHP
	}
	for _, milestone := range []int64{75, 50, 25} {
		if b.LastPct > milestone && pct <= milestone {
			m.drama(b.Network, b.Channel, fmt.Sprintf("⚔️ %s is at %d%% HP — press the attack!", b.Name, milestone))
		}
	}
	b.LastPct = pct
	if b.HP <= 0 {
		m.rewardWorldBoss(ctx, b)
		m.clearBoss(ctx)
		return
	}
	m.saveBoss(ctx, b)
}

// rewardWorldBoss pays every participant when a raid is won.
func (m *Manager) rewardWorldBoss(ctx context.Context, b *worldBoss) {
	n := int64(len(b.Players))
	if n < 1 {
		n = 1
	}
	share := b.MaxHP/(n*2) + 100
	m.drama(b.Network, b.Channel, fmt.Sprintf(
		"🏆 %s is SLAIN by %d hero(es)! the spoils are shared — +%dg and a great leap forward to each.", b.Name, len(b.Players), share))
	topKey, topDmg := topDamager(b.Damage)
	for key, nick := range b.Players {
		_, _ = m.store.HIncr(ctx, sheetKey(key), "gold", share)
		_, _ = m.store.HIncr(ctx, sheetKey(key), "kills", 1)
		reward := m.pctOfTTL(ctx, key, 18, 28)
		_, _ = m.store.HIncr(ctx, sheetKey(key), "ttl", -reward)
		p := player{network: b.Network, nick: nick, channel: b.Channel, key: key}
		m.checkCombatFeats(ctx, p, true) // a world boss counts toward Giant-Slayer
	}
	m.bumpStat("bosses", 1)
	m.bumpStat("gold", share*n)
	// crown the top contributor with a bonus purse for landing the most damage.
	if topKey != "" && topDmg > 0 {
		bonus := share
		_, _ = m.store.HIncr(ctx, sheetKey(topKey), "gold", bonus)
		m.bumpStat("gold", bonus)
		m.drama(b.Network, b.Channel, fmt.Sprintf(
			"⭐ %s struck the hardest (%d damage) and claims the champion's purse — +%dg extra!", b.Players[topKey], topDmg, bonus))
	}
}

// topDamager returns the key and damage of the biggest contributor to a raid.
func topDamager(dmg map[string]int64) (string, int64) {
	var bestKey string
	var best int64
	for k, v := range dmg {
		if v > best || (v == best && k < bestKey) {
			bestKey, best = k, v
		}
	}
	return bestKey, best
}
