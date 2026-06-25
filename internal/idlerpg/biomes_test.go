package idlerpg

import "testing"

func TestBiomeOfNearestTown(t *testing.T) {
	// A point sitting on a town takes that town's terrain.
	for _, tn := range towns {
		if got := biomeOf(int64(tn.X), int64(tn.Y)); got != tn.Terrain {
			t.Errorf("biomeOf(%s) = %q; want %q", tn.Name, got, tn.Terrain)
		}
	}
}

func TestPickMonsterRespectsBiome(t *testing.T) {
	m, _, _ := newMgr()
	// In coastal terrain at a high level, every foe must be either anywhere-roaming
	// ("") or coast-themed — never a forest/swamp/mountain/plains specialist.
	sawCoast := false
	for i := 0; i < 400; i++ {
		mon := m.pickMonster(30, "coast")
		if mon.Biome != "" && mon.Biome != "coast" {
			t.Fatalf("coast picked an off-biome foe: %q (%s)", mon.Name, mon.Biome)
		}
		if mon.Biome == "coast" {
			sawCoast = true
		}
	}
	if !sawCoast {
		t.Fatal("expected at least one coast-specific foe across many picks")
	}
}

func TestPickBossRespectsBiome(t *testing.T) {
	m, _, _ := newMgr()
	// The Kraken is coast-only; in the mountains a high-level idler should never
	// draw it, but Tiamat (mountain) or an anywhere-boss is fair game.
	for i := 0; i < 400; i++ {
		if b, ok := m.pickBoss(40, "mountain"); ok {
			if b.Biome != "" && b.Biome != "mountain" {
				t.Fatalf("mountain drew an off-biome boss: %q (%s)", b.Name, b.Biome)
			}
		}
	}
}
