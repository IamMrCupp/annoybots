package idlerpg

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/state"
)

// noticer is a recorder that also implements engine.Noticer.
type noticer struct {
	recorder
	notices []string
}

func (n *noticer) Notice(_, _, text string) { n.notices = append(n.notices, text) }

func TestTalkPenaltyUsesNoticeWhenAvailable(t *testing.T) {
	nt := &noticer{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := New(state.NewMem(), nt, nil, time.Second, time.Second, time.Hour, time.Hour, log)
	m.Handle(chanMsg("alice", "!rpg"))    // enroll + online
	m.Handle(chanMsg("alice", "chatter")) // talk → penalty
	found := false
	for _, s := range nt.notices {
		if contains(s, "broke the silence") {
			found = true
		}
	}
	if !found {
		t.Fatalf("penalty should go via NOTICE when supported, notices=%v", nt.notices)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

var _ engine.Noticer = (*noticer)(nil)
