package idlerpg

import (
	"context"
	"testing"
)

func TestFeatListAndAward(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	m.awardFeat(ctx, p, 1<<0) // First Blood
	if !r.has("earns a feat") || !r.has("First Blood") {
		t.Fatalf("award should announce, got %v", r.lines)
	}
	// Idempotent: a second award of the same bit says nothing new.
	n := len(r.lines)
	m.awardFeat(ctx, p, 1<<0)
	if len(r.lines) != n {
		t.Fatal("re-awarding an earned feat must not re-announce")
	}
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if got := featList(s["feats"]); len(got) != 1 || got[0] != "First Blood" {
		t.Fatalf("featList = %v; want [First Blood]", got)
	}
}

func TestCombatFeatsThresholds(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	st.HSet(ctx, sheetKey("net|alice"), "kills", 100)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 1000)
	m.checkCombatFeats(ctx, p, true) // boss kill at 100 kills / 1000 gold
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	got := featList(s["feats"])
	want := map[string]bool{"First Blood": true, "Centurion (100 kills)": true, "Giant-Slayer (a boss falls)": true, "Deep Pockets (1000 gold)": true}
	if len(got) != len(want) {
		t.Fatalf("feats = %v; want %d distinct", got, len(want))
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("unexpected feat %q in %v", g, got)
		}
	}
}

func TestFeatsCommand(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg feats"))
	if !r.has("no feats yet") {
		t.Fatalf("fresh player should have no feats, got %q", r.last())
	}
	st.HSet(ctx, sheetKey("net|alice"), "feats", (1<<0)|(1<<3))
	m.Handle(chanMsg("alice", "!rpg feats"))
	if !r.has("First Blood") || !r.has("Giant-Slayer") || !r.has("2/") {
		t.Fatalf("feats list wrong: %q", r.last())
	}
}
