package chanops

import (
	"io"
	"log/slog"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// fakeOpper records Op calls and reports a preset "did we hold ops" result.
type fakeOpper struct {
	held  bool // what Op returns (i.e. this bot holds ops)
	calls []string
}

func (f *fakeOpper) Op(network, channel, nick string) bool {
	f.calls = append(f.calls, network+"|"+channel+"|"+nick)
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
	if len(op.calls) != 1 || op.calls[0] != "net|#chan|boss" {
		t.Fatalf("expected one Op(net,#chan,boss), got %v", op.calls)
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
