package idlerpg

import (
	"context"
	"fmt"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Dungeons are the payoff for wandering the map. While roaming you may stumble on
// one; entering starts a personal, multi-tick **delve**. Each tick you push into
// the next room — a lurking foe, a sprung trap, or a chest — and the final chamber
// holds the dungeon's lord, a fat purse, and guaranteed treasure. Get downed
// inside and you're dragged out, the delve lost.
//
// State is two fields: the "dgn" room counter on the sheet, and a string key with
// the dungeon's name — so a delve survives a restart.

const (
	dungeonOdds     = 60 // ~1-in-N chance per tick a roaming idler finds one
	dungeonMinRooms = 4
	dungeonMaxRooms = 7
)

var dungeonNames = []string{
	"the Sunken Crypt",
	"the Warrens of Ash",
	"the Hollow Spire",
	"the Drowned Vault",
	"the Gloomforge",
	"the Barrow of Kings",
	"the Serpent's Coil",
}

func dungeonKey(player string) string { return "rpg:dgn:" + player }

// dungeonOf returns the name of the dungeon a character is delving, or "".
func (m *Manager) dungeonOf(ctx context.Context, key string) string {
	name, _ := m.store.GetStr(ctx, dungeonKey(key))
	return name
}

// inDungeon reports whether a character is currently delving.
func (m *Manager) inDungeon(ctx context.Context, key string) bool {
	s, _ := m.store.HGetAll(ctx, sheetKey(key))
	return s["dgn"] > 0
}

// exitDungeon clears the delve state.
func (m *Manager) exitDungeon(ctx context.Context, key string) {
	_ = m.store.HSet(ctx, sheetKey(key), "dgn", 0)
	_ = m.store.Del(ctx, dungeonKey(key))
}

// maybeDiscoverDungeon rarely uncovers a dungeon for a roaming idler.
func (m *Manager) maybeDiscoverDungeon(ctx context.Context, p player) {
	if m.roll(dungeonOdds) != 0 {
		return
	}
	sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	if sheet["dgn"] > 0 || sheet["dest"] > 0 {
		return // already delving, or marching to a town
	}
	class, _ := m.store.GetStr(ctx, classKey(p.key))
	if isDowned(sheet, class) {
		return
	}
	rooms := int64(dungeonMinRooms + m.roll(dungeonMaxRooms-dungeonMinRooms+1))
	name := dungeonNames[m.roll(len(dungeonNames))]
	_ = m.store.HSet(ctx, sheetKey(p.key), "dgn", rooms)
	_ = m.store.SetStr(ctx, dungeonKey(p.key), name)
	m.drama(p.network, p.channel, fmt.Sprintf(
		"🏚 %s stumbles on %s — %d rooms of darkness yawn ahead. they descend…", p.nick, name, rooms))
}

// dungeonTick delves a single room. The last room is the dungeon lord.
func (m *Manager) dungeonTick(ctx context.Context, p player) {
	sheet, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	rooms := sheet["dgn"]
	if rooms <= 0 {
		return
	}
	name := m.dungeonOf(ctx, p.key)
	class, _ := m.store.GetStr(ctx, classKey(p.key))
	lvl := sheet["level"]

	if rooms == 1 { // the final chamber
		lord := monster{
			Name:   "the lord of " + name,
			AC:     16 + lvl/6,
			Atk:    6 + lvl/6,
			DmgDie: 10,
			HP:     60 + lvl*4,
			Gold:   120 + lvl*10,
		}
		m.drama(p.network, p.channel, fmt.Sprintf("🕯 %s reaches the final chamber of %s — %s awaits!", p.nick, name, lord.Name))
		m.resolveFight(ctx, p, sheet, class, lord)
		after, _ := m.store.HGetAll(ctx, sheetKey(p.key))
		if !isDowned(after, class) {
			plunder := m.harvestGold(200 + lvl*12)
			_, _ = m.store.HIncr(ctx, sheetKey(p.key), "gold", plunder)
			m.bumpStat("gold", plunder)
			m.drama(p.network, p.channel, fmt.Sprintf("🏆 %s plunders %s — +%dg and treasure hauled from the dark!", p.nick, name, plunder))
			m.findItem(ctx, p, lvl+12) // guaranteed good spoils
		}
		m.exitDungeon(ctx, p.key)
		return
	}

	switch m.roll(3) {
	case 0: // something lurks
		mon := m.pickMonster(lvl, biomeOf(sheet["mx"], sheet["my"]))
		mon.HP += lvl // tougher in the dark
		m.drama(p.network, p.channel, fmt.Sprintf("🕯 deeper into %s, %s meets %s.", name, p.nick, mon.Name))
		m.resolveFight(ctx, p, sheet, class, mon)
	case 1: // a chest
		g := m.harvestGold(int64(20 + m.roll(int(lvl)*4+30)))
		_, _ = m.store.HIncr(ctx, sheetKey(p.key), "gold", g)
		m.bumpStat("gold", g)
		m.drama(p.network, p.channel, fmt.Sprintf("💰 %s prises open a chest in %s — +%dg.", p.nick, name, g))
	default: // a trap
		dmg := 2 + lvl/8
		m.damage(ctx, p.key, dmg)
		m.drama(p.network, p.channel, fmt.Sprintf("🪤 a trap bites %s in %s — %d damage.", p.nick, name, dmg))
	}

	after, _ := m.store.HGetAll(ctx, sheetKey(p.key))
	if isDowned(after, class) {
		m.drama(p.network, p.channel, fmt.Sprintf("💀 %s is dragged senseless from %s — the delve is lost.", p.nick, name))
		m.exitDungeon(ctx, p.key)
		return
	}
	_, _ = m.store.HIncr(ctx, sheetKey(p.key), "dgn", -1)
}

// dungeonStatus answers !rpg dungeon.
func (m *Manager) dungeonStatus(msg engine.Message) string {
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	if len(sheet) == 0 {
		return "you're not playing. !rpg to start the grind."
	}
	if sheet["dgn"] <= 0 {
		return msg.Nick + " is not delving. wander the wilds and you may find a way down."
	}
	return fmt.Sprintf("🏚 %s is delving %s — %d room(s) to go.", msg.Nick, m.dungeonOf(ctx, pkey), sheet["dgn"])
}
