package idlerpg

import (
	"context"
	"fmt"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Status effects are temporary conditions measured in ticks. Poison is a
// damage-over-time inflicted by venomous foes: it lives as a countdown in the
// "poison" sheet field, saps a little HP each tick in Tick, and is cured by any
// full heal (a quaffed draught, an inn rest, a temple revive). It makes the
// swamp/forest venom-bearers genuinely dangerous and gives potions a second job.

// venomous foes envenom you when they land a telling blow.
var venomous = map[string]bool{
	"a will-o'-wisp":    true,
	"a green hag":       true,
	"a bog zombie":      true,
	"a sahuagin raider": true,
	"a wyvern":          true,
	"a manticore":       true,
}

const poisonTicks = 3 // how many ticks a fresh poisoning lasts

// poisonDamage is the per-tick HP loss from venom, scaling mildly with level.
func poisonDamage(level int64) int64 { return 1 + level/12 }

// blessAtk / blessDmg are the combat bonuses a blessing grants in monster fights.
const (
	blessAtk = 2
	blessDmg = 1
)

// tickStatus applies active status effects for one tick and counts them down.
func (m *Manager) tickStatus(ctx context.Context, p player) {
	sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	if sheet["poison"] > 0 {
		m.damage(ctx, p.key, poisonDamage(sheet["level"]))
		if left, _ := m.store.HIncr(ctx, sheetKey(p.key), "poison", -1); left <= 0 {
			m.out.Say(p.network, p.channel, fmt.Sprintf("🫧 the venom in %s's blood finally fades.", p.nick))
		}
	}
	if sheet["bless"] > 0 {
		if left, _ := m.store.HIncr(ctx, sheetKey(p.key), "bless", -1); left <= 0 {
			m.out.Say(p.network, p.channel, fmt.Sprintf("🕊️ the blessing upon %s lifts.", p.nick))
		}
	}
}

// blessed reports whether a sheet currently carries a blessing.
func blessed(sheet map[string]int64) bool { return sheet["bless"] > 0 }

const blessTicks = 8 // how many ticks a bought blessing lasts

// bless handles !rpg bless at a temple — buy a temporary combat blessing.
func (m *Manager) bless(msg engine.Message) {
	m.townService(msg, "temple", func(ctx context.Context, pkey string, sheet map[string]int64, t *town) string {
		if blessed(sheet) {
			return msg.Nick + " already walks under a blessing."
		}
		price := 25 + sheet["level"]*3
		if sheet["gold"] < price {
			return fmt.Sprintf("the priests of %s ask %dg for a blessing (you have %dg).", t.Name, price, sheet["gold"])
		}
		_, _ = m.store.HIncr(ctx, sheetKey(pkey), "gold", -price)
		_ = m.store.HSet(ctx, sheetKey(pkey), "bless", blessTicks)
		return fmt.Sprintf("🕊️ the temple of %s blesses %s — +%d attack, +%d damage in the wilds for a while. -%dg.",
			t.Name, msg.Nick, blessAtk, blessDmg, price)
	})
}

// applyPoison envenoms a player for n ticks (refreshing to the longer duration).
func (m *Manager) applyPoison(ctx context.Context, p player, n int64) {
	if sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key)); sheet["poison"] >= n {
		return
	}
	_ = m.store.HSet(ctx, sheetKey(p.key), "poison", n)
	m.drama(p.network, p.channel, fmt.Sprintf("☠️ %s is poisoned — the venom will sap them until it wears off (or they heal).", p.nick))
}

// curePoison clears any venom; called whenever a character is healed to full.
func (m *Manager) curePoison(ctx context.Context, key string) {
	_ = m.store.HSet(ctx, sheetKey(key), "poison", 0)
}

// poisoned reports whether a sheet currently carries venom.
func poisoned(sheet map[string]int64) bool { return sheet["poison"] > 0 }
