package idlerpg

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/IamMrCupp/annoybots/internal/state"
)

func TestTalkPenaltyScalesWithLevel(t *testing.T) {
	// a realistic base TTL so the level's duration (and thus the floor) is meaningful
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := New(state.NewMem(), &recorder{}, nil, time.Second, time.Hour, time.Hour, time.Hour, log)
	lo := m.talkPenalty(1, 20)
	hi := m.talkPenalty(40, 20)
	if hi <= lo {
		t.Fatalf("talk penalty should scale with level: lvl1=%d lvl40=%d", lo, hi)
	}
}

func TestTalkingAnnouncesPenalty(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg")) // enroll + online
	before, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	m.Handle(chanMsg("alice", "just chatting in here")) // talk → penalty + announce
	if !r.has("broke the silence") {
		t.Fatalf("talking should announce a visible penalty, got %v", r.lines)
	}
	after, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if after["ttl"] <= before["ttl"] {
		t.Fatalf("talking should add time: %d -> %d", before["ttl"], after["ttl"])
	}
}
