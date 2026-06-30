package idlerpg

import (
	"context"
	"testing"
)

func TestPoisonSapsAndFades(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 20)
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	m.applyPoison(ctx, p, 2)
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); !poisoned(s) {
		t.Fatal("alice should be poisoned")
	}
	// two ticks of status: damage accrues, poison counts down to 0
	m.tickStatus(ctx, p)
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["dmg"] <= 0 {
		t.Fatal("poison should deal damage")
	}
	m.tickStatus(ctx, p)
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); poisoned(s) {
		t.Fatal("poison should have worn off after its duration")
	}
}

func TestQuaffCuresPoison(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "pots", 1)
	st.HSet(ctx, sheetKey("net|alice"), "poison", 3)
	m.Handle(chanMsg("alice", "!rpg quaff")) // healthy but poisoned → still allowed
	if !r.has("venom is purged") {
		t.Fatalf("quaff should cure poison, got %q", r.last())
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); poisoned(s) {
		t.Fatal("poison should be cured after quaffing")
	}
}

func TestVenomousMonsterPoisons(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 1) // low HP so it takes damage
	sheet, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
	// a venomous foe strong enough to land hits
	mon := monster{Name: "a will-o'-wisp", AC: 30, Atk: 30, DmgDie: 4, HP: 100}
	m.resolveFight(ctx, p, sheet, "", mon)
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); !poisoned(s) {
		t.Fatal("a venomous foe that drew blood should leave poison")
	}
}
