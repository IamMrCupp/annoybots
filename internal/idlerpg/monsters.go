package idlerpg

import (
	"context"
	"fmt"
)

// Monster encounters are the PvE heart of the D&D layer — and, unlike the level-up
// PvP battle, they work with a single player. On some ticks a wandering idler runs
// into a level-scaled monster and a quick d20 fight resolves: attacker rolls
// d20 + attack vs the defender's AC, hits deal damage, repeat until one drops.
// Win → time toward the next level, gold, a kill, maybe loot. Lose → bloodied or
// downed.

const (
	monsterOdds = 5  // ~1-in-N chance a monster appears for a random idler each tick
	bossOdds    = 14 // once a monster appears, ~1-in-N chance it's a boss (if one is eligible)
	bossKills   = 3  // a boss counts as several kills toward renown/titles
)

// monster is one bestiary entry.
type monster struct {
	Name   string
	MinLvl int64 // first level at which it can appear
	AC     int64 // armor class (target number to hit it)
	Atk    int64 // its attack bonus
	DmgDie int64 // its damage die (dN)
	HP     int64
	Gold   int64 // reward on a kill
	Boss   bool  // a named, high-stakes foe with outsized rewards
}

// bestiary, weakest to nastiest.
var bestiary = []monster{
	{"a giant rat", 0, 10, 0, 4, 4, 1, false},
	{"a goblin", 1, 12, 2, 6, 7, 3, false},
	{"a kobold warren-scout", 2, 12, 3, 4, 6, 4, false},
	{"an orc", 4, 13, 4, 8, 16, 8, false},
	{"a gnoll pack-hunter", 6, 14, 4, 8, 24, 12, false},
	{"an ogre", 9, 11, 5, 10, 38, 22, false},
	{"a wyvern", 13, 15, 6, 8, 55, 45, false},
	{"a young dragon", 18, 17, 7, 12, 85, 110, false},
}

// bosses are rare, named legends — far above a normal foe in AC, HP, and damage,
// but they pay out a fortune, several kills, and guaranteed top-tier loot. Each
// gates on a minimum level so low-level idlers aren't fed to a god.
var bosses = []monster{
	{"the Tarrasque", 12, 18, 8, 12, 160, 200, true},
	{"the Kraken of the Sunless Deep", 16, 18, 9, 12, 210, 280, true},
	{"the Lich-King Vol'kresh", 20, 19, 9, 10, 240, 360, true},
	{"Tiamat, Queen of Dragons", 25, 20, 10, 12, 300, 480, true},
	{"Asmodeus, Lord of the Nine Hells", 30, 20, 11, 12, 360, 640, true},
}

// pickBoss returns an eligible boss and true, on the bossOdds chance, when the
// player is high enough level to face one.
func (m *Manager) pickBoss(level int64) (monster, bool) {
	if m.roll(bossOdds) != 0 {
		return monster{}, false
	}
	var eligible []monster
	for _, b := range bosses {
		if b.MinLvl <= level {
			eligible = append(eligible, b)
		}
	}
	if len(eligible) == 0 {
		return monster{}, false
	}
	return eligible[m.roll(len(eligible))], true
}

// pickMonster chooses a level-appropriate foe.
func (m *Manager) pickMonster(level int64) monster {
	var eligible []monster
	for _, mon := range bestiary {
		if mon.MinLvl <= level {
			eligible = append(eligible, mon)
		}
	}
	if len(eligible) == 0 {
		return bestiary[0]
	}
	return eligible[m.roll(len(eligible))]
}

// maybeMonster occasionally throws a monster at a random online idler.
func (m *Manager) maybeMonster(ctx context.Context) {
	if m.roll(monsterOdds) != 0 {
		return
	}
	if p, ok := m.randomOnline(); ok {
		m.fightMonster(ctx, p)
	}
}

// fightMonster picks a scaled foe and resolves an encounter for p.
func (m *Manager) fightMonster(ctx context.Context, p player) {
	sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	class, _ := m.store.GetStr(ctx, classKey(p.key))
	if isDowned(sheet, class) {
		return // already down — can't fight
	}
	mon := m.pickMonster(sheet["level"])
	if boss, ok := m.pickBoss(sheet["level"]); ok {
		mon = boss
		m.out.Say(p.network, p.channel, fmt.Sprintf(
			"🌩️ the sky darkens and the ground splits — %s rises to challenge %s!", mon.Name, p.nick))
	}
	m.resolveFight(ctx, p, sheet, class, mon)
}

// resolveFight runs the round-by-round combat and applies the outcome.
func (m *Manager) resolveFight(ctx context.Context, p player, sheet map[string]int64, class string, mon monster) {
	startHP := curHP(sheet, class)
	pHP := startHP
	pAC := 10 + abilityMod(sheet["dex"])
	pAtk := 2 + sheet["level"]/4 + classAttackMod(sheet, class)
	switch sheet["law"] { // ethical axis: lawful is disciplined, chaotic is reckless
	case 1:
		pAC++ // lawful: +AC
	case 2:
		pAtk++ // chaotic: +attack
	}
	pDmgBonus := classAttackMod(sheet, class)
	if pDmgBonus < 0 {
		pDmgBonus = 0
	}
	cm := classCombat(class, sheet)
	usedAbility := false
	monHP := mon.HP

	swing := func() bool { // one weapon attack; returns whether it landed
		if int64(m.roll(20)+1)+pAtk >= mon.AC {
			monHP -= int64(m.roll(8)+1) + pDmgBonus
			return true
		}
		return false
	}

	for round := 0; round < 30 && pHP > 0 && monHP > 0; round++ {
		hit := swing()
		for k := 0; k < cm.extraAttacks; k++ { // fighter: Extra Attack
			if swing() {
				hit, usedAbility = true, true
			}
		}
		if hit && cm.bonusOnHit > 0 { // rogue/ranger: Sneak Attack / Hunter's Mark
			monHP -= cm.bonusOnHit
			usedAbility = true
		}
		if cm.autoDmg > 0 { // wizard: Arcane Bolt
			monHP -= cm.autoDmg
			usedAbility = true
		}
		if monHP <= 0 {
			break
		}
		// bard: Cutting Words may spoil the monster's swing.
		if cm.negateChance > 0 && m.roll(cm.negateChance) == 0 {
			usedAbility = true
		} else if int64(m.roll(20)+1)+mon.Atk >= pAC {
			pHP -= int64(m.roll(int(mon.DmgDie)) + 1)
		}
		if cm.selfHeal > 0 && pHP < startHP { // cleric: Healing Word
			pHP += cm.selfHeal
			if pHP > startHP {
				pHP = startHP
			}
			usedAbility = true
		}
	}

	if taken := startHP - pHP; taken > 0 {
		m.damage(ctx, p.key, taken)
	}

	if monHP <= 0 {
		flourish := ""
		if usedAbility && cm.ability != "" {
			flourish = " with " + cm.ability
		}
		if mon.Boss {
			reward := m.pctOfTTL(ctx, p.key, 22, 35)
			_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", -reward)
			_, _ = m.store.HIncr(ctx, sheetKey(p.key), "gold", mon.Gold)
			_, _ = m.store.HIncr(ctx, sheetKey(p.key), "kills", bossKills)
			m.out.Say(p.network, p.channel, fmt.Sprintf(
				"🏆 %s has slain %s%s! a legend is born — +%dg, %d kills, and %ds toward glory.",
				p.nick, mon.Name, flourish, mon.Gold, bossKills, reward))
			// guaranteed top-tier spoils: two drops rolled as if far higher level.
			m.findItem(ctx, p, sheet["level"]+30)
			m.findItem(ctx, p, sheet["level"]+30)
			return
		}
		reward := m.pctOfTTL(ctx, p.key, 8, 14)
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", -reward)
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "gold", mon.Gold)
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "kills", 1)
		m.out.Say(p.network, p.channel, fmt.Sprintf(
			"⚔️ %s slew %s%s — +%dg, %ds closer to the next level.", p.nick, mon.Name, flourish, mon.Gold, reward))
		if m.roll(3) == 0 {
			m.findItem(ctx, p, sheet["level"])
		}
		return
	}

	if pHP <= 0 {
		m.out.Say(p.network, p.channel, fmt.Sprintf(
			"💀 %s was felled by %s and left for dead — they must recover before pressing on.", p.nick, mon.Name))
		return
	}
	m.out.Say(p.network, p.channel, fmt.Sprintf(
		"🩸 %s fled %s, bloodied but alive.", p.nick, mon.Name))
}
