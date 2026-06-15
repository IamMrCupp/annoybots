package tell

import (
	"strings"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/event"
)

type recorder struct{ lines []string }

func (r *recorder) Say(network, target, text string) {
	r.lines = append(r.lines, network+" "+target+" "+text)
}
func (r *recorder) Action(_, _, text string) { r.lines = append(r.lines, "ACT "+text) }

func (r *recorder) has(sub string) bool {
	for _, l := range r.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func chanMsg(nick, text string) engine.Message {
	return engine.Message{Network: "net", Channel: "#chan", Nick: nick, Text: text}
}

func TestTellStoreAndDeliverOnMessage(t *testing.T) {
	r := &recorder{}
	m := New(r)

	if !m.Handle(chanMsg("alice", "!message bob hey check the logs")) {
		t.Fatal("!message should be consumed")
	}
	if !r.has("i'll tell bob") {
		t.Fatalf("expected confirmation, got %v", r.lines)
	}

	// bob speaks → note delivered, then cleared.
	if m.Handle(chanMsg("bob", "hello")) {
		t.Fatal("a normal message must not be consumed")
	}
	if !r.has("bob: alice wanted you to know — hey check the logs") {
		t.Fatalf("expected delivery to bob, got %v", r.lines)
	}

	before := len(r.lines)
	m.Handle(chanMsg("bob", "still here"))
	if len(r.lines) != before {
		t.Fatal("note should only be delivered once")
	}
}

func TestTellDeliverOnJoin(t *testing.T) {
	r := &recorder{}
	m := New(r)
	m.Handle(chanMsg("alice", "!message carol welcome back"))

	m.OnJoin(event.Event{Kind: event.Join, Network: "net", Channel: "#chan", Nick: "carol"})
	if !r.has("carol: alice wanted you to know — welcome back") {
		t.Fatalf("expected delivery on join, got %v", r.lines)
	}
}

func TestTellUsage(t *testing.T) {
	r := &recorder{}
	m := New(r)
	if !m.Handle(chanMsg("alice", "!message bob")) {
		t.Fatal("malformed !message is still consumed")
	}
	if !r.has("usage: !message") {
		t.Fatalf("expected usage hint, got %v", r.lines)
	}
}

func TestTellIgnoresPrivateAndNormal(t *testing.T) {
	r := &recorder{}
	m := New(r)
	priv := chanMsg("alice", "!message bob hi")
	priv.Private = true
	if m.Handle(priv) {
		t.Fatal("!message in a DM should not be handled here")
	}
	if m.Handle(chanMsg("alice", "just talking")) {
		t.Fatal("normal chatter should not be consumed")
	}
}

func TestTellCapsInbox(t *testing.T) {
	r := &recorder{}
	m := New(r)
	for i := 0; i < maxPerTarget+3; i++ {
		m.Handle(chanMsg("alice", "!message bob spam"))
	}
	if !r.has("inbox is full") {
		t.Fatalf("expected inbox-full message after cap, got %d lines", len(r.lines))
	}
	m.OnJoin(event.Event{Kind: event.Join, Network: "net", Channel: "#chan", Nick: "bob"})
	delivered := 0
	for _, l := range r.lines {
		if strings.Contains(l, "wanted you to know") {
			delivered++
		}
	}
	if delivered != maxPerTarget {
		t.Fatalf("expected exactly %d delivered notes, got %d", maxPerTarget, delivered)
	}
}
