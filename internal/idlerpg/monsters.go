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

const monsterOdds = 5 // ~1-in-N chance a monster appears for a random idler each tick

// monster is one bestiary entry.
type monster struct {
	Name   string
	MinLvl int64 // first level at which it can appear
	AC     int64 // armor class (target number to hit it)
	Atk    int64 // its attack bonus
	DmgDie int64 // its damage die (dN)
	HP     int64
	Gold   int64 // reward on a kill
}

// bestiary, weakest to nastiest.
var bestiary = []monster{
	{"a giant rat", 0, 10, 0, 4, 4, 1},
	{"a goblin", 1, 12, 2, 6, 7, 3},
	{"a kobold warren-scout", 2, 12, 3, 4, 6, 4},
	{"an orc", 4, 13, 4, 8, 16, 8},
	{"a gnoll pack-hunter", 6, 14, 4, 8, 24, 12},
	{"an ogre", 9, 11, 5, 10, 38, 22},
	{"a wyvern", 13, 15, 6, 8, 55, 45},
	{"a young dragon", 18, 17, 7, 12, 85, 110},
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
	m.resolveFight(ctx, p, sheet, class, m.pickMonster(sheet["level"]))
}

// resolveFight runs the round-by-round combat and applies the outcome.
func (m *Manager) resolveFight(ctx context.Context, p player, sheet map[string]int64, class string, mon monster) {
	startHP := curHP(sheet, class)
	pHP := startHP
	pAC := 10 + abilityMod(sheet["dex"])
	pAtk := 2 + sheet["level"]/4 + classAttackMod(sheet, class)
	pDmgBonus := classAttackMod(sheet, class)
	if pDmgBonus < 0 {
		pDmgBonus = 0
	}
	monHP := mon.HP

	for round := 0; round < 30 && pHP > 0 && monHP > 0; round++ {
		if int64(m.roll(20)+1)+pAtk >= mon.AC { // player swings (weapon d8)
			monHP -= int64(m.roll(8)+1) + pDmgBonus
		}
		if monHP <= 0 {
			break
		}
		if int64(m.roll(20)+1)+mon.Atk >= pAC { // monster swings
			pHP -= int64(m.roll(int(mon.DmgDie)) + 1)
		}
	}

	if taken := startHP - pHP; taken > 0 {
		m.damage(ctx, p.key, taken)
	}

	if monHP <= 0 {
		reward := m.pctOfTTL(ctx, p.key, 8, 14)
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "ttl", -reward)
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "gold", mon.Gold)
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "kills", 1)
		m.out.Say(p.network, p.channel, fmt.Sprintf(
			"⚔️ %s slew %s! +%dg, %ds closer to the next level.", p.nick, mon.Name, mon.Gold, reward))
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
