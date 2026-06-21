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
	Ability string // signature combat ability name
	AbilDsc string // one-line ability description
	Blurb   string // one-line flavor for announcements
}

var classes = map[string]classDef{
	"fighter": {"fighter", "str", 10, "Extra Attack", "a second swing each combat round", "a frontline bruiser — STR drives the blade"},
	"ranger":  {"ranger", "dex", 10, "Hunter's Mark", "extra damage on every hit", "a deadeye skirmisher — DEX guides the shot"},
	"rogue":   {"rogue", "dex", 8, "Sneak Attack", "big bonus damage when you land a hit", "a sneak who strikes where it hurts (DEX)"},
	"cleric":  {"cleric", "wis", 8, "Healing Word", "heal yourself a little each round", "a battle-priest channeling WIS"},
	"bard":    {"bard", "cha", 8, "Cutting Words", "a chance to spoil the enemy's attack", "a silver-tongued meddler running on CHA"},
	"wizard":  {"wizard", "int", 6, "Arcane Bolt", "guaranteed magic damage each round", "a glass cannon slinging INT"},
}

// combatMods are the per-class effects applied during a monster encounter. All
// magnitudes are deterministic functions of the character's ability scores, so
// they're easy to test.
type combatMods struct {
	extraAttacks int    // additional weapon swings per round (fighter)
	bonusOnHit   int64  // extra damage when a weapon hit lands (rogue/ranger)
	autoDmg      int64  // damage dealt every round, no roll (wizard)
	selfHeal     int64  // HP recovered each round (cleric)
	negateChance int    // 1-in-N chance to negate the monster's attack (bard); 0 = none
	ability      string // display name, empty if no class
}

// classCombat returns the combat effects for a character's class.
func classCombat(class string, sheet map[string]int64) combatMods {
	c, ok := classOf(class)
	if !ok {
		return combatMods{}
	}
	nonNeg := func(v int64) int64 {
		if v < 0 {
			return 0
		}
		return v
	}
	cm := combatMods{ability: c.Ability}
	switch c.Name {
	case "fighter":
		cm.extraAttacks = 1
	case "wizard":
		cm.autoDmg = nonNeg(2 + abilityMod(sheet["int"]))
	case "rogue":
		cm.bonusOnHit = nonNeg(3 + abilityMod(sheet["dex"]))
	case "ranger":
		cm.bonusOnHit = nonNeg(2 + abilityMod(sheet["dex"]))
	case "cleric":
		cm.selfHeal = nonNeg(1 + abilityMod(sheet["wis"]))
	case "bard":
		cm.negateChance = 3
	}
	return cm
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
