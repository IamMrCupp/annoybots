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

// town is a static, named landmark drawn on the world map for flavor.
type town struct {
	Name string
	X, Y int
}

// towns are fixed points of interest on the map.
var towns = []town{
	{"Idlecrest", 250, 250},
	{"Lurk Harbor", 70, 410},
	{"Mount AFK", 420, 90},
	{"Quietford", 130, 150},
	{"The Lag Marsh", 360, 380},
	{"Tab-Away Tavern", 200, 60},
}

// moveOnMap drifts an online player one random step, placing them first if they've
// never been on the map. Caller passes the player's character key.
func (m *Manager) moveOnMap(ctx context.Context, key string) {
	sheet, err := m.store.HGetAll(ctx, sheetKey(key))
	if err != nil {
		return
	}
	x, y := sheet["mx"], sheet["my"]
	if x == 0 || y == 0 {
		x = int64(m.roll(worldSize) + 1)
		y = int64(m.roll(worldSize) + 1)
	} else {
		x = clampWorld(x + int64(m.roll(2*worldStep+1)-worldStep))
		y = clampWorld(y + int64(m.roll(2*worldStep+1)-worldStep))
	}
	_ = m.store.HSet(ctx, sheetKey(key), "mx", x)
	_ = m.store.HSet(ctx, sheetKey(key), "my", y)
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
