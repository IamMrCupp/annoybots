package idlerpg

import "context"

// Hit points give combat stakes. We store damage *taken* (the "dmg" field) rather
// than current HP, so an unset field (0) naturally means full health — no sentinel
// needed. Current HP = maxHP − dmg. At 0 a character is "downed": they can't
// progress until they heal back up (a little each tick). idlerpg.org has no HP at
// all; this is core D&D feel.

const baseHP = 8

// maxHP derives the hit-point ceiling from level, CON, and the class hit die.
func maxHP(sheet map[string]int64, class string) int64 {
	die := int64(8) // unclassed characters use a d8
	if c, ok := classOf(class); ok {
		die = c.HitDie
	}
	perLevel := die/2 + 1 + abilityMod(sheet["con"]) // average die + CON modifier
	if perLevel < 1 {
		perLevel = 1
	}
	return baseHP + perLevel*(sheet["level"]+1)
}

// curHP returns current hit points (clamped at 0).
func curHP(sheet map[string]int64, class string) int64 {
	hp := maxHP(sheet, class) - sheet["dmg"]
	if hp < 0 {
		return 0
	}
	return hp
}

// isDowned reports whether the character is at 0 HP.
func isDowned(sheet map[string]int64, class string) bool {
	return curHP(sheet, class) <= 0
}

// damage applies amt damage and returns the new damage total.
func (m *Manager) damage(ctx context.Context, key string, amt int64) int64 {
	d, _ := m.store.HIncr(ctx, sheetKey(key), "dmg", amt)
	return d
}

// heal removes amt damage, clamped so a character never goes below zero damage.
func (m *Manager) heal(ctx context.Context, key string, amt int64) {
	if d, _ := m.store.HIncr(ctx, sheetKey(key), "dmg", -amt); d < 0 {
		_ = m.store.HSet(ctx, sheetKey(key), "dmg", 0)
	}
}

// tickHP applies natural healing for one tick and reports whether the character is
// still downed afterward (so the caller can freeze their progress).
func (m *Manager) tickHP(ctx context.Context, key string) bool {
	sheet, err := m.store.HGetAll(ctx, sheetKey(key))
	if err != nil {
		return false
	}
	class, _ := m.store.GetStr(ctx, classKey(key))
	dmg := sheet["dmg"]
	if dmg > 0 {
		regen := sheet["level"]/2 + abilityMod(sheet["con"]) + 1
		if regen < 1 {
			regen = 1
		}
		m.heal(ctx, key, regen)
		if dmg -= regen; dmg < 0 {
			dmg = 0
		}
	}
	return maxHP(sheet, class)-dmg <= 0
}
