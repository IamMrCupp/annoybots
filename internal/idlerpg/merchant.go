package idlerpg

import (
	"context"
	"fmt"
)

// The wandering merchant is a purely positive random event: a passing trader
// finds an idler and gifts them gold, a healing draught, or a shortcut toward the
// next level. A little kindness amid the monsters and calamities.
func (m *Manager) merchant(ctx context.Context, p player) {
	sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	switch m.roll(3) {
	case 0: // a purse of gold
		g := int64(20 + m.roll(int(sheet["level"])*5+30))
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "gold", g)
		m.bumpStat("gold", g)
		m.drama(p.network, p.channel, fmt.Sprintf(
			"🧭 a wandering merchant presses a purse of %dg into %s's hands and is gone.", g, p.nick))
	case 1: // a healing draught
		n, _ := m.store.HIncr(ctx, sheetKey(p.key), "pots", 1)
		m.drama(p.network, p.channel, fmt.Sprintf(
			"🧭 a wandering merchant gifts %s a healing draught for the road (%d in the pack).", p.nick, n))
	default: // a shortcut toward the next level
		amt := m.pctOfTTL(ctx, p.key, 4, 8)
		if amt < 1 {
			amt = int64(10 + m.roll(30))
		}
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", -amt)
		m.drama(p.network, p.channel, fmt.Sprintf(
			"🧭 a wandering merchant points %s down a hidden shortcut — %ds closer to the next level.", p.nick, amt))
	}
}
