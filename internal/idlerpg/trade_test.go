package idlerpg

import (
	"context"
	"testing"
)

func TestGiveGold(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "gold", 1000)

	m.Handle(chanMsg("alice", "!rpg give bob 300"))
	if !r.has("gives 300g to bob") {
		t.Fatalf("gold gift should announce, got %q", r.last())
	}
	a, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	b, _ := st.HGetAll(ctx, sheetKey("net|bob"))
	if a["gold"] != 700 || b["gold"] != 300 {
		t.Fatalf("gold transfer wrong: alice=%d bob=%d", a["gold"], b["gold"])
	}
}

func TestGiveGoldGuards(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "gold", 50)

	m.Handle(chanMsg("alice", "!rpg give bob 100")) // too poor
	if !r.has("only has 50g") {
		t.Fatalf("overspend should be refused, got %q", r.last())
	}
	m.Handle(chanMsg("alice", "!rpg give alice 10")) // self
	if !r.has("can't give to yourself") {
		t.Fatalf("self-gift should be refused, got %q", r.last())
	}
	m.Handle(chanMsg("alice", "!rpg give ghost 10")) // not playing
	if !r.has("isn't playing") {
		t.Fatalf("gift to non-player should be refused, got %q", r.last())
	}
}

func TestGiveItem(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	// alice stashes a weapon, then gives it to bob
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 30)
	st.HSet(ctx, sheetKey("net|alice"), rarityField("weapon"), 3)
	m.Handle(chanMsg("alice", "!rpg stash weapon"))
	m.Handle(chanMsg("alice", "!rpg give bob item 1"))
	if !r.has("gives bob a") {
		t.Fatalf("item gift should announce, got %q", r.last())
	}
	if len(m.readStash(ctx, "net|alice")) != 0 {
		t.Fatal("alice's stash should be empty after giving")
	}
	bs := m.readStash(ctx, "net|bob")
	if len(bs) != 1 || bs[0].Level != 30 {
		t.Fatalf("bob should receive the item, got %+v", bs)
	}
}
