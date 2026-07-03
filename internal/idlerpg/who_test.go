package idlerpg

import (
	"context"
	"testing"
)

func TestWhoListsOnline(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 20)
	st.HSet(ctx, sheetKey("net|bob"), "level", 5)

	got := m.who(chanMsg("alice", "!rpg who"))
	if got == "" || !contains(got, "alice (lvl 20)") || !contains(got, "bob (lvl 5)") {
		t.Fatalf("who should list online idlers with levels, got %q", got)
	}
	// alice (higher level) should come before bob
	ai, bi := indexOf(got, "alice"), indexOf(got, "bob")
	if ai < 0 || bi < 0 || ai > bi {
		t.Fatalf("who should be sorted by level desc, got %q", got)
	}
}

func TestWhoEmpty(t *testing.T) {
	m, _, _ := newMgr()
	if got := m.who(chanMsg("nobody", "!rpg who")); !contains(got, "no one is idling") {
		t.Fatalf("empty who should say so, got %q", got)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
