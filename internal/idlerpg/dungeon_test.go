package idlerpg

import (
	"context"
	"testing"
)

func TestDungeonDelveClearsRoomsAndEndsAtTheLord(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 30)
	st.HSet(ctx, sheetKey("net|alice"), "mx", 250)
	st.HSet(ctx, sheetKey("net|alice"), "my", 250)
	st.HSet(ctx, sheetKey("net|alice"), "dgn", 3)
	st.SetStr(ctx, dungeonKey("net|alice"), "the Sunken Crypt")
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	if !m.inDungeon(ctx, "net|alice") {
		t.Fatal("alice should be delving")
	}
	// Clear rooms until the delve ends: either she reaches the lord, or she's
	// dragged out. Either way "dgn" lands on 0 and the name key is cleared.
	for i := 0; i < 3 && m.inDungeon(ctx, "net|alice"); i++ {
		st.HSet(ctx, sheetKey("net|alice"), "dmg", 0) // keep her upright
		m.dungeonTick(ctx, p)
	}
	if m.inDungeon(ctx, "net|alice") {
		t.Fatal("three ticks should have finished a three-room dungeon")
	}
	if name, _ := st.GetStr(ctx, dungeonKey("net|alice")); name != "" {
		t.Fatalf("dungeon name should be cleared on exit, got %q", name)
	}
	if !r.has("the Sunken Crypt") {
		t.Fatalf("the delve should be narrated in-channel, got %q", r.last())
	}
}

func TestDungeonFinalRoomPlundersOnVictory(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	// A high level with no damage taken: she should beat the lord and loot it.
	st.HSet(ctx, sheetKey("net|alice"), "level", 90)
	st.HSet(ctx, sheetKey("net|alice"), "dgn", 1)
	st.SetStr(ctx, dungeonKey("net|alice"), "the Gloomforge")
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	m.dungeonTick(ctx, p)
	if !r.has("final chamber") {
		t.Fatalf("the last room should announce the lord, got %q", r.last())
	}
	if m.inDungeon(ctx, "net|alice") {
		t.Fatal("the delve should end after the final chamber")
	}
}

func TestDungeonDiscoverySkipsTravellersAndDelvers(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 10)
	st.HSet(ctx, sheetKey("net|alice"), "dest", 1) // marching to a town
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	for i := 0; i < 300; i++ {
		m.maybeDiscoverDungeon(ctx, p)
	}
	if m.inDungeon(ctx, "net|alice") {
		t.Fatal("a traveller should never stumble into a dungeon")
	}
}

func TestDungeonStatus(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))

	if got := m.dungeonStatus(chanMsg("alice", "!rpg dungeon")); !contains(got, "not delving") {
		t.Fatalf("idle player should report no delve, got %q", got)
	}
	st.HSet(ctx, sheetKey("net|alice"), "dgn", 4)
	st.SetStr(ctx, dungeonKey("net|alice"), "the Hollow Spire")
	got := m.dungeonStatus(chanMsg("alice", "!rpg dungeon"))
	if !contains(got, "the Hollow Spire") || !contains(got, "4 room") {
		t.Fatalf("delve status should name the dungeon and rooms left, got %q", got)
	}
}
