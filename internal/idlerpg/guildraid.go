package idlerpg

import (
	"context"
	"fmt"
)

// A guild raid is the world boss made personal: a guild spends its vault to call
// down a champion, and only that guild's members can fight it. Damage is tracked
// per member, the spoils are split, and the biggest contributor takes a bonus —
// so a guild's gold turns into a shared story instead of sitting in a chest.
//
// The raid rides inside the guild blob, so it persists with everything else.

const (
	guildRaidCost     = 1000 // vault gold to call a raid
	guildRaidDuration = 900  // seconds before the champion departs
)

var guildRaidFoes = []string{
	"the Iron Sepulchre",
	"Ymirrath, the Frost Colossus",
	"the Choir of Ashes",
	"Vharos, Herald of the Deep",
	"the Sunken Titan",
	"Malgrith the Oathbreaker",
}

// guildRaid is one guild's active raid. Exported fields round-trip through JSON.
type guildRaid struct {
	Name     string            `json:"name"`
	HP       int64             `json:"hp"`
	MaxHP    int64             `json:"maxhp"`
	Deadline int64             `json:"deadline"`
	Network  string            `json:"net"`
	Channel  string            `json:"chan"`
	Damage   map[string]int64  `json:"dmg"`
	Names    map[string]string `json:"names"`
	LastPct  int64             `json:"lastpct"`
}

// callRaid answers "!rpg guild raid": spend the vault to summon a champion.
func (m *Manager) callRaid(ctx context.Context, network, channel, nick, key string) string {
	g := m.guildOf(key)
	if g == nil {
		return nick + " belongs to no guild."
	}
	m.gmu.Lock()
	if g.Raid != nil {
		name := g.Raid.Name
		m.gmu.Unlock()
		return fmt.Sprintf("%s is already fighting %s.", g.Name, name)
	}
	if g.Vault < guildRaidCost {
		vault, gname := g.Vault, g.Name
		m.gmu.Unlock()
		return fmt.Sprintf("calling a raid costs %dg from the vault — %s holds %dg.", guildRaidCost, gname, vault)
	}
	g.Vault -= guildRaidCost
	members := append([]string(nil), g.Members...)
	gname := g.Name
	m.gmu.Unlock()

	// Scale the champion to the guild's present strength so it's a real fight.
	var lvlSum int64
	for _, member := range members {
		s, _ := m.store.HGetAll(ctx, sheetKey(member))
		lvlSum += s["level"] + 1
	}
	hp := 300 + lvlSum*30
	r := &guildRaid{
		Name:     guildRaidFoes[m.roll(len(guildRaidFoes))],
		HP:       hp,
		MaxHP:    hp,
		Deadline: m.now().Unix() + guildRaidDuration,
		Network:  network,
		Channel:  channel,
		Damage:   map[string]int64{},
		Names:    map[string]string{},
		LastPct:  100,
	}
	m.gmu.Lock()
	g.Raid = r
	m.gmu.Unlock()
	m.saveGuilds(ctx)

	m.drama(network, channel, fmt.Sprintf(
		"🛡️⚔️ %s calls a GUILD RAID — %s answers with %d HP! only %s may strike it.",
		gname, r.Name, hp, gname))
	return ""
}

// guildRaidTick advances every guild's raid: guildmates present chip damage,
// milestones are announced, and victory or the deadline resolves it.
func (m *Manager) guildRaidTick(ctx context.Context) {
	type active struct {
		guild *guild
		raid  *guildRaid
	}
	var running []active
	m.gmu.Lock()
	for _, g := range m.book().Guilds {
		if g.Raid != nil {
			running = append(running, active{g, g.Raid})
		}
	}
	m.gmu.Unlock()
	if len(running) == 0 {
		return
	}
	roster := m.snapshot()
	changed := false

	for _, a := range running {
		r := a.raid
		if m.now().Unix() >= r.Deadline {
			m.drama(r.Network, r.Channel, fmt.Sprintf(
				"🌑 %s departs unbroken — %s's vault bought only a story.", r.Name, a.guild.Name))
			m.gmu.Lock()
			a.guild.Raid = nil
			m.gmu.Unlock()
			changed = true
			continue
		}
		// Only this guild's online members land blows.
		var total int64
		for _, p := range roster {
			if m.guildOf(p.key) != a.guild {
				continue
			}
			s, _ := m.store.HGetAll(ctx, sheetKey(p.key))
			dmg := worldBossDamage(s)
			total += dmg
			r.Damage[p.key] += dmg
			r.Names[p.key] = p.nick
		}
		if total == 0 {
			continue // no guildmates around this tick
		}
		r.HP -= total
		changed = true
		pct := int64(0)
		if r.HP > 0 {
			pct = r.HP * 100 / r.MaxHP
		}
		for _, milestone := range []int64{75, 50, 25} {
			if r.LastPct > milestone && pct <= milestone {
				m.drama(r.Network, r.Channel, fmt.Sprintf("⚔️ %s is at %d%% — %s presses on!", r.Name, milestone, a.guild.Name))
			}
		}
		r.LastPct = pct
		if r.HP <= 0 {
			m.rewardGuildRaid(ctx, a.guild, r)
			m.gmu.Lock()
			a.guild.Raid = nil
			m.gmu.Unlock()
		}
	}
	if changed {
		m.saveGuilds(ctx)
	}
}

// rewardGuildRaid splits the spoils: everyone who struck it shares gold and
// progress, and the top contributor takes a champion's cut.
func (m *Manager) rewardGuildRaid(ctx context.Context, g *guild, r *guildRaid) {
	topKey, topDmg := topDamager(r.Damage)
	purse := r.MaxHP // the champion's hoard scales with how tough it was
	share := purse
	if len(r.Damage) > 0 {
		share = purse / int64(len(r.Damage))
	}
	for key := range r.Damage {
		_, _ = m.store.HIncr(ctx, sheetKey(key), "gold", share)
		m.bumpStat("gold", share)
		reward := m.pctOfTTL(ctx, key, 12, 20)
		_, _ = m.store.HIncr(ctx, sheetKey(key), "ttl", -reward)
	}
	m.drama(r.Network, r.Channel, fmt.Sprintf(
		"🏆 %s falls to %s! %d guildmates split %dg and a march toward glory.",
		r.Name, g.Name, len(r.Damage), share*int64(len(r.Damage))))
	if topKey != "" {
		bonus := purse / 2
		_, _ = m.store.HIncr(ctx, sheetKey(topKey), "gold", bonus)
		m.bumpStat("gold", bonus)
		m.drama(r.Network, r.Channel, fmt.Sprintf(
			"⭐ %s struck hardest (%d damage) — a champion's cut of %dg.", r.Names[topKey], topDmg, bonus))
	}
	// The guild keeps a share of the hoard in its vault.
	m.gmu.Lock()
	g.Vault += purse / 4
	m.gmu.Unlock()
}

// raidStatus describes a guild's active raid for !rpg guild.
func raidStatus(g *guild, now int64) string {
	if g.Raid == nil {
		return ""
	}
	pct := int64(0)
	if g.Raid.MaxHP > 0 && g.Raid.HP > 0 {
		pct = g.Raid.HP * 100 / g.Raid.MaxHP
	}
	return fmt.Sprintf(" · ⚔️ raiding %s (%d%%, %s left)", g.Raid.Name, pct, dur(g.Raid.Deadline-now))
}
