package idlerpg

import (
	"context"
	"testing"
)

func TestBestiaryRecordsOnlyCataloguedSpecies(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))

	species := bestiary[0].Name
	m.recordKill(ctx, "net|alice", species)
	m.recordKill(ctx, "net|alice", "the lord of the Sunken Crypt") // a dungeon lord
	m.recordKill(ctx, "net|alice", "Bahamut the World-Wyrm")       // a world boss

	counts, _ := st.HGetAll(ctx, bestiaryKey("net|alice"))
	if counts[species] != 1 {
		t.Fatalf("a catalogued species should be recorded, got %v", counts)
	}
	if len(counts) != 1 {
		t.Fatalf("generated foes are not species and must not be recorded, got %v", counts)
	}
	// the realm tally tracks it too
	realm, _ := st.HGetAll(ctx, realmBestiaryKey())
	if realm[species] != 1 {
		t.Fatalf("the realm guide should count it, got %v", realm)
	}
}

func TestBestiaryProgressAndCompletionFeat(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	seen, total := m.charBestiary(ctx, "net|alice")
	if seen != 0 || total != len(speciesNames()) {
		t.Fatalf("a new character has met nothing: %d/%d", seen, total)
	}
	for _, name := range speciesNames() {
		m.recordKill(ctx, "net|alice", name)
	}
	seen, total = m.charBestiary(ctx, "net|alice")
	if seen != total {
		t.Fatalf("every species slain should complete the guide: %d/%d", seen, total)
	}
	m.checkCombatFeats(ctx, p, false)
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if s["feats"]&(1<<10) == 0 {
		t.Fatal("completing the bestiary should earn Naturalist")
	}
}

func TestReadBestiaryListsEverySpeciesWithLegendsLast(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.recordKill(ctx, "net|alice", bestiary[0].Name)

	entries, err := ReadBestiary(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(speciesNames()) {
		t.Fatalf("the guide should list every species: %d vs %d", len(entries), len(speciesNames()))
	}
	// legends sort last
	seenBoss := false
	for _, e := range entries {
		if e.Boss {
			seenBoss = true
		} else if seenBoss {
			t.Fatal("ordinary foes must sort before legends")
		}
	}
	// unmet species report zero
	var zero int
	for _, e := range entries {
		if e.Kills == 0 {
			zero++
		}
	}
	if zero == 0 {
		t.Fatal("a fresh realm should have unmet species")
	}
}

func TestCollectionLogRecordsFinds(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	for i := 0; i < 20; i++ {
		m.findItem(ctx, p, 60)
	}
	coll := CollectionOf(ctx, st, "net|alice")
	if len(coll) == 0 {
		t.Fatal("finds should be logged by rarity")
	}
	var total int64
	for _, c := range coll {
		total += c.Kills
	}
	if total == 0 {
		t.Fatal("the collection log should count finds")
	}
}

func TestBestiaryStatusCommand(t *testing.T) {
	m, r, _ := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.recordKill(ctx, "net|alice", bestiary[0].Name)
	m.recordKill(ctx, "net|alice", bestiary[0].Name)

	m.Handle(chanMsg("alice", "!rpg bestiary"))
	if !r.has("bestiary") || !r.has("species") {
		t.Fatalf("!rpg bestiary should report progress, got %q", r.last())
	}
	if !r.has("favourite prey") {
		t.Fatalf("it should name the favourite prey, got %q", r.last())
	}
	// an unknown player is handled
	got := m.bestiaryStatus(chanMsg("alice", "!rpg bestiary nobody"), []string{"!rpg", "bestiary", "nobody"})
	if !contains(got, "isn't playing") {
		t.Fatalf("unknown players are reported, got %q", got)
	}
}
