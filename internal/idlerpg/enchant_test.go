package idlerpg

import (
	"context"
	"testing"
)

func TestEnchantUpgradesRarity(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 20)
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 20) // a common weapon
	st.HSet(ctx, sheetKey("net|alice"), rarityField("weapon"), 0)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 1000000)
	// stand at a market
	for _, tn := range towns {
		if tn.Service == "market" {
			st.HSet(ctx, sheetKey("net|alice"), "mx", int64(tn.X))
			st.HSet(ctx, sheetKey("net|alice"), "my", int64(tn.Y))
		}
	}
	m.Handle(chanMsg("alice", "!rpg enchant weapon"))
	if !r.has("enchants their weapon to uncommon") {
		t.Fatalf("enchant reply wrong: %q", r.last())
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s[rarityField("weapon")] != 1 {
		t.Fatalf("weapon rarity = %d; want 1 (uncommon)", s[rarityField("weapon")])
	}
}

func TestEnchantNeedsAnItemAndGold(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	for _, tn := range towns {
		if tn.Service == "market" {
			st.HSet(ctx, sheetKey("net|alice"), "mx", int64(tn.X))
			st.HSet(ctx, sheetKey("net|alice"), "my", int64(tn.Y))
		}
	}
	m.Handle(chanMsg("alice", "!rpg enchant weapon")) // no weapon
	if !r.has("no weapon to enchant") {
		t.Fatalf("expected no-item message, got %q", r.last())
	}
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 30)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 1) // too poor
	m.Handle(chanMsg("alice", "!rpg enchant weapon"))
	if !r.has("want") || !r.has("uncommon") {
		t.Fatalf("expected price message, got %q", r.last())
	}
}

func TestEnchantCaps(t *testing.T) {
	cost1 := enchantCost(20, 0)
	cost4 := enchantCost(20, 3)
	if cost4 <= cost1 {
		t.Fatalf("legendary step (%d) should cost more than the first (%d)", cost4, cost1)
	}
}
