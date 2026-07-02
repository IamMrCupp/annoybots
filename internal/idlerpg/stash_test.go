package idlerpg

import (
	"context"
	"testing"
)

func TestStashAndEquipSwap(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	// equip a rare weapon lvl 20
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 20)
	st.HSet(ctx, sheetKey("net|alice"), rarityField("weapon"), 2) // rare
	st.SetStr(ctx, nameKey("net|alice", "weapon"), "Oldfang")

	// empty stash first
	m.Handle(chanMsg("alice", "!rpg stash"))
	if !r.has("stash is empty") {
		t.Fatalf("fresh stash should be empty, got %q", r.last())
	}
	// bank the weapon
	m.Handle(chanMsg("alice", "!rpg stash weapon"))
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s[itemField("weapon")] != 0 {
		t.Fatal("banking should clear the equipped slot")
	}
	stash := m.readStash(ctx, "net|alice")
	if len(stash) != 1 || stash[0].Level != 20 || stash[0].Name != "Oldfang" {
		t.Fatalf("stash should hold the banked weapon, got %+v", stash)
	}
	// equip a NEW weapon, then equip the stashed one back (swap)
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 5)
	st.HSet(ctx, sheetKey("net|alice"), rarityField("weapon"), 0)
	m.Handle(chanMsg("alice", "!rpg equip 1"))
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if s[itemField("weapon")] != 20 || s[rarityField("weapon")] != 2 {
		t.Fatalf("equip should restore the stashed weapon, got lvl %d rar %d", s[itemField("weapon")], s[rarityField("weapon")])
	}
	if nm, _ := st.GetStr(ctx, nameKey("net|alice", "weapon")); nm != "Oldfang" {
		t.Fatalf("equip should restore the item's name, got %q", nm)
	}
	// the level-5 weapon that was equipped should now be in the stash (swapped)
	stash = m.readStash(ctx, "net|alice")
	if len(stash) != 1 || stash[0].Level != 5 {
		t.Fatalf("the swapped-out weapon should be in the stash, got %+v", stash)
	}
}

func TestStashRejectsEmptySlot(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg stash weapon")) // nothing equipped
	if !r.has("no weapon equipped to bank") {
		t.Fatalf("stashing an empty slot should be refused, got %q", r.last())
	}
}
