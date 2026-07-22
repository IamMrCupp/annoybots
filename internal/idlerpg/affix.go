package idlerpg

import "strings"

// Affixes give gear a reason to exist beyond a power number. A drop can roll one
// or more magical properties — vampiric, thorned, keen — and those properties do
// real work in every fight. Two legendaries are no longer interchangeable: one
// might heal you as you swing, the other turn your attacker's blows back on them.
//
// An item's affixes are a bitmask in the "af:<slot>" sheet field (the sheet hash
// holds only ints), rolled on the drop and scaled at combat time by that item's
// rarity — a legendary's vampiric bite is far stronger than a rare's.

type affixDef struct {
	bit  int64
	name string // shown on the item
	desc string // shown in help / on the dashboard
}

// affixList, in display order. Bits are stable — append new affixes, never reorder.
var affixList = []affixDef{
	{1 << 0, "vampiric", "heals you for part of the damage you deal"},
	{1 << 1, "thorned", "turns damage back on whatever strikes you"},
	{1 << 2, "keen", "strikes critically far more often"},
	{1 << 3, "vital", "grants bonus maximum hit points"},
	{1 << 4, "warded", "blunts every blow you take"},
	{1 << 5, "swift", "sharpens your aim"},
}

func affixField(slot string) string { return "af:" + slot }

// affixNames decodes a mask into its affix names, in display order.
func affixNames(mask int64) []string {
	var out []string
	for _, a := range affixList {
		if mask&a.bit != 0 {
			out = append(out, a.name)
		}
	}
	return out
}

// affixSuffix renders a mask for display next to an item, e.g. " [vampiric, keen]".
func affixSuffix(mask int64) string {
	names := affixNames(mask)
	if len(names) == 0 {
		return ""
	}
	return " [" + strings.Join(names, ", ") + "]"
}

// affixCount is how many affixes a rarity tier rolls: common never, legendary
// always several. Index matches the rarities table.
func (m *Manager) affixCount(rarityIdx int64) int {
	switch rarityName(rarityIdx) {
	case "uncommon":
		return m.roll(2) // 0 or 1
	case "rare":
		return 1
	case "epic":
		return 1 + m.roll(2) // 1 or 2
	case "legendary":
		return 2 + m.roll(2) // 2 or 3
	}
	return 0 // common
}

// rollAffixes picks a distinct set of affixes for a drop of the given rarity.
func (m *Manager) rollAffixes(rarityIdx int64) int64 {
	want := m.affixCount(rarityIdx)
	var mask int64
	for i := 0; i < want*4 && countBits(mask) < want; i++ { // a few tries to fill
		mask |= affixList[m.roll(len(affixList))].bit
	}
	return mask
}

// affixCap is the most affixes one item can carry, so repeated enchanting can't
// stack every property onto a single slot.
const affixCap = 4

// awakenAffix adds one affix the item doesn't already have, up to affixCap. It
// returns the mask unchanged once the item is full.
func (m *Manager) awakenAffix(mask int64) int64 {
	if countBits(mask) >= affixCap {
		return mask
	}
	var missing []int64
	for _, a := range affixList {
		if mask&a.bit == 0 {
			missing = append(missing, a.bit)
		}
	}
	if len(missing) == 0 {
		return mask
	}
	return mask | missing[m.roll(len(missing))]
}

func countBits(mask int64) int {
	n := 0
	for ; mask != 0; mask &= mask - 1 {
		n++
	}
	return n
}

// affixMods is the aggregate effect of every affix across all equipped gear.
type affixMods struct {
	lifesteal int64 // HP restored on a landed hit
	thorns    int64 // damage reflected when something hits you
	keen      int64 // widened critical range (crit on d20 >= 20-keen)
	vital     int64 // bonus maximum HP
	warded    int64 // flat reduction to incoming damage
	swift     int64 // bonus to hit
}

// affixesOf sums a character's equipped affixes. Each instance is weighted by its
// item's rarity tier (common 1 → legendary 5), so where an affix sits matters as
// much as having it.
func affixesOf(sheet map[string]int64) affixMods {
	var am affixMods
	for _, slot := range itemSlots {
		mask := sheet[affixField(slot)]
		if mask == 0 || sheet[itemField(slot)] <= 0 {
			continue
		}
		tier := sheet[rarityField(slot)] + 1 // 1..5
		for _, a := range affixList {
			if mask&a.bit == 0 {
				continue
			}
			switch a.name {
			case "vampiric":
				am.lifesteal += tier
			case "thorned":
				am.thorns += tier
			case "keen":
				am.keen += tier // each point widens the crit range by one
			case "vital":
				am.vital += tier * 3
			case "warded":
				am.warded += tier
			case "swift":
				am.swift += tier
			}
		}
	}
	// Keen is the one that would break the d20 if left unbounded — a crit range
	// wider than a few faces turns every swing into a critical.
	if am.keen > 4 {
		am.keen = 4
	}
	return am
}

// Affixes lists every affix and what it does, for help and the dashboard.
func Affixes() []HelpItem {
	out := make([]HelpItem, 0, len(affixList))
	for _, a := range affixList {
		out = append(out, HelpItem{Cmd: a.name, Desc: a.desc})
	}
	return out
}
