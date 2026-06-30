package idlerpg

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Towns turn the world-map landmarks into real stops: travel to one, and while
// you're there you can rest (inn), buy gear (market), or revive (temple). This is
// where the gold monsters drop finally gets spent.

// dist returns the distance between two points (rounded).
func dist(x1, y1, x2, y2 int64) int64 {
	return int64(math.Round(math.Hypot(float64(x1-x2), float64(y1-y2))))
}

// atTown returns the town the position is standing in (within townRadius), or nil.
func atTown(x, y int64) *town {
	for i := range towns {
		if dist(x, y, int64(towns[i].X), int64(towns[i].Y)) <= townRadius {
			return &towns[i]
		}
	}
	return nil
}

// nearestTown returns the closest town and the distance to it.
func nearestTown(x, y int64) (town, int64) {
	best, bestD := towns[0], dist(x, y, int64(towns[0].X), int64(towns[0].Y))
	for _, t := range towns[1:] {
		if d := dist(x, y, int64(t.X), int64(t.Y)); d < bestD {
			best, bestD = t, d
		}
	}
	return best, bestD
}

// townByName looks up a town by (case-insensitive) name, returning its 0-based index.
func townByName(name string) (town, int, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for i, t := range towns {
		if strings.ToLower(t.Name) == name {
			return t, i, true
		}
	}
	return town{}, 0, false
}

func townNames() string {
	names := make([]string, len(towns))
	for i, t := range towns {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}

// playerPos returns a character's map position and whether they've been placed.
func playerPos(sheet map[string]int64) (int64, int64, bool) {
	x, y := sheet["mx"], sheet["my"]
	return x, y, x != 0 && y != 0
}

// setTravel points a character at a town; they walk there over the next ticks.
func (m *Manager) setTravel(msg engine.Message, fields []string) {
	if len(fields) < 3 {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg travel <town>. towns: "+townNames())
		return
	}
	t, idx, ok := townByName(strings.Join(fields[2:], " "))
	if !ok {
		m.out.Say(msg.Network, msg.Channel, "no such town. towns: "+townNames())
		return
	}
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if s, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(s) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	_ = m.store.HSet(ctx, sheetKey(pkey), "dest", int64(idx+1))
	m.out.Say(msg.Network, msg.Channel, msg.Nick+" sets out for "+t.Name+".")
}

// townStatus reports where a character is on the map.
func (m *Manager) townStatus(msg engine.Message) string {
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	if _, ok := sheet["level"]; !ok {
		return "you're not playing. !rpg to start the grind."
	}
	x, y, placed := playerPos(sheet)
	if !placed {
		return "you haven't set foot on the map yet — idle a moment."
	}
	if dest := sheet["dest"]; dest > 0 && int(dest) <= len(towns) {
		t := towns[dest-1]
		return fmt.Sprintf("%s is travelling to %s — about %d away.", msg.Nick, t.Name, dist(x, y, int64(t.X), int64(t.Y)))
	}
	if t := atTown(x, y); t != nil {
		return fmt.Sprintf("%s is at %s — the %s. %s", msg.Nick, t.Name, t.Service, serviceHint(t.Service))
	}
	nt, d := nearestTown(x, y)
	return fmt.Sprintf("%s is roaming the wilds. nearest town: %s (~%d away). !rpg travel %s", msg.Nick, nt.Name, d, nt.Name)
}

func serviceHint(service string) string {
	switch service {
	case "inn":
		return "(!rpg rest)"
	case "market":
		return "(!rpg shop)"
	case "temple":
		return "(!rpg revive)"
	}
	return ""
}

// rest heals to full at an inn.
func (m *Manager) rest(msg engine.Message) {
	m.townService(msg, "inn", func(ctx context.Context, pkey string, sheet map[string]int64, t *town) string {
		if sheet["dmg"] == 0 && !poisoned(sheet) {
			return msg.Nick + " is already hale and unpoisoned."
		}
		_ = m.store.HSet(ctx, sheetKey(pkey), "dmg", 0)
		m.curePoison(ctx, pkey)
		return fmt.Sprintf("%s rests at %s and recovers to full HP.", msg.Nick, t.Name)
	})
}

// shop quotes the market price.
func (m *Manager) shop(msg engine.Message) {
	m.townService(msg, "market", func(_ context.Context, _ string, sheet map[string]int64, t *town) string {
		lvl := sheet["level"]
		return fmt.Sprintf("%s market: a level-%d item for %dg. !rpg buy <slot> — slots: %s",
			t.Name, lvl+1, shopPrice(lvl), strings.Join(itemSlots, ", "))
	})
}

// buy purchases a level-appropriate item for a slot.
func (m *Manager) buy(msg engine.Message, fields []string) {
	if len(fields) >= 3 && strings.EqualFold(fields[2], "potion") {
		m.buyPotion(msg)
		return
	}
	if len(fields) < 3 || !isItemSlot(fields[2]) {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg buy <slot|potion>. slots: "+strings.Join(itemSlots, ", "))
		return
	}
	slot := strings.ToLower(fields[2])
	m.townService(msg, "market", func(ctx context.Context, pkey string, sheet map[string]int64, t *town) string {
		lvl := sheet["level"]
		price := shopPrice(lvl)
		if sheet["gold"] < price {
			return fmt.Sprintf("not enough gold — the %s wants %dg (you have %dg).", t.Name, price, sheet["gold"])
		}
		newLvl := lvl + 1
		if newLvl <= sheet[itemField(slot)]*rarityMult(sheet[rarityField(slot)])/100 {
			return fmt.Sprintf("your %s is already better than what's on the shelf.", slot)
		}
		_, _ = m.store.HIncr(ctx, sheetKey(pkey), "gold", -price)
		_ = m.store.HSet(ctx, sheetKey(pkey), itemField(slot), newLvl)
		_ = m.store.HSet(ctx, sheetKey(pkey), rarityField(slot), 0) // common
		_ = m.store.Del(ctx, nameKey(pkey, slot))
		return fmt.Sprintf("%s buys a level-%d %s for %dg.", msg.Nick, newLvl, slot, price)
	})
}

// revive clears the downed state at a temple, for a price.
func (m *Manager) revive(msg engine.Message) {
	m.townService(msg, "temple", func(ctx context.Context, pkey string, sheet map[string]int64, t *town) string {
		class, _ := m.store.GetStr(ctx, classKey(pkey))
		if !isDowned(sheet, class) {
			return msg.Nick + " isn't downed — no need for the temple."
		}
		price := 15 + sheet["level"]*4
		if sheet["gold"] < price {
			return fmt.Sprintf("the priests of %s want %dg to revive you (you have %dg).", t.Name, price, sheet["gold"])
		}
		_, _ = m.store.HIncr(ctx, sheetKey(pkey), "gold", -price)
		_ = m.store.HSet(ctx, sheetKey(pkey), "dmg", 0)
		m.curePoison(ctx, pkey)
		return fmt.Sprintf("the temple of %s revives %s — full HP, -%dg.", t.Name, msg.Nick, price)
	})
}

// townService is the shared gate for a service command: it loads the character,
// checks they're standing at a town offering `service`, and runs `do`.
func (m *Manager) townService(msg engine.Message, service string, do func(ctx context.Context, pkey string, sheet map[string]int64, t *town) string) {
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	if _, ok := sheet["level"]; !ok {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	x, y, placed := playerPos(sheet)
	t := (*town)(nil)
	if placed {
		t = atTown(x, y)
	}
	if t == nil || t.Service != service {
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("there's no %s here. (!rpg town to find one)", service))
		return
	}
	m.out.Say(msg.Network, msg.Channel, do(ctx, pkey, sheet, t))
}

func shopPrice(level int64) int64 { return (level + 1) * 8 }

func isItemSlot(s string) bool {
	s = strings.ToLower(s)
	for _, slot := range itemSlots {
		if slot == s {
			return true
		}
	}
	return false
}
