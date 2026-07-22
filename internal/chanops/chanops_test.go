package chanops

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// fakeOpper records Op calls and reports a preset "did we hold ops" result.
type fakeOpper struct {
	held  bool // what the mode calls return (i.e. this bot holds ops)
	calls []string
	kicks []string
}

func (f *fakeOpper) Op(network, channel, nick string) bool {
	return f.Mode(network, channel, "+o", nick)
}

func (f *fakeOpper) Mode(network, channel, modes, nick string) bool {
	f.calls = append(f.calls, network+"|"+channel+"|"+modes+"|"+nick)
	return f.held
}

func (f *fakeOpper) Kick(network, channel, nick, reason string) bool {
	f.kicks = append(f.kicks, network+"|"+channel+"|"+nick+"|"+reason)
	return f.held
}

// recSender records Say/Notice so we can assert the DM hint.
type recSender struct{ lines []string }

func (r *recSender) Say(_, target, text string)    { r.lines = append(r.lines, "say:"+target+":"+text) }
func (r *recSender) Action(_, target, text string) { r.lines = append(r.lines, "act:"+target+":"+text) }
func (r *recSender) Notice(_, target, text string) {
	r.lines = append(r.lines, "notice:"+target+":"+text)
}

func newMgr(held bool) (*Manager, *fakeOpper, *recSender) {
	op := &fakeOpper{held: held}
	out := &recSender{}
	m := New(op, out, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return m, op, out
}

func chanMsg(nick, text string) engine.Message {
	return engine.Message{Network: "net", Channel: "#chan", Nick: nick, Text: text, Account: nick}
}

func TestOpGrantsForAuthorizedAdminWhenBotHoldsOps(t *testing.T) {
	m, op, _ := newMgr(true)
	m.SetAuthz(func(msg engine.Message) bool { return msg.Nick == "boss" })

	if !m.Handle(chanMsg("boss", "!op")) {
		t.Fatal("!op should be consumed")
	}
	if len(op.calls) != 1 || op.calls[0] != "net|#chan|+o|boss" {
		t.Fatalf("expected one +o for boss, got %v", op.calls)
	}
}

func TestOpIgnoresUnauthorizedSilently(t *testing.T) {
	m, op, out := newMgr(true)
	m.SetAuthz(func(msg engine.Message) bool { return msg.Nick == "boss" })

	if !m.Handle(chanMsg("randomuser", "!op")) {
		t.Fatal("!op is still consumed even when unauthorized (must not fall through to the engine)")
	}
	if len(op.calls) != 0 {
		t.Fatalf("unauthorized user must not be opped, got %v", op.calls)
	}
	if len(out.lines) != 0 {
		t.Fatalf("unauthorized attempt must be silent (no notice-storm), got %v", out.lines)
	}
}

func TestOpConsumedButSilentWhenBotLacksOps(t *testing.T) {
	// A bot that doesn't hold ops still consumes the command (so it doesn't reach
	// the engine) but sends nothing — a sibling bot that IS opped will handle it.
	m, op, out := newMgr(false)
	m.SetAuthz(func(engine.Message) bool { return true })

	if !m.Handle(chanMsg("boss", "!op")) {
		t.Fatal("!op should be consumed")
	}
	if len(op.calls) != 1 {
		t.Fatalf("the bot should attempt Op (which reports not-opped), got %v", op.calls)
	}
	if len(out.lines) != 0 {
		t.Fatalf("a non-opped bot must stay silent, got %v", out.lines)
	}
}

func TestOpWithoutAuthzGrantsNobody(t *testing.T) {
	m, op, _ := newMgr(true) // SetAuthz never called
	if !m.Handle(chanMsg("boss", "!op")) {
		t.Fatal("!op should be consumed")
	}
	if len(op.calls) != 0 {
		t.Fatalf("with no authz predicate, nobody is authorized, got %v", op.calls)
	}
}

func TestOpInDMHintsToUseAChannel(t *testing.T) {
	m, op, out := newMgr(true)
	m.SetAuthz(func(engine.Message) bool { return true })

	dm := engine.Message{Network: "net", Channel: "boss", Nick: "boss", Text: "!op", Private: true, Account: "boss"}
	if !m.Handle(dm) {
		t.Fatal("!op in a DM is still consumed")
	}
	if len(op.calls) != 0 {
		t.Fatalf("no op mode in a DM, got %v", op.calls)
	}
	if len(out.lines) != 1 || out.lines[0] != "notice:boss:use !op in a channel where a bot holds operator status." {
		t.Fatalf("expected a DM hint, got %v", out.lines)
	}
}

func TestNonOpMessagesFallThrough(t *testing.T) {
	m, _, _ := newMgr(true)
	m.SetAuthz(func(engine.Message) bool { return true })
	for _, text := range []string{"", "hello", "!open the door", "op me please"} {
		if m.Handle(chanMsg("boss", text)) {
			t.Fatalf("%q should not be consumed as an !op command", text)
		}
	}
	// but "!op" with trailing args is still an !op command (op self)
	if !m.Handle(chanMsg("boss", "!op now")) {
		t.Fatal("!op with args should still be consumed")
	}
}

func TestChanOpsVerbFamily(t *testing.T) {
	cases := []struct{ text, wantMode, wantTarget string }{
		{"!op", "+o", "boss"},        // defaults to the sender
		{"!op alice", "+o", "alice"}, // or an explicit nick
		{"!deop alice", "-o", "alice"},
		{"!voice alice", "+v", "alice"},
		{"!devoice alice", "-v", "alice"},
	}
	for _, c := range cases {
		m, op, _ := newMgr(true)
		m.SetAuthz(func(engine.Message) bool { return true })
		if !m.Handle(chanMsg("boss", c.text)) {
			t.Fatalf("%q should be consumed", c.text)
		}
		want := "net|#chan|" + c.wantMode + "|" + c.wantTarget
		if len(op.calls) != 1 || op.calls[0] != want {
			t.Fatalf("%q → want %q, got %v", c.text, want, op.calls)
		}
	}
}

func TestChanOpsKick(t *testing.T) {
	m, op, _ := newMgr(true)
	m.SetAuthz(func(engine.Message) bool { return true })

	if !m.Handle(chanMsg("boss", "!kick pest being annoying")) {
		t.Fatal("!kick should be consumed")
	}
	if len(op.kicks) != 1 || op.kicks[0] != "net|#chan|pest|being annoying" {
		t.Fatalf("expected a kick with a reason, got %v", op.kicks)
	}
	// no reason → a default one naming the requester
	m2, op2, _ := newMgr(true)
	m2.SetAuthz(func(engine.Message) bool { return true })
	m2.Handle(chanMsg("boss", "!kick pest"))
	if len(op2.kicks) != 1 || !strings.Contains(op2.kicks[0], "requested by boss") {
		t.Fatalf("expected a default reason, got %v", op2.kicks)
	}
	// missing target → a usage hint, no kick
	m3, op3, out3 := newMgr(true)
	m3.SetAuthz(func(engine.Message) bool { return true })
	m3.Handle(chanMsg("boss", "!kick"))
	if len(op3.kicks) != 0 {
		t.Fatalf("no target means no kick, got %v", op3.kicks)
	}
	if len(out3.lines) != 1 || !strings.Contains(out3.lines[0], "usage") {
		t.Fatalf("expected a usage hint, got %v", out3.lines)
	}
}

func TestChanOpsRefusesToTurnOnItsOwnBots(t *testing.T) {
	// Deopping or kicking the bots holding the channel is how a channel is lost.
	for _, text := range []string{"!deop mybot", "!kick mybot", "!devoice sibling"} {
		m, op, out := newMgr(true)
		m.SetAuthz(func(engine.Message) bool { return true })
		m.SetSiblings(func(nick string) bool { return nick == "sibling" })
		msg := chanMsg("boss", text)
		msg.Self = "mybot"
		if !m.Handle(msg) {
			t.Fatalf("%q should be consumed", text)
		}
		if len(op.calls) != 0 || len(op.kicks) != 0 {
			t.Fatalf("%q must be refused, got calls=%v kicks=%v", text, op.calls, op.kicks)
		}
		if len(out.lines) != 1 || !strings.Contains(out.lines[0], "refusing") {
			t.Fatalf("%q should explain the refusal, got %v", text, out.lines)
		}
	}
	// but +o/+v on a bot is fine — that's how it gets its ops back
	m, op, _ := newMgr(true)
	m.SetAuthz(func(engine.Message) bool { return true })
	m.SetSiblings(func(nick string) bool { return nick == "sibling" })
	msg := chanMsg("boss", "!op sibling")
	msg.Self = "mybot"
	m.Handle(msg)
	if len(op.calls) != 1 {
		t.Fatalf("granting ops to a sibling is allowed, got %v", op.calls)
	}
}

func TestChanOpsUnauthorizedSilentAcrossFamily(t *testing.T) {
	for _, text := range []string{"!op", "!deop bob", "!voice bob", "!kick bob"} {
		m, op, out := newMgr(true)
		m.SetAuthz(func(msg engine.Message) bool { return msg.Nick == "boss" })
		if !m.Handle(chanMsg("rando", text)) {
			t.Fatalf("%q is still consumed", text)
		}
		if len(op.calls) != 0 || len(op.kicks) != 0 || len(out.lines) != 0 {
			t.Fatalf("%q from a non-admin must be silent, got %v %v %v", text, op.calls, op.kicks, out.lines)
		}
	}
}
