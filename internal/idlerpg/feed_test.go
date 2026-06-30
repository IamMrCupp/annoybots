package idlerpg

import (
	"context"
	"strings"
	"testing"
)

func TestDramaRecordsToFeed(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.drama("net", "#chan", "✨ alice has attained level 5!")
	m.drama("net", "#chan", "⚔️ alice slew a goblin")

	// it still announces in-channel
	if !r.has("attained level 5") {
		t.Fatalf("drama should still Say in-channel, got %v", r.lines)
	}
	// and it persists to the feed, newest-first
	feed, err := ReadFeed(ctx, st, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 2 {
		t.Fatalf("feed has %d entries; want 2", len(feed))
	}
	if !strings.Contains(feed[0].Text, "slew a goblin") {
		t.Fatalf("feed should be newest-first, got %q first", feed[0].Text)
	}
	if feed[0].Ts == 0 {
		t.Fatal("feed entries should carry a timestamp")
	}
}

func TestLevelUpHitsFeed(t *testing.T) {
	m, _, st := newMgr()
	m.Handle(chanMsg("alice", "!rpg")) // enroll, ttl=1
	m.Tick()                           // ttl→0→level up (a drama line)
	feed, _ := ReadFeed(context.Background(), st, 10)
	found := false
	for _, e := range feed {
		if strings.Contains(e.Text, "attained level") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a level-up should appear in the feed, got %+v", feed)
	}
}

func TestReadCharFeed(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.drama("net", "#c", "✨ alice has attained level 5!")
	m.drama("net", "#c", "⚔️ bob slew a goblin")
	m.drama("net", "#c", "🎖️ alice earns a feat — First Blood!")

	got, err := ReadCharFeed(ctx, st, "alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("alice's feed has %d entries; want 2 (hers only)", len(got))
	}
	for _, e := range got {
		if !strings.Contains(strings.ToLower(e.Text), "alice") {
			t.Fatalf("char feed leaked a non-alice entry: %q", e.Text)
		}
	}
}
