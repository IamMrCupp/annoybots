package idlerpg

import (
	"context"
	"testing"
)

func TestNewFeatThresholds(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "kills", 5000)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 10000)
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
	m.checkCombatFeats(ctx, p, false)
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	got := map[string]bool{}
	for _, f := range featList(s["feats"]) {
		got[f] = true
	}
	for _, want := range []string{"Exterminator (5000 kills)", "Dragon-Hoard (10000 gold)"} {
		if !got[want] {
			t.Fatalf("expected feat %q, got %v", want, featList(s["feats"]))
		}
	}
}

func TestContentExpanded(t *testing.T) {
	// guard against accidental shrinkage of the variety tables
	if len(bestiary) < 25 || len(bosses) < 8 || len(worldBosses) < 9 {
		t.Fatalf("content tables shrank: bestiary=%d bosses=%d worldbosses=%d", len(bestiary), len(bosses), len(worldBosses))
	}
	if len(lootAdjs) < 25 || len(lootEpithets) < 20 {
		t.Fatalf("loot pools shrank: adjs=%d epithets=%d", len(lootAdjs), len(lootEpithets))
	}
	// every biome still has a sensible foe pool at a high level
	for _, b := range []string{"coast", "mountain", "forest", "swamp", "plains"} {
		m, _, _ := newMgr()
		mon := m.pickMonster(30, b)
		if mon.Name == "" {
			t.Fatalf("biome %q produced no monster", b)
		}
	}
}
