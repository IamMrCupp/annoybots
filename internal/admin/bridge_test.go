package admin

import (
	"context"
	"strings"
	"testing"
)

// saidWith reports how many recorded lines contain every one of the substrings.
func (f *fakeControl) saidWith(subs ...string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.said {
		ok := true
		for _, sub := range subs {
			if !strings.Contains(s, sub) {
				ok = false
				break
			}
		}
		if ok {
			n++
		}
	}
	return n
}

func (f *fakeControl) clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.said = nil
}

func newBridged(t *testing.T) (*Manager, *fakeControl, context.Context) {
	t.Helper()
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())
	return m, c, context.Background()
}

func TestPartylineBridgeEchoesToAChannel(t *testing.T) {
	m, c, ctx := newBridged(t)
	m.Handle(ctx, partyMsg("testnet", "alice", "!party"))
	m.Handle(ctx, partyMsg("testnet", "bob", "!party"))

	// Not bridged: chat reaches members only.
	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "hello"))
	if got := c.saidWith("#public"); got != 0 {
		t.Fatalf("nothing should reach a channel before bridging, got %#v", c.said)
	}

	m.Handle(ctx, partyMsg("testnet", "alice", "!bridge testnet #public"))
	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "hello again"))

	if got := c.saidWith("#public", "hello again"); got != 1 {
		t.Fatalf("bridged chat should reach #public exactly once, got %#v", c.said)
	}
	if got := c.saidWith("testnet bob", "hello again"); got != 1 {
		t.Fatalf("members should still be DMed, got %#v", c.said)
	}
}

func TestPartylineBridgeRejectsBadTargets(t *testing.T) {
	m, c, ctx := newBridged(t)
	m.Handle(ctx, partyMsg("testnet", "alice", "!party"))

	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "!bridge nosuchnet #public"))
	if c.saidWith("unknown network") == 0 {
		t.Fatalf("an unknown network should be refused, got %#v", c.said)
	}
	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "!bridge testnet notachannel"))
	if c.saidWith("doesn't look like a channel") == 0 {
		t.Fatalf("a non-channel should be refused, got %#v", c.said)
	}
	if m.bridgeOf() != nil {
		t.Fatal("no bridge should have been set")
	}
}

func TestPartylineUnbridge(t *testing.T) {
	m, c, ctx := newBridged(t)
	m.Handle(ctx, partyMsg("testnet", "alice", "!party"))
	m.Handle(ctx, partyMsg("testnet", "alice", "!bridge testnet #public"))

	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "!unbridge"))
	if c.saidWith("stopped") == 0 {
		t.Fatalf("unbridging should confirm, got %#v", c.said)
	}
	if m.bridgeOf() != nil {
		t.Fatal("the bridge should be cleared")
	}
	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "back to private"))
	if c.saidWith("#public") != 0 {
		t.Fatalf("nothing should reach #public after unbridging, got %#v", c.said)
	}
	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "!unbridge"))
	if c.saidWith("isn't bridged") == 0 {
		t.Fatalf("a second unbridge should say so, got %#v", c.said)
	}
}

func TestPartylineBridgeIsOneWay(t *testing.T) {
	// Public channel chatter must never flow back onto the partyline.
	m, c, ctx := newBridged(t)
	m.Handle(ctx, partyMsg("testnet", "alice", "!party"))
	m.Handle(ctx, partyMsg("testnet", "bob", "!party"))
	m.Handle(ctx, partyMsg("testnet", "alice", "!bridge testnet #public"))
	c.clear()

	public := partyMsg("testnet", "stranger", "hi from the channel")
	public.Private = false
	public.Channel = "#public"
	if m.Handle(ctx, public) {
		t.Fatal("a public channel line is not an admin command")
	}
	if c.saidWith("hi from the channel") != 0 {
		t.Fatalf("channel chatter must not be relayed onto the partyline, got %#v", c.said)
	}
}

func TestPartylineBridgeReportsAndReplaces(t *testing.T) {
	m, c, ctx := newBridged(t)
	m.Handle(ctx, partyMsg("testnet", "alice", "!party"))

	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "!bridge"))
	if c.saidWith("usage") == 0 {
		t.Fatalf("bare !bridge with no bridge set should show usage, got %#v", c.said)
	}
	m.Handle(ctx, partyMsg("testnet", "alice", "!bridge testnet #one"))
	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "!bridge"))
	if c.saidWith("#one") == 0 {
		t.Fatalf("bare !bridge should report the current target, got %#v", c.said)
	}
	c.clear()
	m.Handle(ctx, partyMsg("testnet", "alice", "!bridge testnet #two"))
	if c.saidWith("#two", "was") == 0 {
		t.Fatalf("re-bridging should name the previous target, got %#v", c.said)
	}
	if b := m.bridgeOf(); b == nil || b.channel != "#two" {
		t.Fatalf("the bridge should now point at #two, got %+v", b)
	}
}
