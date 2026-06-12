package admin

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mrcupp/annoybots/internal/botnet"
	"github.com/mrcupp/annoybots/internal/engine"
)

type fakeQuoter struct {
	mu    sync.Mutex
	added [][2]string
}

func (f *fakeQuoter) AddQuote(pack, line string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, [2]string{pack, line})
	return true
}
func (f *fakeQuoter) DelQuote(_, _ string) bool { return true }
func (f *fakeQuoter) PackNames() []string       { return nil }
func (f *fakeQuoter) addedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.added)
}

type fakeControl struct {
	mu   sync.Mutex
	said []string
}

func (f *fakeControl) record(s string)          { f.mu.Lock(); f.said = append(f.said, s); f.mu.Unlock() }
func (f *fakeControl) Say(n, t, x string)       { f.record("SAY " + n + " " + t + " " + x) }
func (f *fakeControl) Action(n, t, x string)    { f.record("ACT " + n + " " + t + " " + x) }
func (f *fakeControl) Join(n, c string)         { f.record("JOIN " + n + " " + c) }
func (f *fakeControl) Part(n, c string)         { f.record("PART " + n + " " + c) }
func (f *fakeControl) Invite(n, nick, c string) { f.record("INVITE " + n + " " + nick + " " + c) }
func (f *fakeControl) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.said) == 0 {
		return ""
	}
	return f.said[len(f.said)-1]
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func dm(account, text string) engine.Message {
	return engine.Message{Network: "testnet", Channel: "admin", Nick: "someone", Text: text, Private: true, Account: account, Self: "arywen"}
}

func bossConfig() Config {
	return Config{Enabled: true, Admins: []Identity{{Network: "testnet", Account: "boss"}}}
}

func TestNonAdminRejected(t *testing.T) {
	q := &fakeQuoter{}
	c := &fakeControl{}
	m := New("arywen", bossConfig(), q, c, nil, quietLog())

	if !m.Handle(context.Background(), dm("rando", "!addquote rickmorty hello")) {
		t.Fatal("admin command should be consumed even when rejected")
	}
	if q.addedCount() != 0 {
		t.Fatal("non-admin must not be able to add quotes")
	}
	if !strings.Contains(c.last(), "not an admin") {
		t.Fatalf("expected rejection reply, got %q", c.last())
	}
}

func TestAdminAddQuote(t *testing.T) {
	q := &fakeQuoter{}
	c := &fakeControl{}
	m := New("arywen", bossConfig(), q, c, nil, quietLog())

	m.Handle(context.Background(), dm("boss", "!addquote rickmorty get schwifty please"))
	if q.addedCount() != 1 || q.added[0] != [2]string{"rickmorty", "get schwifty please"} {
		t.Fatalf("expected quote added, got %#v", q.added)
	}
}

func TestPublicCommandIgnored(t *testing.T) {
	m := New("arywen", bossConfig(), &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	pub := dm("boss", "!addquote rickmorty x")
	pub.Private = false
	if m.Handle(context.Background(), pub) {
		t.Fatal("admin commands must be ignored outside DMs")
	}
}

func TestInviteCommand(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), &fakeQuoter{}, c, nil, quietLog())
	m.Handle(context.Background(), dm("boss", "!invite testnet #secret bob"))
	found := false
	for _, s := range c.said {
		if s == "INVITE testnet bob #secret" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected INVITE control call, got %#v", c.said)
	}
}

func TestQuoteSyncOverBus(t *testing.T) {
	bus := botnet.NewMem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q1, q2 := &fakeQuoter{}, &fakeQuoter{}
	ary := New("arywen", bossConfig(), q1, &fakeControl{}, bus, quietLog())
	kur := New("kurkutu", bossConfig(), q2, &fakeControl{}, bus, quietLog())
	if err := ary.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := kur.Run(ctx); err != nil {
		t.Fatal(err)
	}

	// Admin DMs Arywen; the add should propagate to Kurkutu via the bus.
	ary.Handle(ctx, dm("boss", "!addquote shared a synced annoyance"))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if q2.addedCount() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if q2.addedCount() != 1 || q2.added[0] != [2]string{"shared", "a synced annoyance"} {
		t.Fatalf("sibling did not receive synced quote: %#v", q2.added)
	}
}

func TestAdminPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.json")
	cfg := bossConfig()
	cfg.StatePath = path

	m1 := New("arywen", cfg, &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	m1.Handle(context.Background(), dm("boss", "!addadmin testnet deputy"))

	// A fresh manager loading the same state file should recognize the new admin.
	m2 := New("arywen", cfg, &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	if !m2.isAdmin(dm("deputy", "!admins")) {
		t.Fatal("runtime-added admin should persist across restarts")
	}
}
