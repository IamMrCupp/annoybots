package admin

import (
	"context"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

func TestWildcardMatchFold(t *testing.T) {
	cases := []struct {
		pat, s string
		ok     bool
	}{
		{"*!*@home.example.com", "Aaron!znc@home.example.com", true},
		{"*!*@home.example.com", "aaron!znc@other.host", false},
		{"*!znc@home", "aaron!znc@home", true},
		{"*!zoe@home", "aaron!znc@home", false},
		{"aaron!*@*", "AARON!x@y", true}, // case-insensitive
		{"*!*@107.196.170.190", "b10burd3n!u@107.196.170.190", true},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"*", "anything", true},
	}
	for _, c := range cases {
		if got := wildcardMatchFold(c.pat, c.s); got != c.ok {
			t.Errorf("match(%q, %q) = %v, want %v", c.pat, c.s, got, c.ok)
		}
	}
}

func ircMsg(nick, ident, host, text string) engine.Message {
	return engine.Message{
		Network: "empradio", Channel: nick, Nick: nick, Ident: ident, Host: host,
		Text: text, Private: true, Self: "arwyen",
	}
}

func TestHostmaskAdminMatch(t *testing.T) {
	cfg := Config{Enabled: true, Admins: []Identity{
		{Network: "empradio", Mask: "*!*@home.example.com"}, // config mask -> owner
	}}
	m := New("arwyen", cfg, "", &fakeQuoter{}, &fakeControl{}, nil, quietLog())

	// matching host -> owner (config default), no account needed
	from := ircMsg("IamMrCupp", "znc", "home.example.com", "!admins")
	if !m.has(from, flagOwner) {
		t.Fatal("hostmask admin should be recognized as owner")
	}
	// same nick, different host -> not an admin (nick-squat resistant)
	imposter := ircMsg("IamMrCupp", "x", "coffeeshop.wifi", "!admins")
	if m.has(imposter, flagFriend) {
		t.Fatal("a matching nick from a different host must NOT be admin")
	}
}

func TestHostBoundSession(t *testing.T) {
	c := &fakeControl{}
	m := New("arwyen", bossConfig(), "hunter2", &fakeQuoter{}, c, nil, quietLog())

	// login from a specific host
	home := ircMsg("rando", "znc", "home.example.com", "!login hunter2")
	m.Handle(context.Background(), home)
	if !m.has(home, flagMaster) {
		t.Fatal("login should grant a session for the same nick+host")
	}
	// same nick, different host does not inherit the session
	elsewhere := ircMsg("rando", "znc", "elsewhere.net", "!admins")
	if m.has(elsewhere, flagMaster) {
		t.Fatal("session must be bound to host; a different host should not inherit it")
	}
}
