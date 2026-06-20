package idlerpg

// Loot rarity layers tiers on top of the plain item levels: a dropped item rolls
// a rarity (common → legendary) that multiplies its power, and the rarest finds
// come named ("Flametongue"). idlerpg.org's loot is flat levels; this modernizes
// the feel. Rarity is a per-slot sheet field; names are separate string keys
// (the sheet hash holds only ints).

type rarity struct {
	name   string
	weight int   // out of 1000
	mult   int64 // power multiplier, percent
	named  bool  // does this tier grant a name?
}

// rarities, common to legendary. Indexes are stored on the sheet, so order is
// stable — append new tiers, don't reorder.
var rarities = []rarity{
	{"common", 690, 100, false},
	{"uncommon", 200, 130, false},
	{"rare", 80, 170, false},
	{"epic", 25, 220, false},
	{"legendary", 5, 300, true},
}

var legendaryNames = []string{
	"Flametongue", "Widowmaker", "the Whisper", "Doombringer",
	"Starcaller", "Frostbite", "Kingslayer", "the Lurker's Gift",
}

func rarityField(slot string) string     { return "ir:" + slot }
func nameKey(player, slot string) string { return "rpg:in:" + player + ":" + slot }

// rarityMult returns a rarity index's power multiplier (percent); unknown → 100.
func rarityMult(idx int64) int64 {
	if idx >= 0 && int(idx) < len(rarities) {
		return rarities[idx].mult
	}
	return 100
}

// rarityName returns a rarity index's label; unknown → "common".
func rarityName(idx int64) string {
	if idx >= 0 && int(idx) < len(rarities) {
		return rarities[idx].name
	}
	return rarities[0].name
}

// pickRarity rolls a rarity, biased upward by level so high-level finds skew rarer.
func (m *Manager) pickRarity(level int64) int {
	roll := m.roll(1000) + int(level)*4
	if roll > 999 {
		roll = 999
	}
	acc := 0
	for i, r := range rarities {
		if acc += r.weight; roll < acc {
			return i
		}
	}
	return len(rarities) - 1
}
