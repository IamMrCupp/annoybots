package admin

import (
	"context"
	"strings"
	"testing"
)

// The reported bug: "!part EMP #tns" replied "parting #tns on EMP" while the
// configured network was "empradio", so nothing happened and the bot looked
// broken. A wrong name must never report success.
func TestPartRejectsUnknownNetworkAndNamesTheRealOnes(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())

	m.Handle(context.Background(), partyMsg("testnet", "alice", "!part EMP #tns"))

	var said string
	for _, s := range c.said {
		if strings.HasPrefix(s, "SAY testnet alice ") {
			said = s
		}
		if strings.HasPrefix(s, "PART ") {
			t.Fatalf("an unknown network must not part anything, got %#v", c.said)
		}
	}
	if strings.Contains(said, "parting") {
		t.Fatalf("it must not claim success for an unknown network: %q", said)
	}
	if !strings.Contains(said, "unknown network") {
		t.Fatalf("it should say the name is unknown, got %q", said)
	}
	// and it should tell you what the valid names are, so the fix is immediate
	if !strings.Contains(said, "testnet") {
		t.Fatalf("it should list the networks it knows, got %q", said)
	}
}

func TestJoinAndInviteAlsoValidate(t *testing.T) {
	for _, cmd := range []string{"!join EMP #tns", "!invite EMP #tns bob"} {
		c := &fakeControl{}
		m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())
		m.Handle(context.Background(), partyMsg("testnet", "alice", cmd))
		for _, s := range c.said {
			if strings.HasPrefix(s, "JOIN ") || strings.HasPrefix(s, "INVITE ") {
				t.Fatalf("%q must not act on an unknown network, got %#v", cmd, c.said)
			}
			if strings.Contains(s, "joining") || strings.Contains(s, "inviting") {
				t.Fatalf("%q must not claim success, got %q", cmd, s)
			}
		}
	}
}

func TestNetworkNamesAreCaseInsensitive(t *testing.T) {
	// A correct name in the wrong case should work rather than silently no-op.
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())

	m.Handle(context.Background(), partyMsg("testnet", "alice", "!part TESTNET #tns"))
	if !c.contains("PART TESTNET #tns") {
		t.Fatalf("a case-different but real network should still part, got %#v", c.said)
	}
}

func TestKnownNetworkAcceptsTheExactName(t *testing.T) {
	c := &fakeControl{}
	m := New("arywen", bossConfig(), "", &fakeQuoter{}, c, nil, quietLog())
	m.Handle(context.Background(), partyMsg("testnet", "alice", "!part testnet #tns"))
	if !c.contains("PART testnet #tns") {
		t.Fatalf("the exact name must still work, got %#v", c.said)
	}
	if !c.contains("SAY testnet alice parting #tns on testnet") {
		t.Fatalf("a real part should still confirm, got %#v", c.said)
	}
}
