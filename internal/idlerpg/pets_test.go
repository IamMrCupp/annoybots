package idlerpg

import (
	"context"
	"testing"
)

func TestPetCombatBonusAndQuery(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))

	// No companion yet.
	m.Handle(chanMsg("alice", "!rpg pet"))
	if !r.has("no companion yet") {
		t.Fatalf("fresh player should have no pet, got %q", r.last())
	}

	// Grant a wolf; !rpg pet should describe it.
	st.SetStr(ctx, petKey("net|alice"), "wolf")
	pet, ok := m.petOf(ctx, "net|alice")
	if !ok || pet.Name != "wolf" {
		t.Fatalf("petOf wrong: %+v ok=%v", pet, ok)
	}
	m.Handle(chanMsg("alice", "!rpg pet"))
	if !r.has("wolf") || !r.has("atk") {
		t.Fatalf("pet status should describe the wolf, got %q", r.last())
	}
}

func TestBossCanTamePet(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 100000)
	sheet, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
	boss := monster{Name: "the Test Colossus", AC: 1, DmgDie: 1, HP: 1, Gold: 100, Boss: true}
	// Slay bosses until a companion is tamed (1-in-bossPetChance each); never on a non-boss.
	tamed := false
	for i := 0; i < 200 && !tamed; i++ {
		st.SetStr(ctx, petKey("net|alice"), "") // clear so each kill can re-roll
		m.resolveFight(ctx, p, sheet, "", boss)
		if _, ok := m.petOf(ctx, "net|alice"); ok {
			tamed = true
		}
	}
	if !tamed {
		t.Fatal("a boss kill should eventually tame a companion")
	}
}
