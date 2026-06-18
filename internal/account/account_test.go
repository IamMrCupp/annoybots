package account

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/state"
)

type recorder struct{ lines []string }

func (r *recorder) Say(_, _, text string)    { r.lines = append(r.lines, text) }
func (r *recorder) Action(_, _, text string) { r.lines = append(r.lines, text) }
func (r *recorder) has(sub string) bool {
	for _, l := range r.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
func (r *recorder) last() string {
	if len(r.lines) == 0 {
		return ""
	}
	return r.lines[len(r.lines)-1]
}

func dm(network, account, nick, text string) engine.Message {
	return engine.Message{Network: network, Channel: nick, Nick: nick, Account: account, Text: text, Private: true}
}

func newMgr() (*Manager, *recorder) {
	r := &recorder{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(state.NewMem(), r, log), r
}

func TestRegisterAndResolve(t *testing.T) {
	m, r := newMgr()
	// unlinked: resolves to the raw network identity
	if got := m.Resolve("discord", "uid1", "bob"); got != "discord|uid1" {
		t.Fatalf("unlinked resolve = %q; want discord|uid1", got)
	}
	if !m.Handle(dm("discord", "uid1", "bob", "!register bob hunter2")) {
		t.Fatal("!register should be consumed")
	}
	if !r.has("registered 'bob'") {
		t.Fatalf("expected registration, got %v", r.lines)
	}
	if got := m.Resolve("discord", "uid1", "bob"); got != "bob" {
		t.Fatalf("linked resolve = %q; want bob", got)
	}
}

func TestLinkAcrossNetworks(t *testing.T) {
	m, _ := newMgr()
	m.Handle(dm("discord", "uid1", "bob", "!register bob hunter2"))
	// link the IRC identity (no services account → keyed by nick)
	m.Handle(dm("empradio", "", "bob", "!link bob hunter2"))
	if got := m.Resolve("empradio", "", "bob"); got != "bob" {
		t.Fatalf("irc resolve = %q; want bob (linked)", got)
	}
	if got := m.Resolve("discord", "uid1", "bob"); got != "bob" {
		t.Fatalf("discord resolve = %q; want bob", got)
	}
}

func TestLinkWrongPassword(t *testing.T) {
	m, r := newMgr()
	m.Handle(dm("discord", "uid1", "bob", "!register bob hunter2"))
	m.Handle(dm("empradio", "", "bob", "!link bob nope"))
	if !r.has("wrong password") {
		t.Fatalf("expected rejection, got %v", r.lines)
	}
	if got := m.Resolve("empradio", "", "bob"); got != "empradio|bob" {
		t.Fatalf("wrong-pw link should not link: %q", got)
	}
}

func TestUnlink(t *testing.T) {
	m, _ := newMgr()
	m.Handle(dm("discord", "uid1", "bob", "!register bob hunter2"))
	m.Handle(dm("discord", "uid1", "bob", "!unlink"))
	if got := m.Resolve("discord", "uid1", "bob"); got != "discord|uid1" {
		t.Fatalf("after unlink, resolve = %q; want raw identity", got)
	}
}

func TestWhoamiAndNameTaken(t *testing.T) {
	m, r := newMgr()
	m.Handle(dm("discord", "uid1", "bob", "!register bob hunter2"))
	m.Handle(dm("empradio", "", "bob", "!link bob hunter2"))
	m.Handle(dm("discord", "uid1", "bob", "!whoami"))
	if !r.has("discord|uid1") || !r.has("empradio|bob") {
		t.Fatalf("whoami should list both identities, got %q", r.last())
	}
	// a different identity can't take the same name
	m.Handle(dm("twitch", "", "carol", "!register bob secret"))
	if !r.has("taken") {
		t.Fatalf("duplicate name should be rejected, got %q", r.last())
	}
}
