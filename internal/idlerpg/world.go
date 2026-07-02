package idlerpg

import (
	"context"
)

// The world map: every player has a persistent position and wanders a step each
// tick while they're online (idlerpg.net's marquee "watch everyone roam" map).
// Positions are 1-based (mx/my fields on the sheet); 0 means "not placed yet", so
// a brand-new or pre-existing character gets dropped somewhere random on its first
// move. This is cosmetic — movement doesn't affect leveling.

const (
	worldSize = 500 // the realm is worldSize×worldSize
	worldStep = 14  // max distance a player drifts per tick
)

// town is a named landmark on the world map. Service is what you can do there:
// "inn" (rest to heal), "market" (buy gear), or "temple" (revive when downed).
// Terrain is the biome around it, which themes the monsters you meet nearby.
type town struct {
	Name    string
	X, Y    int
	Service string
	Terrain string
}

// townRadius is how close (world units) you must be to a town to use its service.
const townRadius = 25

// towns are fixed points of interest on the map.
var towns = []town{
	{"Idlecrest", 250, 250, "temple", "plains"},
	{"Lurk Harbor", 70, 410, "market", "coast"},
	{"Mount AFK", 420, 90, "inn", "mountain"},
	{"Quietford", 130, 150, "market", "forest"},
	{"The Lag Marsh", 360, 380, "temple", "swamp"},
	{"Tab-Away Tavern", 200, 60, "inn", "plains"},
}

// biomeOf returns the terrain a position sits in — the nearest town's biome.
func biomeOf(x, y int64) string {
	t, _ := nearestTown(x, y)
	return t.Terrain
}

// moveOnMap advances an online player one step: toward their travel destination if
// they've set one (announcing arrival), otherwise a random drift. Places them on
// the map first if they've never been on it.
func (m *Manager) moveOnMap(ctx context.Context, p player) {
	sheet, err := m.store.HGetAll(ctx, sheetKey(p.key))
	if err != nil {
		return
	}
	x, y := sheet["mx"], sheet["my"]
	if x == 0 || y == 0 {
		_ = m.store.HSet(ctx, sheetKey(p.key), "mx", int64(m.roll(worldSize)+1))
		_ = m.store.HSet(ctx, sheetKey(p.key), "my", int64(m.roll(worldSize)+1))
		return
	}
	// Travelling to a town: walk toward it, announce on arrival. A mount speeds it.
	if dest := sheet["dest"]; dest > 0 && int(dest) <= len(towns) {
		t := towns[dest-1]
		step := worldStep
		if m.mountOf(ctx, p.key) != "" {
			step += mountBonus
		}
		nx, ny, reached := stepToward(int(x), int(y), t.X, t.Y, step)
		_ = m.store.HSet(ctx, sheetKey(p.key), "mx", int64(nx))
		_ = m.store.HSet(ctx, sheetKey(p.key), "my", int64(ny))
		if reached {
			_ = m.store.HSet(ctx, sheetKey(p.key), "dest", 0)
			m.out.Say(p.network, p.channel, "🏘 "+p.nick+" arrives at "+t.Name+" — the "+t.Service+".")
		}
		return
	}
	// Otherwise wander.
	x = clampWorld(x + int64(m.roll(2*worldStep+1)-worldStep))
	y = clampWorld(y + int64(m.roll(2*worldStep+1)-worldStep))
	_ = m.store.HSet(ctx, sheetKey(p.key), "mx", x)
	_ = m.store.HSet(ctx, sheetKey(p.key), "my", y)
}

func clampWorld(v int64) int64 {
	if v < 1 {
		return 1
	}
	if v > worldSize {
		return worldSize
	}
	return v
}
