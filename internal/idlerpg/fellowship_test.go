package idlerpg

import "testing"

func TestFellowshipPct(t *testing.T) {
	cases := map[int]int64{0: 100, 1: 100, 2: 105, 3: 110, 7: 130, 20: 130}
	for online, want := range cases {
		if got := fellowshipPct(online); got != want {
			t.Errorf("fellowshipPct(%d) = %d; want %d", online, got, want)
		}
	}
}

func TestFellowshipShowsInWho(t *testing.T) {
	m, _, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	got := m.who(chanMsg("alice", "!rpg who"))
	if !contains(got, "fellowship") || !contains(got, "+5%") {
		t.Fatalf("who should note the fellowship bonus with 2 online, got %q", got)
	}
}
