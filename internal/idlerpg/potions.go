package idlerpg

import (
	"context"
	"fmt"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Healing draughts are a portable gold sink: buy them at a market, carry a stack,
// and quaff one ANYWHERE to restore full HP — the only way to pick yourself up
// after being downed out in the wild, far from a temple. Count lives in the
// "pots" sheet field.

func potionPrice(level int64) int64 { return 15 + level*2 }

// buyPotion sells one healing draught at a market.
func (m *Manager) buyPotion(msg engine.Message) {
	m.townService(msg, "market", func(ctx context.Context, pkey string, sheet map[string]int64, _ *town) string {
		price := potionPrice(sheet["level"])
		if sheet["gold"] < price {
			return fmt.Sprintf("a healing draught costs %dg (you have %dg).", price, sheet["gold"])
		}
		_, _ = m.store.HIncr(ctx, sheetKey(pkey), "gold", -price)
		n, _ := m.store.HIncr(ctx, sheetKey(pkey), "pots", 1)
		return fmt.Sprintf("%s buys a healing draught for %dg — %d in your pack.", msg.Nick, price, n)
	})
}

// quaff drinks a healing draught to restore full HP. Works anywhere, which is the
// whole point: it rescues you when you're downed away from a temple.
func (m *Manager) quaff(msg engine.Message) {
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	if len(sheet) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	if sheet["pots"] <= 0 {
		m.out.Say(msg.Network, msg.Channel, msg.Nick+" has no healing draughts. buy one at a market (!rpg buy potion).")
		return
	}
	class, _ := m.store.GetStr(ctx, classKey(pkey))
	if curHP(sheet, class) >= maxHP(sheet, class) && !poisoned(sheet) {
		m.out.Say(msg.Network, msg.Channel, msg.Nick+" is already at full health — no need to quaff.")
		return
	}
	_ = m.store.HSet(ctx, sheetKey(pkey), "dmg", 0)
	m.curePoison(ctx, pkey) // a draught also purges venom
	n, _ := m.store.HIncr(ctx, sheetKey(pkey), "pots", -1)
	cured := ""
	if poisoned(sheet) {
		cured = " the venom is purged, and"
	}
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf(
		"🧪 %s quaffs a healing draught —%s they're restored to full HP. (%d left)", msg.Nick, cured, n))
}
