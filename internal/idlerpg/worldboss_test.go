package idlerpg

import (
	"context"
	"strings"
	"testing"
)

func TestWorldBossSpawnAndSlay(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 5)
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 100000) // huge contribution → quick kill

	m.spawnWorldBoss(ctx, "net", "#chan")
	if !r.has("WORLD BOSS rises") {
		t.Fatalf("spawn should announce, got %v", r.lines)
	}
	if bv, _ := ReadWorldBoss(ctx, st); bv == nil || bv.HP <= 0 {
		t.Fatal("a raid should exist with positive HP")
	}
	for i := 0; i < 50; i++ {
		m.worldBossTick(ctx)
		if bv, _ := ReadWorldBoss(ctx, st); bv == nil {
			break // slain and cleared
		}
	}
	if bv, _ := ReadWorldBoss(ctx, st); bv != nil {
		t.Fatal("the raid should be slain and cleared")
	}
	if !r.has("is SLAIN") {
		t.Fatalf("victory should announce, got %v", r.lines)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["gold"] <= 0 || s["kills"] < 1 {
		t.Fatalf("a participant should be rewarded, sheet=%v", s)
	}
}

func TestWorldBossDeparts(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.spawnWorldBoss(ctx, "net", "#chan")
	m.bmu.Lock()
	m.boss.Deadline = m.now().Unix() - 1 // already expired
	m.bmu.Unlock()
	m.worldBossTick(ctx)
	if !r.has("departs unbroken") {
		t.Fatalf("an expired raid should depart, got %v", r.lines)
	}
	if bv, _ := ReadWorldBoss(ctx, st); bv != nil {
		t.Fatal("a departed raid should be cleared")
	}
}

func TestRaidAdminSummons(t *testing.T) {
	m, r, _ := newMgr()
	m.SetAuthz(allowAll)
	m.Handle(chanMsg("alice", "!rpg")) // someone online to scale against
	m.Handle(chanMsg("boss", "!rpg raid"))
	if !r.has("WORLD BOSS rises") {
		t.Fatalf("admin raid should summon a world boss, got %v", r.lines)
	}
	// a second summon is refused while one is active
	m.Handle(chanMsg("boss", "!rpg raid"))
	if !r.has("already stalks the realm") {
		t.Fatalf("double raid should be refused, got %q", r.last())
	}
}

func TestInfoShowsWorldBoss(t *testing.T) {
	m, _, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	if got := m.info(); strings.Contains(got, "WORLD BOSS") {
		t.Fatalf("no raid yet, info should not mention one: %q", got)
	}
	m.spawnWorldBoss(context.Background(), "net", "#chan")
	if got := m.info(); !strings.Contains(got, "WORLD BOSS") {
		t.Fatalf("active raid should show in info, got %q", got)
	}
}
