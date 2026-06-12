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
