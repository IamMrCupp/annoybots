package idlerpg

import "testing"

func TestCatalog(t *testing.T) {
	if got := Classes(); len(got) != len(classes) || got[0].Name == "" || got[0].Ability == "" {
		t.Fatalf("Classes() wrong: %+v", got)
	}
	if got := Races(); len(got) != len(races) || got[0].Bonus == "" {
		t.Fatalf("Races() wrong: %+v", got)
	}
	if got := Pets(); len(got) != len(petKinds) {
		t.Fatalf("Pets() = %d; want %d", len(got), len(petKinds))
	}
	if got := Alignments(); len(got) != 9 {
		t.Fatalf("Alignments() = %d; want 9", len(got))
	}
	// classes/races are sorted by name
	cs := Classes()
	for i := 1; i < len(cs); i++ {
		if cs[i-1].Name > cs[i].Name {
			t.Fatal("Classes() not sorted")
		}
	}
	// a known race bonus formats correctly (elf = +2 DEX, +1 INT)
	for _, r := range Races() {
		if r.Name == "elf" && r.Bonus != "+2 DEX, +1 INT" {
			t.Fatalf("elf bonus = %q; want '+2 DEX, +1 INT'", r.Bonus)
		}
	}
}
