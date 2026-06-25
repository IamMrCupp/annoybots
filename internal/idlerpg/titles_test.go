package idlerpg

import (
	"context"
	"testing"
)

func TestTitleFor(t *testing.T) {
	cases := []struct {
		level, kills int64
		want         string
	}{
		{0, 0, ""},                    // fresh — no renown
		{3, 10, ""},                   // not enough yet
		{1, 25, "the Brave"},          // first kill milestone
		{15, 30, "the Seasoned"},      // level track beats low kills
		{53, 707, "the Dragonslayer"}, // the live character
		{50, 0, "the Ascended"},       // pure level legend
		{10, 1000, "the Annihilator"}, // pure combat renown
		{100, 0, "the Eternal"},       // top of the level ladder
		{120, 2000, "the Eternal"},    // most prestigious wins
	}
	for _, c := range cases {
		got := titleFor(map[string]int64{"level": c.level, "kills": c.kills})
		if got != c.want {
			t.Errorf("titleFor(lvl=%d kills=%d) = %q; want %q", c.level, c.kills, got, c.want)
		}
	}
}

func TestStatusShowsTitle(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	// promote alice into Dragonslayer territory (500+ kills), then read her status.
	st.HSet(ctx, sheetKey("net|alice"), "kills", 600)
	m.Handle(chanMsg("alice", "!rpg"))
	if !r.has("the Dragonslayer") {
		t.Fatalf("status should show the earned title, got %q", r.last())
	}
}
