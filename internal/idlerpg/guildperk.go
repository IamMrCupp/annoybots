package idlerpg

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Vault perks turn a guild's pooled gold into something lasting. Each perk levels
// up independently, costs more the higher it climbs, and quietly benefits every
// member forever after — the reason to keep depositing.

type perkDef struct {
	name string
	desc string
	base int64 // gold for the first level; level N costs base*(N+1)
	max  int64
}

var guildPerkList = []perkDef{
	{"swiftness", "guildmates idle faster (+2% per level)", 800, 5},
	{"fortune", "monster gold is richer for members (+5% per level)", 600, 5},
	{"might", "guildmates strike harder (+1 attack per level)", 1000, 3},
}

func perkByName(name string) (perkDef, bool) {
	for _, p := range guildPerkList {
		if p.name == strings.ToLower(strings.TrimSpace(name)) {
			return p, true
		}
	}
	return perkDef{}, false
}

// perkCost is what the next level of a perk costs.
func perkCost(p perkDef, level int64) int64 { return p.base * (level + 1) }

// perkLevel reads a guild's level in a perk (0 when unbought). Callers must not
// hold gmu.
func (m *Manager) perkLevel(key, perk string) int64 {
	g := m.guildOf(key)
	if g == nil {
		return 0
	}
	m.gmu.Lock()
	defer m.gmu.Unlock()
	return g.Perks[perk]
}

// guildSwiftness is the extra idle-speed percentage a character's guild perks
// grant, on top of the fellowship and guild-together bonuses.
func (m *Manager) guildSwiftness(key string) int64 { return m.perkLevel(key, "swiftness") * 2 }

// guildMight is the attack bonus a character's guild grants in monster fights.
func (m *Manager) guildMight(key string) int64 { return m.perkLevel(key, "might") }

// guildFortune scales a gold reward by the guild's fortune perk.
func (m *Manager) guildFortune(key string, gold int64) int64 {
	lvl := m.perkLevel(key, "fortune")
	if lvl == 0 {
		return gold
	}
	return gold * (100 + lvl*5) / 100
}

// perkCmd handles "!rpg guild perk [name]" — list the perks, or buy the next level.
func (m *Manager) perkCmd(ctx context.Context, nick, key string, fields []string) string {
	g := m.guildOf(key)
	if g == nil {
		return nick + " belongs to no guild."
	}
	if len(fields) < 4 { // list them
		m.gmu.Lock()
		parts := make([]string, 0, len(guildPerkList))
		for _, p := range guildPerkList {
			lvl := g.Perks[p.name]
			if lvl >= p.max {
				parts = append(parts, fmt.Sprintf("%s %d/%d (maxed)", p.name, lvl, p.max))
				continue
			}
			parts = append(parts, fmt.Sprintf("%s %d/%d — next %dg", p.name, lvl, p.max, perkCost(p, lvl)))
		}
		vault, gname := g.Vault, g.Name
		m.gmu.Unlock()
		return fmt.Sprintf("🛡 %s perks — %s · vault %dg", gname, strings.Join(parts, " · "), vault)
	}

	p, ok := perkByName(fields[3])
	if !ok {
		names := make([]string, len(guildPerkList))
		for i, d := range guildPerkList {
			names[i] = d.name
		}
		sort.Strings(names)
		return "no such perk. try: " + strings.Join(names, ", ")
	}
	m.gmu.Lock()
	if g.Perks == nil {
		g.Perks = map[string]int64{}
	}
	lvl := g.Perks[p.name]
	if lvl >= p.max {
		gname := g.Name
		m.gmu.Unlock()
		return fmt.Sprintf("%s has already mastered %s (%d/%d).", gname, p.name, lvl, p.max)
	}
	cost := perkCost(p, lvl)
	if g.Vault < cost {
		vault, gname := g.Vault, g.Name
		m.gmu.Unlock()
		return fmt.Sprintf("%s costs %dg — %s's vault holds %dg. !rpg guild deposit <gold>", p.name, cost, gname, vault)
	}
	g.Vault -= cost
	g.Perks[p.name] = lvl + 1
	gname, vault := g.Name, g.Vault
	m.gmu.Unlock()
	m.saveGuilds(ctx)
	return fmt.Sprintf("🛡 %s raises %s to %d/%d for %dg — %s. vault: %dg",
		gname, p.name, lvl+1, p.max, cost, p.desc, vault)
}

// perkSummary renders a guild's bought perks for !rpg guild / the dashboard.
func perkSummary(perks map[string]int64) string {
	if len(perks) == 0 {
		return ""
	}
	var parts []string
	for _, p := range guildPerkList {
		if lvl := perks[p.name]; lvl > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", p.name, lvl))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " · perks: " + strings.Join(parts, ", ")
}

// GuildPerks lists every perk and what it does, for help and the dashboard.
func GuildPerks() []HelpItem {
	out := make([]HelpItem, 0, len(guildPerkList))
	for _, p := range guildPerkList {
		out = append(out, HelpItem{Cmd: p.name, Desc: p.desc})
	}
	return out
}
