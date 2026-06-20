package idlerpg

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// The six D&D ability scores. We roll 4d6-drop-lowest at character creation
// (3–18) and derive the classic modifier (floor((score-10)/2)). idlerpg.org is
// deliberately stat-less; this is the foundation that makes classes, combat, and
// saves mean something. Stored as lowercase fields on the player sheet.
var abilities = []string{"str", "dex", "con", "int", "wis", "cha"}

// abilityLabels maps the field to its display label, in canonical order.
var abilityLabels = []struct{ field, label string }{
	{"str", "STR"}, {"dex", "DEX"}, {"con", "CON"},
	{"int", "INT"}, {"wis", "WIS"}, {"cha", "CHA"},
}

// roll4d6DropLowest rolls four d6, drops the lowest, and sums the rest (3–18).
func (m *Manager) roll4d6DropLowest() int64 {
	d := []int{m.roll(6) + 1, m.roll(6) + 1, m.roll(6) + 1, m.roll(6) + 1}
	sort.Ints(d)
	return int64(d[1] + d[2] + d[3])
}

// abilityMod is the D&D modifier for a score: floor((score-10)/2).
func abilityMod(score int64) int64 {
	d := score - 10
	if d >= 0 {
		return d / 2
	}
	return -((-d + 1) / 2) // floor division for negatives
}

// ensureAbilities rolls and stores the six ability scores if the character hasn't
// got them yet (new enrollees, or existing characters from before this feature).
// "str" is the sentinel — a real score is never 0.
func (m *Manager) ensureAbilities(ctx context.Context, key string) {
	sheet, err := m.store.HGetAll(ctx, sheetKey(key))
	if err != nil || sheet["str"] != 0 {
		return
	}
	for _, a := range abilities {
		_ = m.store.HSet(ctx, sheetKey(key), a, m.roll4d6DropLowest())
	}
}

// abilityLine renders the ability block for a sheet, e.g.
// "STR 14 (+2) · DEX 12 (+1) · …".
func abilityLine(sheet map[string]int64) string {
	parts := make([]string, 0, len(abilityLabels))
	for _, a := range abilityLabels {
		s := sheet[a.field]
		parts = append(parts, fmt.Sprintf("%s %d (%+d)", a.label, s, abilityMod(s)))
	}
	return strings.Join(parts, " · ")
}
