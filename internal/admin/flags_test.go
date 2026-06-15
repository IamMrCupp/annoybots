package admin

import (
	"context"
	"strings"
	"testing"
)

func TestHasFlagHierarchy(t *testing.T) {
	// owner grants everything; op grants op/voice/friend but not master/owner.
	cases := []struct {
		have string
		want byte
		ok   bool
	}{
		{"n", flagOwner, true}, {"n", flagMaster, true}, {"n", flagFriend, true},
		{"m", flagOwner, false}, {"m", flagMaster, true}, {"m", flagOp, true},
		{"o", flagMaster, false}, {"o", flagOp, true}, {"o", flagVoice, true}, {"o", flagFriend, true},
		{"f", flagFriend, true}, {"f", flagOp, false},
		{"", flagFriend, false},
		{"of", flagOp, true},
	}
	for _, c := range cases {
		if got := hasFlag(c.have, c.want); got != c.ok {
			t.Errorf("hasFlag(%q, %q) = %v, want %v", c.have, string(c.want), got, c.ok)
		}
	}
}

func TestNormalizeFlags(t *testing.T) {
	if got := normalizeFlags("", "n"); got != "n" {
		t.Errorf("empty should fall back to default: got %q", got)
	}
	if got := normalizeFlags("oxof", "x"); got != "of" { // drop unknown 'x', dedup 'o'
		t.Errorf("normalizeFlags = %q, want %q", got, "of")
	}
	if got := normalizeFlags("zzz", "v"); got != "v" { // all-unknown -> default
		t.Errorf("all-unknown should fall back: got %q", got)
	}
}

// A friend can use !help/!party; an op can !addquote but not master commands;
// only an owner can !addadmin.
func TestFlagGatedCommands(t *testing.T) {
	cfg := bossConfig()
	cfg.Admins = append(cfg.Admins,
		Identity{Network: "testnet", Account: "pal", Flags: "f"},
		Identity{Network: "testnet", Account: "deputy", Flags: "o"},
	)
	ctx := context.Background()

	m := New("arywen", cfg, "", &fakeQuoter{}, &fakeControl{}, nil, quietLog())
	if !m.has(dm("pal", "x"), flagFriend) || m.has(dm("pal", "x"), flagOp) {
		t.Fatal("friend should hold friend but not op")
	}
	if m.has(dm("deputy", "x"), flagMaster) || !m.has(dm("deputy", "x"), flagOp) {
		t.Fatal("op should hold op but not master")
	}

	// op is allowed !addquote (op-level)
	q := &fakeQuoter{}
	m2 := New("arywen", cfg, "", q, &fakeControl{}, nil, quietLog())
	m2.Handle(ctx, dm("deputy", "!addquote rickmorty schwifty"))
	if q.addedCount() != 1 {
		t.Fatal("op should be allowed to !addquote")
	}

	// op is denied owner-only !addadmin
	c := &fakeControl{}
	m3 := New("arywen", cfg, "", &fakeQuoter{}, c, nil, quietLog())
	m3.Handle(ctx, dm("deputy", "!addadmin testnet someone"))
	denied := false
	for _, s := range c.said {
		if strings.Contains(s, "not an admin for that") {
			denied = true
		}
	}
	if !denied {
		t.Fatalf("op should be denied !addadmin, got %#v", c.said)
	}
}
