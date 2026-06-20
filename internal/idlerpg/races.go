package idlerpg

import (
	"sort"
	"strings"
)

// Races give a character heritage — small, permanent ability-score bumps baked in
// at character creation (the D&D way; idlerpg.org has no races at all). Choose once
// with !rpg race <name>; the modifiers are added to your rolled scores and can't be
// taken back.
type raceDef struct {
	Name  string           // canonical lowercase name
	Mods  map[string]int64 // ability field → bonus
	Blurb string
}

var races = map[string]raceDef{
	"human":    {"human", map[string]int64{"con": 1, "cha": 1}, "adaptable and ambitious"},
	"elf":      {"elf", map[string]int64{"dex": 2, "int": 1}, "keen-eyed and quick"},
	"dwarf":    {"dwarf", map[string]int64{"con": 2, "wis": 1}, "stout and steady"},
	"halfling": {"halfling", map[string]int64{"dex": 2, "cha": 1}, "nimble and lucky"},
	"half-orc": {"half-orc", map[string]int64{"str": 2, "con": 1}, "brawny and relentless"},
	"gnome":    {"gnome", map[string]int64{"int": 2, "dex": 1}, "clever and twitchy"},
	"tiefling": {"tiefling", map[string]int64{"cha": 2, "int": 1}, "charming and infernal"},
}

func raceKey(player string) string { return "rpg:race:" + player }

// raceNames lists the canonical races, sorted, for usage messages.
func raceNames() []string {
	out := make([]string, 0, len(races))
	for n := range races {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// raceOf looks up a race by (case-insensitive) name.
func raceOf(name string) (raceDef, bool) {
	r, ok := races[strings.ToLower(strings.TrimSpace(name))]
	return r, ok
}
