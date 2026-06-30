package idlerpg

import (
	"context"
	"fmt"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Enchanting is the high-end gold sink: at a market you can pay to push an
// equipped item up one rarity tier (common → … → legendary). Drops are random;
// enchanting is deterministic agency over your gear, with a steep, escalating
// price so a fortune in boss gold has somewhere to go.

// enchantCost prices bumping an item of `level` from `curRarity` to the next tier.
// Quadratic in the target tier so each step costs far more than the last.
func enchantCost(level, curRarity int64) int64 {
	target := curRarity + 1 // 1..4
	return (level + 5) * target * target * 25
}

// enchant handles !rpg enchant <slot> at a market.
func (m *Manager) enchant(msg engine.Message, fields []string) {
	if len(fields) < 3 || !isItemSlot(fields[2]) {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg enchant <slot>. slots: "+strings.Join(itemSlots, ", "))
		return
	}
	slot := strings.ToLower(fields[2])
	m.townService(msg, "market", func(ctx context.Context, pkey string, sheet map[string]int64, t *town) string {
		lvl := sheet[itemField(slot)]
		if lvl <= 0 {
			return "you have no " + slot + " to enchant — find or buy one first."
		}
		cur := sheet[rarityField(slot)]
		if int(cur) >= len(rarities)-1 {
			return "your " + slot + " is already legendary — it can ascend no further."
		}
		cost := enchantCost(lvl, cur)
		if sheet["gold"] < cost {
			return fmt.Sprintf("the artificers of %s want %dg to raise your %s to %s (you have %dg).",
				t.Name, cost, slot, rarityName(cur+1), sheet["gold"])
		}
		_, _ = m.store.HIncr(ctx, sheetKey(pkey), "gold", -cost)
		_ = m.store.HSet(ctx, sheetKey(pkey), rarityField(slot), cur+1)

		p := player{network: msg.Network, nick: msg.Nick, channel: msg.Channel, key: pkey}
		newName := ""
		if rarities[cur+1].named {
			newName = m.magicName(slot, rarities[cur+1].name == "legendary")
			_ = m.store.SetStr(ctx, nameKey(pkey, slot), newName)
		}
		if rarities[cur+1].name == "legendary" {
			m.awardFeat(ctx, p, 1<<4) // Treasure Hunter
			m.bumpStat("legendaries", 1)
		}

		line := fmt.Sprintf("✨ %s enchants their %s to %s", msg.Nick, slot, rarityName(cur+1))
		if newName != "" {
			line += " — “" + newName + "”"
		}
		m.record(line + "!") // feed-worthy; the town service Says the return value
		return line + fmt.Sprintf(" for %dg.", cost)
	})
}
