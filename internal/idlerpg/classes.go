package idlerpg

import (
	"sort"
	"strings"
)

// Classes give a character a combat identity keyed off a primary ability score.
// Where idlerpg.org's "class" is pure flavor text, ours is mechanical: the
// primary ability's modifier is added to your attack power, and the hit die feeds
// max HP (see #67). Pick one with !rpg class <name>.
type classDef struct {
	Name    string // canonical lowercase name
	Primary string // primary ability field ("str", "dex", …)
	HitDie  int64  // HP per level (d6/d8/d10), used by the HP system
	Blurb   string // one-line flavor for announcements
}

var classes = map[string]classDef{
	"fighter": {"fighter", "str", 10, "a frontline bruiser — STR drives the blade"},
	"ranger":  {"ranger", "dex", 10, "a deadeye skirmisher — DEX guides the shot"},
	"rogue":   {"rogue", "dex", 8, "a sneak who strikes where it hurts (DEX)"},
	"cleric":  {"cleric", "wis", 8, "a battle-priest channeling WIS"},
	"bard":    {"bard", "cha", 8, "a silver-tongued meddler running on CHA"},
	"wizard":  {"wizard", "int", 6, "a glass cannon slinging INT"},
}

// classNames lists the canonical classes, sorted, for usage messages.
func classNames() []string {
	out := make([]string, 0, len(classes))
	for n := range classes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// classOf looks up a class by (case-insensitive) name.
func classOf(name string) (classDef, bool) {
	c, ok := classes[strings.ToLower(strings.TrimSpace(name))]
	return c, ok
}

// classAttackMod returns the primary-ability modifier a character's class adds to
// attacks. A legacy/free-text class (not in the canonical set) grants nothing.
func classAttackMod(sheet map[string]int64, class string) int64 {
	if c, ok := classOf(class); ok {
		return abilityMod(sheet[c.Primary])
	}
	return 0
}
