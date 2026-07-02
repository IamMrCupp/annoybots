package idlerpg

import (
	"context"
	"testing"
)

func TestBuyMountAndTravelFaster(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 10)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 100000)
	for _, tn := range towns { // stand at a market
		if tn.Service == "market" {
			st.HSet(ctx, sheetKey("net|alice"), "mx", int64(tn.X))
			st.HSet(ctx, sheetKey("net|alice"), "my", int64(tn.Y))
		}
	}

	m.Handle(chanMsg("alice", "!rpg mount")) // none yet
	if !r.has("travels on foot") {
		t.Fatalf("fresh player should be unmounted, got %q", r.last())
	}
	m.Handle(chanMsg("alice", "!rpg buy mount"))
	if !r.has("travel to towns is now twice as swift") {
		t.Fatalf("buy mount failed: %q", r.last())
	}
	if m.mountOf(ctx, "net|alice") == "" {
		t.Fatal("alice should now have a mount")
	}
	// can't double-buy
	m.Handle(chanMsg("alice", "!rpg buy mount"))
	if !r.has("already keeps a mount") {
		t.Fatalf("double mount buy should be refused, got %q", r.last())
	}
}
