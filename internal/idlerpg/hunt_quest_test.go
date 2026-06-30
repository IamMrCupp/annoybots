package idlerpg

import (
	"context"
	"testing"
	"time"
)

func TestHuntQuestCompletesOnKills(t *testing.T) {
	m, r, _ := newMgr()
	ctx := context.Background()
	m.now = func() time.Time { return time.Unix(1000, 0) }
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")

	m.startHuntQuest(ctx, m.draftParty())
	if m.quest == nil || m.quest.Kind != "hunt" || m.quest.Target < 1 {
		t.Fatalf("a hunt should be active with a target, got %+v", m.quest)
	}
	if !r.has("a hunt begins") {
		t.Fatalf("hunt should announce, got %v", r.lines)
	}
	target := m.quest.Target
	// credit kills from a party member until the target is reached
	for i := int64(0); i < target; i++ {
		m.questKillCredit(ctx, "net|alice")
	}
	if m.quest != nil {
		t.Fatal("the hunt should complete once the target is reached")
	}
	if !r.has("the hunt is done") {
		t.Fatalf("completed hunt should announce, got %v", r.lines)
	}
}

func TestHuntIgnoresNonPartyKills(t *testing.T) {
	m, _, _ := newMgr()
	ctx := context.Background()
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")
	m.startHuntQuest(ctx, m.draftParty())
	before := m.quest.Progress
	m.questKillCredit(ctx, "net|stranger") // not on the party
	if m.quest.Progress != before {
		t.Fatal("a non-party kill must not count toward the hunt")
	}
}

func TestHuntFailsOnTimeout(t *testing.T) {
	m, r, _ := newMgr()
	ctx := context.Background()
	base := time.Unix(1000, 0)
	m.now = func() time.Time { return base }
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")
	m.startHuntQuest(ctx, m.draftParty())
	m.now = func() time.Time { return base.Add(3 * time.Hour) } // past the deadline
	m.questTick(ctx)
	if m.quest != nil {
		t.Fatal("an expired hunt should be cleared")
	}
	if !r.has("the hunt fails") {
		t.Fatalf("expired hunt should announce failure, got %v", r.lines)
	}
}
