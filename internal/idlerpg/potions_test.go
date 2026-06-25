package idlerpg

import (
	"context"
	"testing"
)

// place a player at a market town (Lurk Harbor) so town-gated services work.
func atMarket(st interface {
	HSet(context.Context, string, string, int64) error
}, key string) {
	ctx := context.Background()
	for _, t := range towns {
		if t.Service == "market" {
			st.HSet(ctx, sheetKey(key), "mx", int64(t.X))
			st.HSet(ctx, sheetKey(key), "my", int64(t.Y))
			return
		}
	}
}

func TestBuyAndQuaffPotion(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 10)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 1000)
	atMarket(st, "net|alice")

	m.Handle(chanMsg("alice", "!rpg buy potion"))
	if !r.has("buys a healing draught") {
		t.Fatalf("buy potion failed: %q", r.last())
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["pots"] != 1 {
		t.Fatalf("pots = %d; want 1", s["pots"])
	}

	// hurt alice, then quaff from anywhere (move her off the town first).
	st.HSet(ctx, sheetKey("net|alice"), "mx", 5)
	st.HSet(ctx, sheetKey("net|alice"), "my", 5)
	st.HSet(ctx, sheetKey("net|alice"), "dmg", 999) // downed
	m.Handle(chanMsg("alice", "!rpg quaff"))
	if !r.has("restored to full HP") {
		t.Fatalf("quaff should heal: %q", r.last())
	}
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if s["pots"] != 0 || s["dmg"] != 0 {
		t.Fatalf("after quaff pots=%d dmg=%d; want 0/0", s["pots"], s["dmg"])
	}
}

func TestQuaffWithNoPotions(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg quaff"))
	if !r.has("no healing draughts") {
		t.Fatalf("quaff with empty pack should warn, got %q", r.last())
	}
}
