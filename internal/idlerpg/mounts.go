package idlerpg

import (
	"context"
	"fmt"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Mounts are steeds bought at a market. A mount doubles your travel speed toward
// a town, so you reach the inn / market / temple in half the ticks — real value
// now that town services (heal, enchant, bless, revive) matter. Stored as a
// single string key (the steed's name).

const mountBonus = worldStep // a mount adds this to the per-tick travel step (≈2×)

var mountKinds = []string{
	"a swift courser",
	"a caparisoned warhorse",
	"a dire elk",
	"a giant riding lizard",
	"a nightmare steed",
}

func mountKey(player string) string { return "rpg:mount:" + player }

func mountPrice(level int64) int64 { return 400 + level*8 }

// mountOf returns a character's steed name, or "" if unmounted.
func (m *Manager) mountOf(ctx context.Context, key string) string {
	name, _ := m.store.GetStr(ctx, mountKey(key))
	return name
}

// buyMount sells a steed at a market.
func (m *Manager) buyMount(msg engine.Message) {
	m.townService(msg, "market", func(ctx context.Context, pkey string, sheet map[string]int64, t *town) string {
		if m.mountOf(ctx, pkey) != "" {
			return msg.Nick + " already keeps a mount in the stable."
		}
		price := mountPrice(sheet["level"])
		if sheet["gold"] < price {
			return fmt.Sprintf("a mount costs %dg at the %s stables (you have %dg).", price, t.Name, sheet["gold"])
		}
		mount := mountKinds[m.roll(len(mountKinds))]
		_, _ = m.store.HIncr(ctx, sheetKey(pkey), "gold", -price)
		_ = m.store.SetStr(ctx, mountKey(pkey), mount)
		return fmt.Sprintf("🐎 %s buys %s for %dg — travel to towns is now twice as swift.", msg.Nick, mount, price)
	})
}

// mountStatus answers !rpg mount.
func (m *Manager) mountStatus(msg engine.Message) string {
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if s, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(s) == 0 {
		return "you're not playing. !rpg to start the grind."
	}
	mount := m.mountOf(ctx, pkey)
	if mount == "" {
		return msg.Nick + " travels on foot. buy a mount at a market (!rpg buy mount)."
	}
	return fmt.Sprintf("🐎 %s rides %s — double travel speed.", msg.Nick, mount)
}
