package admin

import (
	"context"
	"testing"
	"time"

	"github.com/IamMrCupp/annoybots/internal/botnet"
	"github.com/IamMrCupp/annoybots/internal/engine"
)

// partyMsg is a DM from an admin (account "boss") with a distinct nick, so we can
// have several partyline members. Channel == nick is the DM reply target.
func partyMsg(network, nick, text string) engine.Message {
	return engine.Message{
		Network: network, Channel: nick, Nick: nick, Text: text,
		Private: true, Account: "boss", Self: "arywen",
	}
}

func (f *fakeControl) contains(want string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.said {
		if s == want {
			return true
		}
	}
	return false
}

func TestPartylineJoinAndRelay(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())
	ctx := context.Background()

	m.Handle(ctx, partyMsg("testnet", "alice", "!party"))
	m.Handle(ctx, partyMsg("testnet", "bob", "!party"))

	// alice's plain DM should relay to bob, not echo back to alice.
	if !m.Handle(ctx, partyMsg("testnet", "alice", "hello everyone")) {
		t.Fatal("party member's plain DM should be consumed")
	}
	if !c.contains("SAY testnet bob [party] <alice> hello everyone") {
		t.Fatalf("bob should receive the partyline line, got %#v", c.said)
	}
	if c.contains("SAY testnet alice [party] <alice> hello everyone") {
		t.Fatal("sender should not get their own line echoed")
	}
}

func TestPartylineNonMemberPlainDMIgnored(t *testing.T) {
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	// carol is an admin but hasn't joined the partyline; her plain DM is not ours.
	if m.Handle(context.Background(), partyMsg("testnet", "carol", "just chatting")) {
		t.Fatal("a non-member's plain DM must not be consumed as partyline")
	}
}

func TestPartylineUnpartyStopsRelay(t *testing.T) {
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	ctx := context.Background()
	m.Handle(ctx, partyMsg("testnet", "alice", "!party"))
	m.Handle(ctx, partyMsg("testnet", "alice", "!unparty"))
	if m.Handle(ctx, partyMsg("testnet", "alice", "still here?")) {
		t.Fatal("after !unparty, plain DMs should no longer be relayed")
	}
}

func TestPartylineCrossBus(t *testing.T) {
	bus := botnet.NewMem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ca, ck := &fakeControl{}, &fakeControl{}
	ary := New("arywen", bossConfig(), "", &fakeQuoter{}, ca, bus, quietLog())
	kur := New("kurkutu", bossConfig(), "", &fakeQuoter{}, ck, bus, quietLog())
	if err := ary.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := kur.Run(ctx); err != nil {
		t.Fatal(err)
	}

	// bob joins via kurkutu; alice joins via arywen and speaks.
	kur.Handle(ctx, partyMsg("testnet", "bob", "!party"))
	ary.Handle(ctx, partyMsg("testnet", "alice", "!party"))
	ary.Handle(ctx, partyMsg("testnet", "alice", "cross-bot hi"))

	// bob (on kurkutu) should receive alice's line, relayed over the bus.
	want := "SAY testnet bob [party] <alice> cross-bot hi"
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ck.contains(want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("bob did not receive the cross-bot partyline line: %#v", ck.said)
}
