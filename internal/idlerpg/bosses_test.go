package idlerpg

import (
	"context"
	"testing"
)

func TestPickBossLevelGated(t *testing.T) {
	m, _, _ := newMgr()
	// Below every boss's MinLvl, no boss is ever eligible regardless of the roll.
	for i := 0; i < 200; i++ {
		if _, ok := m.pickBoss(5); ok {
			t.Fatal("a level-5 idler should never face a boss")
		}
	}
	// At a high level, a boss eventually appears (bossOdds chance per call).
	seen := false
	for i := 0; i < 500; i++ {
		if b, ok := m.pickBoss(40); ok {
			if !b.Boss || b.MinLvl > 40 {
				t.Fatalf("eligible boss wrong: %+v", b)
			}
			seen = true
			break
		}
	}
	if !seen {
		t.Fatal("a level-40 idler should eventually face a boss across many ticks")
	}
}

func TestBossKillRewards(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 30)
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 100000)
	sheet, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
	// A 1-HP, AC-1 boss is a guaranteed win — this isolates the boss reward branch
	// from combat RNG (real bosses have 160+ HP and are meant to be lost to).
	boss := monster{Name: "the Test Colossus", AC: 1, DmgDie: 1, HP: 1, Gold: 500, Boss: true}
	m.resolveFight(ctx, p, sheet, "", boss)
	after, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if after["kills"] != bossKills {
		t.Fatalf("a boss kill should grant %d kills, got %d", bossKills, after["kills"])
	}
	if after["gold"] != boss.Gold {
		t.Fatalf("boss gold = %d; want %d", after["gold"], boss.Gold)
	}
	if !r.has("has slain") || !r.has(boss.Name) {
		t.Fatalf("expected a boss-kill announcement, got %v", r.lines)
	}
	if itemSum(after) <= 0 {
		t.Fatal("a boss kill should drop loot")
	}
}
