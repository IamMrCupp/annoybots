package idlerpg

import (
	"fmt"
	"sort"
	"strings"
)

// The catalog surfaces the fixed sets a player chooses from — classes, races,
// alignments — plus the companion roster (earned, not chosen). It's derived from
// the live game data (the classes/races/petKinds tables), so the in-channel
// `!rpg help`, the dashboard reference, and the docs can't drift from reality.

// ClassInfo describes a pickable class for the reference.
type ClassInfo struct {
	Name    string // canonical name
	Ability string // primary ability label (e.g. "STR")
	Power   string // signature combat ability + its effect
	Blurb   string // one-line flavor
}

// RaceInfo describes a pickable race for the reference.
type RaceInfo struct {
	Name  string // canonical name
	Bonus string // ability bonuses, e.g. "+2 DEX, +1 INT"
	Blurb string
}

// PetInfo describes a possible companion for the reference.
type PetInfo struct {
	Name  string
	Atk   int64 // attack bonus it grants in monster fights
	Dmg   int64 // damage bonus
	Blurb string
}

// Classes lists the choosable classes, sorted by name.
func Classes() []ClassInfo {
	out := make([]ClassInfo, 0, len(classes))
	for _, c := range classes {
		out = append(out, ClassInfo{
			Name:    c.Name,
			Ability: abilityLabel(c.Primary),
			Power:   c.Ability + " — " + c.AbilDsc,
			Blurb:   c.Blurb,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Races lists the choosable races, sorted by name.
func Races() []RaceInfo {
	out := make([]RaceInfo, 0, len(races))
	for _, r := range races {
		out = append(out, RaceInfo{Name: r.Name, Bonus: raceBonus(r.Mods), Blurb: r.Blurb})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Pets lists the possible companions (earned by slaying a boss), in roster order.
func Pets() []PetInfo {
	out := make([]PetInfo, 0, len(petKinds))
	for _, p := range petKinds {
		out = append(out, PetInfo{Name: p.Name, Atk: p.Atk, Dmg: p.Dmg, Blurb: p.Blurb})
	}
	return out
}

// Alignments lists the nine points of the D&D grid in display order.
func Alignments() []string {
	return []string{
		"lawful good", "neutral good", "chaotic good",
		"lawful neutral", "true neutral", "chaotic neutral",
		"lawful evil", "neutral evil", "chaotic evil",
	}
}

// abilityLabel maps an ability field ("dex") to its display label ("DEX").
func abilityLabel(field string) string {
	for _, a := range abilityLabels {
		if a.field == field {
			return a.label
		}
	}
	return strings.ToUpper(field)
}

// raceBonus renders a race's ability modifiers in canonical ability order.
func raceBonus(mods map[string]int64) string {
	var parts []string
	for _, a := range abilityLabels {
		if v := mods[a.field]; v != 0 {
			parts = append(parts, fmt.Sprintf("+%d %s", v, a.label))
		}
	}
	return strings.Join(parts, ", ")
}
