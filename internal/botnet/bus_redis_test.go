package botnet

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// TestRedisBusRoundTrip exercises the real Redis pub/sub path against an embedded
// in-process Redis, proving Publish/Subscribe marshal and route events correctly.
func TestRedisBusRoundTrip(t *testing.T) {
	srv := miniredis.RunT(t)
	bus := NewRedis(srv.Addr(), "", "annoybots")
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := bus.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	ch, err := bus.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	want := Event{Type: EventSkitStart, From: "arywen", Network: "net", Channel: "#c", Skit: "duet", Nonce: "n1"}

	// The subscription registers asynchronously, so publish until it lands.
	deadline := time.After(4 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case got := <-ch:
			if got != want {
				t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", got, want)
			}
			return
		case <-tick.C:
			if err := bus.Publish(ctx, want); err != nil {
				t.Fatalf("publish: %v", err)
			}
		case <-deadline:
			t.Fatal("did not receive published event in time")
		}
	}
}

// TestRedisBusLinkAuthRoundTrip proves signed events survive the real pub/sub
// path when both ends share a secret.
func TestRedisBusLinkAuthRoundTrip(t *testing.T) {
	srv := miniredis.RunT(t)
	bus := NewRedis(srv.Addr(), "", "annoybots")
	bus.SetSecret("shared")
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := bus.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	want := Event{Type: EventAdminAdd, From: "arywen", Account: "alice", Flags: "o"}
	if err := bus.Publish(ctx, want); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case got := <-ch:
		if got.Account != "alice" || got.Flags != "o" {
			t.Fatalf("event mangled in flight: %+v", got)
		}
		if got.Sig == "" {
			t.Fatal("a published event should carry a signature")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for a signed event")
	}
	if bus.Dropped() != 0 {
		t.Fatalf("nothing should have been dropped, got %d", bus.Dropped())
	}
}

// TestRedisBusRejectsForgedEvents is the attack this feature exists to stop: a
// stranger with Redis access publishing an unsigned admin grant.
func TestRedisBusRejectsForgedEvents(t *testing.T) {
	srv := miniredis.RunT(t)
	victim := NewRedis(srv.Addr(), "", "annoybots")
	victim.SetSecret("shared")
	defer victim.Close()

	attacker := NewRedis(srv.Addr(), "", "annoybots") // no secret at all
	defer attacker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := victim.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// forge an owner grant
	if err := attacker.Publish(ctx, Event{Type: EventAdminAdd, From: "arywen", Account: "mallory", Flags: "n"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// and one signed with the wrong key
	wrong := NewRedis(srv.Addr(), "", "annoybots")
	wrong.SetSecret("not-the-secret")
	defer wrong.Close()
	if err := wrong.Publish(ctx, Event{Type: EventAdminAdd, Account: "mallory", Flags: "n"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-ch:
		t.Fatalf("a forged event reached a consumer: %+v", got)
	case <-time.After(500 * time.Millisecond):
		// nothing delivered, as intended
	}
	if victim.Dropped() < 2 {
		t.Fatalf("both forgeries should be counted as dropped, got %d", victim.Dropped())
	}
}

// TestRedisBusUnauthenticatedStillWorks keeps the rollout safe: with no secret
// configured the bus behaves exactly as it did before link auth existed.
func TestRedisBusUnauthenticatedStillWorks(t *testing.T) {
	srv := miniredis.RunT(t)
	bus := NewRedis(srv.Addr(), "", "annoybots") // no SetSecret
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := bus.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Publish(ctx, Event{Type: EventPartyline, Text: "hi"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case got := <-ch:
		if got.Text != "hi" {
			t.Fatalf("unexpected event: %+v", got)
		}
		if got.Sig != "" {
			t.Fatal("an unauthenticated bus should not sign")
		}
	case <-ctx.Done():
		t.Fatal("timed out; an unauthenticated bus must still deliver")
	}
}
