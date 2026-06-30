package idlerpg

import (
	"context"
	"testing"
)

func TestBlessBuysAndDecays(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 10)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 1000)
	// stand at a temple
	for _, tn := range towns {
		if tn.Service == "temple" {
			st.HSet(ctx, sheetKey("net|alice"), "mx", int64(tn.X))
			st.HSet(ctx, sheetKey("net|alice"), "my", int64(tn.Y))
		}
	}
	m.Handle(chanMsg("alice", "!rpg bless"))
	if !r.has("blesses alice") {
		t.Fatalf("bless reply wrong: %q", r.last())
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); !blessed(s) {
		t.Fatal("alice should be blessed")
	}
	// can't double-bless
	m.Handle(chanMsg("alice", "!rpg bless"))
	if !r.has("already walks under a blessing") {
		t.Fatalf("double-bless should be refused, got %q", r.last())
	}
	// it decays over ticks
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
	for i := 0; i < blessTicks; i++ {
		m.tickStatus(ctx, p)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); blessed(s) {
		t.Fatal("blessing should have lifted after its duration")
	}
}
