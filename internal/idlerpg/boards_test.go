package idlerpg

import (
	"context"
	"strings"
	"testing"
)

func TestTopBoardVariants(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	// Three players, distinct stats. Enroll places them on the level board.
	for _, n := range []string{"alice", "bob", "carol"} {
		m.Handle(chanMsg(n, "!rpg"))
	}
	// alice: most kills; bob: most gold; carol: most duel wins.
	st.HSet(ctx, sheetKey("net|alice"), "kills", 50)
	st.HSet(ctx, sheetKey("net|bob"), "gold", 9000)
	st.HSet(ctx, sheetKey("net|carol"), "duelw", 12)

	if got := m.topBoard("kills"); !strings.HasPrefix(got, "top by kills:") || !strings.Contains(got, "alice (50)") {
		t.Fatalf("kills board wrong: %q", got)
	}
	if got := m.topBoard("gold"); !strings.Contains(got, "bob (9000)") {
		t.Fatalf("gold board wrong: %q", got)
	}
	if got := m.topBoard("duels"); !strings.Contains(got, "carol (12)") {
		t.Fatalf("duels board wrong: %q", got)
	}
	// Unknown stat → usage hint; empty → classic level board.
	if got := m.topBoard("bananas"); !strings.Contains(got, "boards:") {
		t.Fatalf("unknown board should give usage, got %q", got)
	}
	if got := m.topBoard(""); !strings.Contains(got, "top idlers") {
		t.Fatalf("empty arg should be the level board, got %q", got)
	}
}

func TestTopBoardOrdersDescending(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	for _, n := range []string{"alice", "bob"} {
		m.Handle(chanMsg(n, "!rpg"))
	}
	st.HSet(ctx, sheetKey("net|alice"), "kills", 3)
	st.HSet(ctx, sheetKey("net|bob"), "kills", 99)
	got := m.topBoard("kills")
	if strings.Index(got, "bob") > strings.Index(got, "alice") {
		t.Fatalf("higher kills should rank first: %q", got)
	}
}
