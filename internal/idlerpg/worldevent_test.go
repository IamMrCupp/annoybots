package idlerpg

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWorldEventStartsAndExpires(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	base := time.Unix(1000, 0)
	m.now = func() time.Time { return base }
	m.Handle(chanMsg("alice", "!rpg"))

	// force one to start by rolling until it fires (worldEventOdds is rare)
	for i := 0; i < 5000 && m.eventKind() == ""; i++ {
		m.worldEventTick(ctx)
	}
	if m.eventKind() == "" {
		t.Fatal("a world event should eventually begin")
	}
	if !r.has("falls over the realm") {
		t.Fatalf("world event should announce, got %v", r.lines)
	}
	if wv, _ := ReadWorldEvent(ctx, st, base.Unix()); wv == nil || wv.Name == "" {
		t.Fatal("the dashboard should see the active world event")
	}
	// expire it
	m.now = func() time.Time { return base.Add(2 * time.Hour) }
	m.worldEventTick(ctx)
	if m.eventKind() != "" {
		t.Fatal("an expired world event should be cleared")
	}
	if !r.has("passes. the realm returns") {
		t.Fatalf("expiry should announce, got %v", r.lines)
	}
}

func TestHarvestGoldBonus(t *testing.T) {
	m, _, _ := newMgr()
	if got := m.harvestGold(100); got != 100 {
		t.Fatalf("no event → gold unchanged, got %d", got)
	}
	m.emu.Lock()
	m.wevent = &worldEvent{Kind: "harvest"}
	m.emu.Unlock()
	if got := m.harvestGold(100); got != 150 {
		t.Fatalf("harvest → +50%%, got %d", got)
	}
}

func TestWorldEventShowsInInfo(t *testing.T) {
	m, _, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.emu.Lock()
	m.wevent = &worldEvent{Kind: "bloodmoon", Name: "a Blood Moon", Desc: "monsters prowl", Deadline: m.now().Unix() + 600}
	m.emu.Unlock()
	if got := m.info(); !strings.Contains(got, "Blood Moon") {
		t.Fatalf("info should mention the active world event, got %q", got)
	}
}
