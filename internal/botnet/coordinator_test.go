package botnet

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mrcupp/annoybots/internal/engine"
)

type recorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *recorder) Say(_, _, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, text)
}
func (r *recorder) Action(_, _, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, "*"+text+"*")
}
func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func duet() []Skit {
	return []Skit{{
		Name: "duet",
		Steps: []SkitStep{
			{Bot: "arywen", Line: "A1"},
			{Bot: "kurkutu", Line: "K1"},
			{Bot: "arywen", Line: "A2"},
		},
	}}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func TestSkitPerformedInLockstepAcrossBots(t *testing.T) {
	bus := NewMem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ra, rk := &recorder{}, &recorder{}
	ary := NewCoordinator("arywen", bus, ra, duet(), quietLogger(), Options{})
	kur := NewCoordinator("kurkutu", bus, rk, duet(), quietLogger(), Options{})
	if err := ary.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := kur.Run(ctx); err != nil {
		t.Fatal(err)
	}

	// A human types "!skit duet"; only arywen (the lead) initiates.
	ary.OnMessage(ctx, engine.Message{Network: "net", Channel: "#chan", Nick: "human", Text: "!skit duet"})

	waitFor(t, func() bool { return len(ra.snapshot()) == 2 && len(rk.snapshot()) == 1 })

	if got := ra.snapshot(); got[0] != "A1" || got[1] != "A2" {
		t.Fatalf("arywen lines wrong/out of order: %#v", got)
	}
	if got := rk.snapshot(); got[0] != "K1" {
		t.Fatalf("kurkutu line wrong: %#v", got)
	}
}

func TestNonLeadDoesNotInitiate(t *testing.T) {
	bus := NewMem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rk := &recorder{}
	kur := NewCoordinator("kurkutu", bus, rk, duet(), quietLogger(), Options{})
	if err := kur.Run(ctx); err != nil {
		t.Fatal(err)
	}

	// Kurkutu is not the lead of "duet" (arywen is), so this must do nothing.
	kur.OnMessage(ctx, engine.Message{Network: "net", Channel: "#chan", Nick: "human", Text: "!skit duet"})

	time.Sleep(100 * time.Millisecond)
	if n := len(rk.snapshot()); n != 0 {
		t.Fatalf("non-lead bot should not have started the skit, got %d lines", n)
	}
}

func TestSkitCooldownBlocksRestart(t *testing.T) {
	bus := NewMem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Unix(0, 0)
	ra := &recorder{}
	// A solo skit (all steps arywen) so it completes with only one bot running.
	skits := []Skit{{
		Name:     "solo",
		Cooldown: engine.Duration(time.Minute),
		Steps: []SkitStep{
			{Bot: "arywen", Line: "S1"},
			{Bot: "arywen", Line: "S2"},
		},
	}}
	ary := NewCoordinator("arywen", bus, ra, skits, quietLogger(), Options{Now: func() time.Time { return now }})
	if err := ary.Run(ctx); err != nil {
		t.Fatal(err)
	}

	ary.OnMessage(ctx, engine.Message{Network: "net", Channel: "#chan", Nick: "h", Text: "!skit solo"})
	waitFor(t, func() bool { return len(ra.snapshot()) == 2 }) // S1, S2

	ary.OnMessage(ctx, engine.Message{Network: "net", Channel: "#chan", Nick: "h", Text: "!skit solo"})
	time.Sleep(100 * time.Millisecond)
	if n := len(ra.snapshot()); n != 2 {
		t.Fatalf("cooldown should have blocked the restart, got %d lines", n)
	}
}
