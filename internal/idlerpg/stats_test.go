package idlerpg

import (
	"context"
	"testing"
)

func TestReadStats(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 10)
	st.ZIncr(ctx, boardKey(), "net|alice", 10)
	st.HSet(ctx, sheetKey("net|bob"), "level", 4)
	st.ZIncr(ctx, boardKey(), "net|bob", 4)

	m.bumpStat("kills", 7)
	m.bumpStat("bosses", 1)
	m.bumpStat("gold", 250)
	m.bumpStat("legendaries", 2)

	s, err := ReadStats(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if s.Heroes != 2 {
		t.Fatalf("heroes = %d; want 2", s.Heroes)
	}
	if s.Levels != 14 {
		t.Fatalf("levels = %d; want 14 (10+4)", s.Levels)
	}
	if s.Kills != 7 || s.Bosses != 1 || s.Gold != 250 || s.Legendaries != 2 {
		t.Fatalf("counters wrong: %+v", s)
	}
}
