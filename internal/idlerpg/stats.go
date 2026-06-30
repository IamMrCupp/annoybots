package idlerpg

import (
	"context"

	"github.com/IamMrCupp/annoybots/internal/state"
)

// Realm stats are lifetime, realm-wide tallies — monsters slain, bosses felled,
// gold minted, legendaries found — accumulated as events fire and surfaced on the
// dashboard's overview strip. They're plain counters (never decremented), so they
// grow from the moment this ships; past history isn't retroactively counted.

func statKey(field string) string { return "rpg:stat:" + field }

// bumpStat adds to a realm-wide counter. Best-effort.
func (m *Manager) bumpStat(field string, n int64) {
	_, _ = m.store.Incr(context.Background(), statKey(field), n)
}

// RealmStats is the dashboard overview: live aggregates of the whole realm.
type RealmStats struct {
	Heroes      int64 // enrolled characters on the leaderboard
	Levels      int64 // sum of every hero's level
	Kills       int64 // monsters slain (lifetime)
	Bosses      int64 // bosses felled (lifetime)
	Gold        int64 // gold minted by kills (lifetime)
	Legendaries int64 // legendary items found (lifetime)
}

// ReadStats gathers the realm overview: hero count + level sum from the
// leaderboard, plus the lifetime event counters.
func ReadStats(ctx context.Context, store state.Store) (RealmStats, error) {
	board, err := store.ZTop(ctx, boardKey(), 100000)
	if err != nil {
		return RealmStats{}, err
	}
	s := RealmStats{Heroes: int64(len(board))}
	for _, e := range board {
		s.Levels += e.Score
	}
	s.Kills, _ = store.Get(ctx, statKey("kills"))
	s.Bosses, _ = store.Get(ctx, statKey("bosses"))
	s.Gold, _ = store.Get(ctx, statKey("gold"))
	s.Legendaries, _ = store.Get(ctx, statKey("legendaries"))
	return s, nil
}
