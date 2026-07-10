package idlerpg

import (
	"context"
	"fmt"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Feats are one-time achievements — milestones a character crosses exactly once
// (first kill, first boss, 1000 gold, …). They're stored as a bitmask in the
// "feats" sheet field, announced the moment they're earned, and badged on the
// dashboard. Purely additive: existing characters earn theirs as they next cross
// a threshold.

type featDef struct {
	bit  int64
	name string
}

// feats, in display order. Bits are stable — append new feats, never reorder.
var feats = []featDef{
	{1 << 0, "First Blood"},
	{1 << 1, "Centurion (100 kills)"},
	{1 << 2, "Warlord (1000 kills)"},
	{1 << 3, "Giant-Slayer (a boss falls)"},
	{1 << 4, "Treasure Hunter (a legendary)"},
	{1 << 5, "Deep Pockets (1000 gold)"},
	{1 << 6, "Exterminator (5000 kills)"},
	{1 << 7, "Dragon-Hoard (10000 gold)"},
	{1 << 8, "Delver (a dungeon cleared)"},
	{1 << 9, "Guildmaster (founded a guild)"},
}

func featName(bit int64) string {
	for _, f := range feats {
		if f.bit == bit {
			return f.name
		}
	}
	return ""
}

// featList decodes a feats bitmask into the earned feat names, in display order.
func featList(mask int64) []string {
	var out []string
	for _, f := range feats {
		if mask&f.bit != 0 {
			out = append(out, f.name)
		}
	}
	return out
}

// awardFeat sets a feat bit if it isn't already, announcing the unlock once.
func (m *Manager) awardFeat(ctx context.Context, p player, bit int64) {
	sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	if sheet["feats"]&bit != 0 {
		return // already earned
	}
	_ = m.store.HSet(ctx, sheetKey(p.key), "feats", sheet["feats"]|bit)
	m.drama(p.network, p.channel, fmt.Sprintf("🎖️ %s earns a feat — %s!", p.nick, featName(bit)))
}

// checkCombatFeats awards the kill/gold/boss milestones after a winning fight.
// Each award is idempotent (bitmask-guarded), so calling it every kill is fine.
func (m *Manager) checkCombatFeats(ctx context.Context, p player, boss bool) {
	s, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	if s["kills"] >= 1 {
		m.awardFeat(ctx, p, 1<<0)
	}
	if s["kills"] >= 100 {
		m.awardFeat(ctx, p, 1<<1)
	}
	if s["kills"] >= 1000 {
		m.awardFeat(ctx, p, 1<<2)
	}
	if s["kills"] >= 5000 {
		m.awardFeat(ctx, p, 1<<6)
	}
	if boss {
		m.awardFeat(ctx, p, 1<<3)
	}
	if s["gold"] >= 1000 {
		m.awardFeat(ctx, p, 1<<5)
	}
	if s["gold"] >= 10000 {
		m.awardFeat(ctx, p, 1<<7)
	}
}

// featsStatus answers !rpg feats — the sender's earned achievements or a named other's.
func (m *Manager) featsStatus(msg engine.Message, fields []string) string {
	ctx := context.Background()
	name := msg.Nick
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if len(fields) >= 3 {
		name = fields[2]
		pkey = m.resolve(msg.Network, "", name)
	}
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	if len(sheet) == 0 {
		return name + " isn't playing. !rpg to start the grind."
	}
	earned := featList(sheet["feats"])
	if len(earned) == 0 {
		return name + " has earned no feats yet — go make a name for yourself."
	}
	return fmt.Sprintf("🎖️ %s's feats (%d/%d): %s", name, len(earned), len(feats), strings.Join(earned, ", "))
}
