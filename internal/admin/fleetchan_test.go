package admin

import (
	"context"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/botnet"
)

func TestPartAllActsLocallyAndBroadcasts(t *testing.T) {
	c := &fakeControl{}
	bus := botnet.NewMem()
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, bus, quietLog())
	ctx := context.Background()

	sub, err := bus.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m.Handle(ctx, partyMsg("testnet", "alice", "!partall testnet #tns"))

	// the bot we asked parts immediately
	if !c.contains("PART testnet #tns") {
		t.Fatalf("the receiving bot should part locally, got %#v", c.said)
	}
	// and the siblings are told
	select {
	case e := <-sub:
		if e.Type != botnet.EventPartChan || e.Network != "testnet" || e.Channel != "#tns" {
			t.Fatalf("unexpected broadcast: %+v", e)
		}
	default:
		t.Fatal("a part_chan event should have been published")
	}
}

func TestJoinAllActsLocallyAndBroadcasts(t *testing.T) {
	c := &fakeControl{}
	bus := botnet.NewMem()
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, bus, quietLog())
	ctx := context.Background()

	sub, err := bus.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m.Handle(ctx, partyMsg("testnet", "alice", "!joinall testnet #tns"))

	if !c.contains("JOIN testnet #tns") {
		t.Fatalf("the receiving bot should join locally, got %#v", c.said)
	}
	select {
	case e := <-sub:
		if e.Type != botnet.EventJoinChan {
			t.Fatalf("unexpected broadcast: %+v", e)
		}
	default:
		t.Fatal("a join_chan event should have been published")
	}
}

func TestSiblingActsOnABroadcastChannelControl(t *testing.T) {
	// The other side of the wire: a bot that didn't receive the command still acts.
	c := &fakeControl{}
	m := New("kurkutu", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())

	m.onBusEvent(botnet.Event{Type: botnet.EventPartChan, From: "arywen", Network: "testnet", Channel: "#tns"})
	if !c.contains("PART testnet #tns") {
		t.Fatalf("a sibling should part on a broadcast, got %#v", c.said)
	}
	m.onBusEvent(botnet.Event{Type: botnet.EventJoinChan, From: "arywen", Network: "testnet", Channel: "#back"})
	if !c.contains("JOIN testnet #back") {
		t.Fatalf("a sibling should join on a broadcast, got %#v", c.said)
	}
}

func TestChannelControlIgnoresOwnEchoAndJunk(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())

	// our own echo must not double-act (we already parted locally)
	m.onBusEvent(botnet.Event{Type: botnet.EventPartChan, From: "arywen", Network: "testnet", Channel: "#tns"})
	if c.contains("PART testnet #tns") {
		t.Fatalf("a bot must ignore its own echo, got %#v", c.said)
	}
	// malformed events are dropped rather than parting "" on ""
	m.applyChannelControl(botnet.EventPartChan, "", "#tns")
	m.applyChannelControl(botnet.EventPartChan, "testnet", "")
	if len(c.said) != 0 {
		t.Fatalf("incomplete events must be ignored, got %#v", c.said)
	}
}

func TestFleetChannelControlValidatesInput(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())
	ctx := context.Background()

	m.Handle(ctx, partyMsg("testnet", "alice", "!partall"))
	if !c.contains("SAY testnet alice usage: !partall <network> <#channel>") {
		t.Fatalf("a bare !partall should show usage, got %#v", c.said)
	}
	m.Handle(ctx, partyMsg("testnet", "alice", "!partall nosuchnet #tns"))
	if !c.contains("SAY testnet alice unknown network: nosuchnet") {
		t.Fatalf("an unknown network should be refused, got %#v", c.said)
	}
	// neither should have parted anything
	for _, s := range c.said {
		if len(s) > 4 && s[:4] == "PART" {
			t.Fatalf("nothing should have been parted, got %#v", c.said)
		}
	}
}

func TestPlainPartStaysSingleBot(t *testing.T) {
	// The non-destructive default must not change: !part affects only this bot.
	c := &fakeControl{}
	bus := botnet.NewMem()
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, bus, quietLog())
	ctx := context.Background()
	sub, err := bus.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}

	m.Handle(ctx, partyMsg("testnet", "alice", "!part testnet #tns"))
	if !c.contains("PART testnet #tns") {
		t.Fatalf("!part should still part locally, got %#v", c.said)
	}
	select {
	case e := <-sub:
		t.Fatalf("!part must not broadcast to the fleet, got %+v", e)
	default:
	}
}
