package idlerpg

import (
	"context"
	"strings"
	"testing"
)

func TestDuelRequiresPresentOpponent(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg")) // alice online
	m.Handle(chanMsg("alice", "!rpg duel ghost"))
	if !r.has("isn't around to spar") {
		t.Fatalf("dueling an absent player should fail, got %q", r.last())
	}
}

func TestDuelSelf(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg duel alice"))
	if !r.has("shadowbox") {
		t.Fatalf("self-duel should be a draw, got %q", r.last())
	}
}

func TestDuelResolvesAndScores(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg")) // both online
	// Make alice overwhelmingly stronger so she reliably wins the best-of-three.
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 100000)
	m.Handle(chanMsg("alice", "!rpg duel bob"))
	if !r.has("spar") || !r.has("takes it") {
		t.Fatalf("expected a spar result, got %q", r.last())
	}
	if !strings.Contains(r.last(), "alice takes it") {
		t.Fatalf("the far stronger duelist should win, got %q", r.last())
	}
	if w, _ := st.HGetAll(ctx, sheetKey("net|alice")); w[duelWinsField()] != 1 {
		t.Fatalf("winner should have 1 career win, got %d", w[duelWinsField()])
	}
}

func TestOrdinal(t *testing.T) {
	cases := map[int64]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 101: "101st", 111: "111th"}
	for n, want := range cases {
		if got := ordinal(n); got != want {
			t.Errorf("ordinal(%d) = %q; want %q", n, got, want)
		}
	}
}
