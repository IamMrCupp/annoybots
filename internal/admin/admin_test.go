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

	"github.com/IamMrCupp/annoybots/internal/botnet"
	"github.com/IamMrCupp/annoybots/internal/engine"
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
func (f *fakeControl) NetworkStatus() map[string]bool {
	return map[string]bool{"testnet": true, "discord": false}
}
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
	m := New("arywen", bossConfig(), "", q, c, nil, quietLog())

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
	m := New("arywen", bossConfig(), "", q, c, nil, quietLog())

	m.Handle(context.Background(), dm("boss", "!addquote rickmorty get schwifty please"))
	if q.addedCount() != 1 || q.added[0] != [2]string{"rickmorty", "get schwifty please"} {
		t.Fatalf("expected quote added, got %#v", q.added)
	}
}

func TestPublicCommandIgnored(t *testing.T) {
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	pub := dm("boss", "!addquote rickmorty x")
	pub.Private = false
	if m.Handle(context.Background(), pub) {
		t.Fatal("admin commands must be ignored outside DMs")
	}
}

func TestNetworksCommand(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())
	m.Handle(context.Background(), dm("boss", "!networks"))
	if !strings.Contains(c.last(), "testnet (connected)") || !strings.Contains(c.last(), "discord (offline)") {
		t.Fatalf("expected network status, got %q", c.last())
	}
}

func TestInviteCommand(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())
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
	ary := New("arywen", bossConfig(), "", q1, &fakeControl{}, bus, quietLog())
	kur := New("kurkutu", bossConfig(), "", q2, &fakeControl{}, bus, quietLog())
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

func TestPasswordLoginGrantsAndExpires(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "hunter2", &fakeQuoter{}, c, nil, quietLog())
	now := time.Unix(0, 0)
	m.now = func() time.Time { return now }

	user := dm("", "anything") // no verified identity
	if m.isAdmin(user) {
		t.Fatal("should not be admin before login")
	}

	m.Handle(context.Background(), dm("", "!login hunter2"))
	if !m.isAdmin(user) {
		t.Fatal("should be admin right after a correct login")
	}

	now = now.Add(31 * time.Minute) // past the 30m default TTL
	if m.isAdmin(user) {
		t.Fatal("session should have expired")
	}
}

func TestPasswordLoginWrongAndThrottle(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "hunter2", &fakeQuoter{}, c, nil, quietLog())
	now := time.Unix(0, 0)
	m.now = func() time.Time { return now }

	m.Handle(context.Background(), dm("", "!login wrong"))
	if m.isAdmin(dm("", "x")) {
		t.Fatal("wrong password must not authenticate")
	}
	if !strings.Contains(c.last(), "nope.") {
		t.Fatalf("expected failure reply, got %q", c.last())
	}

	for i := 0; i < 5; i++ {
		m.Handle(context.Background(), dm("", "!login wrong"))
	}
	if !strings.Contains(c.last(), "too many") {
		t.Fatalf("expected throttle after repeated failures, got %q", c.last())
	}
}

func TestPasswordLoginDisabled(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())
	m.Handle(context.Background(), dm("", "!login anything"))
	if !strings.Contains(c.last(), "disabled") {
		t.Fatalf("expected disabled reply when no password set, got %q", c.last())
	}
}

func TestLogoutClearsSession(t *testing.T) {
	m := New("arywen", bossConfig(), "hunter2", &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	m.Handle(context.Background(), dm("", "!login hunter2"))
	if !m.isAdmin(dm("", "x")) {
		t.Fatal("should be authed after login")
	}
	m.Handle(context.Background(), dm("", "!logout"))
	if m.isAdmin(dm("", "x")) {
		t.Fatal("logout should end the session")
	}
}

func TestReloadCommand(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())

	// Without a reload hook, the command reports unavailable.
	m.Handle(context.Background(), dm("boss", "!reload"))
	if !strings.Contains(c.last(), "not available") {
		t.Fatalf("expected unavailable reply, got %q", c.last())
	}

	called := false
	m.SetReload(func() (string, error) {
		called = true
		return "3 quote packs, 2 skits", nil
	})
	m.Handle(context.Background(), dm("boss", "!reload"))
	if !called {
		t.Fatal("reload hook should have been invoked")
	}
	if !strings.Contains(c.last(), "3 quote packs, 2 skits") {
		t.Fatalf("expected reload summary, got %q", c.last())
	}
}

func TestReloadRequiresAdmin(t *testing.T) {
	c := &fakeControl{}
	called := false
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())
	m.SetReload(func() (string, error) { called = true; return "x", nil })

	m.Handle(context.Background(), dm("rando", "!reload"))
	if called {
		t.Fatal("non-admin must not be able to reload")
	}
}

func TestAdminPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.json")
	cfg := bossConfig()
	cfg.StatePath = path

	m1 := New("arywen", cfg, "", &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	m1.Handle(context.Background(), dm("boss", "!addadmin testnet deputy"))

	// A fresh manager loading the same state file should recognize the new admin
	// at its persisted flag level (runtime admins default to op).
	m2 := New("arywen", cfg, "", &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	if !m2.has(dm("deputy", "!admins"), flagOp) {
		t.Fatal("runtime-added admin should persist across restarts")
	}
	if m2.has(dm("deputy", "!admins"), flagOwner) {
		t.Fatal("a default runtime admin should NOT have owner")
	}
}
