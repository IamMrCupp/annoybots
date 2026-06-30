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
	{"epic", 25, 220, true},
	{"legendary", 5, 300, true},
}

// Magic-item names are generated per slot so they're slot-appropriate (a weapon
// is a "Blade", boots are "Striders") and rarely repeat. epic items get a
// two-word name; legendaries get a full "… of <epithet>" title.
var lootAdjs = []string{
	"Flaming", "Frostbound", "Vicious", "Ancient", "Cursed", "Gilded", "Shadow",
	"Storm", "Bloodforged", "Whispering", "Thorned", "Gloom", "Radiant",
	"Wyrmscale", "Doomforged", "Spectral", "Venomous", "Hallowed", "Obsidian",
	"Searing", "Tempest", "Eclipse", "Ironbound", "Soulbound", "Glimmering",
	"Howling", "Sundered", "Verdant", "Abyssal", "Runescribed",
}

var lootEpithets = []string{
	"the Lurker", "the Abyss", "Doom", "Kings", "the Wyrm", "Night", "the Fallen",
	"Frost", "Storms", "the Void", "Ash", "the Deep", "Ruin", "Embers",
	"the Dawn", "the Eclipse", "the Titan", "Sorrows", "the Hollow", "the Pyre",
	"the Serpent", "the Tempest", "Echoes", "the Maelstrom",
}

// slotNouns gives each equipment slot its own noun pool, so two different slots
// can never generate the same name.
var slotNouns = map[string][]string{
	"ring":     {"Band", "Signet", "Loop", "Circle", "Coil", "Seal"},
	"amulet":   {"Amulet", "Talisman", "Pendant", "Locket", "Phylactery", "Torc"},
	"charm":    {"Charm", "Idol", "Token", "Sigil", "Fetish", "Ward"},
	"weapon":   {"Blade", "Edge", "Fang", "Reaver", "Cleaver", "Maul", "Glaive"},
	"helm":     {"Helm", "Crown", "Visage", "Casque", "Coif", "Diadem"},
	"tunic":    {"Mail", "Vestment", "Hauberk", "Wrap", "Cuirass", "Shroud"},
	"gloves":   {"Gauntlets", "Grips", "Talons", "Mitts", "Clutches", "Vices"},
	"leggings": {"Greaves", "Legguards", "Chausses", "Cuisses", "Faulds"},
	"shield":   {"Aegis", "Bulwark", "Wall", "Bastion", "Rampart", "Ward"},
	"boots":    {"Treads", "Striders", "Sabatons", "Stalkers", "Greaves", "Steps"},
}

// magicName generates a name for an item in slot. Legendaries get an epithet title.
func (m *Manager) magicName(slot string, legendary bool) string {
	nouns := slotNouns[slot]
	if len(nouns) == 0 {
		nouns = []string{"Relic"}
	}
	name := lootAdjs[m.roll(len(lootAdjs))] + " " + nouns[m.roll(len(nouns))]
	if legendary {
		name += " of " + lootEpithets[m.roll(len(lootEpithets))]
	}
	return name
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
