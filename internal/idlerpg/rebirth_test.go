package idlerpg

import (
	"context"
	"strings"
	"testing"
)

func TestRebirthRequiresLevel(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg")) // level 0
	m.Handle(chanMsg("alice", "!rpg rebirth"))
	if !r.has("must reach level") {
		t.Fatalf("low-level rebirth should be refused, got %q", r.last())
	}
}

func TestRebirthResetsButKeepsAndSpeedsUp(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 55)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 5000)
	st.HSet(ctx, sheetKey("net|alice"), "kills", 400)
	st.ZIncr(ctx, boardKey(), "net|alice", 55)

	m.Handle(chanMsg("alice", "!rpg rebirth"))
	if !r.has("is REBORN") {
		t.Fatalf("rebirth should announce, got %v", r.lines)
	}
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if s["level"] != 0 || s["reb"] != 1 {
		t.Fatalf("rebirth should reset level and bump reb: level=%d reb=%d", s["level"], s["reb"])
	}
	if s["gold"] != 5000 || s["kills"] != 400 {
		t.Fatal("rebirth should keep gold and kills")
	}
	if score, _ := st.ZScore(ctx, boardKey(), "net|alice"); score != 0 {
		t.Fatalf("rebirth should reset leaderboard rank, score=%d", score)
	}
	// speed bonus: a reborn character levels faster than a fresh one
	if m.ttlForReb(10, 1) >= m.ttlForReb(10, 0) {
		t.Fatal("a rebirth should shorten the time to level")
	}
}

func TestPrestigeStarsShowInStatus(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "reb", 3)
	m.Handle(chanMsg("alice", "!rpg")) // status line
	if !strings.Contains(r.last(), "★3") {
		t.Fatalf("status should show prestige stars, got %q", r.last())
	}
}
