package plugin

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

type recorder struct{ lines []string }

func (r *recorder) Say(_, _, text string)    { r.lines = append(r.lines, text) }
func (r *recorder) Action(_, _, text string) { r.lines = append(r.lines, text) }
func (r *recorder) last() string {
	if len(r.lines) == 0 {
		return ""
	}
	return r.lines[len(r.lines)-1]
}
func (r *recorder) has(s string) bool {
	for _, l := range r.lines {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func chanMsg(nick, text string) engine.Message {
	return engine.Message{Network: "net", Channel: "#c", Nick: nick, Text: text}
}

// loadScript builds a Manager with a single script body in a temp dir.
func loadScript(t *testing.T, body string) (*Manager, *recorder) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.lua"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	m := New(r, quiet())
	if n := m.Load(dir); n != 1 {
		t.Fatalf("expected 1 script loaded, got %d", n)
	}
	return m, r
}

func TestPubBindReplies(t *testing.T) {
	m, r := loadScript(t, `bind("pub","!hello",function(ev) reply("hi "..ev.nick) end)`)
	if !m.Handle(chanMsg("alice", "!hello")) {
		t.Fatal("a matching command should be claimed")
	}
	if r.last() != "hi alice" {
		t.Fatalf("reply = %q; want %q", r.last(), "hi alice")
	}
}

func TestNonCommandIgnored(t *testing.T) {
	m, _ := loadScript(t, `bind("pub","!hello",function(ev) reply("hi") end)`)
	if m.Handle(chanMsg("alice", "just chatting")) {
		t.Fatal("a non-command message must not be claimed")
	}
}

func TestArgsTable(t *testing.T) {
	m, r := loadScript(t, `bind("pub","!echo",function(ev) reply(table.concat(ev.args," ")) end)`)
	m.Handle(chanMsg("bob", "!echo one two three"))
	if r.last() != "one two three" {
		t.Fatalf("args join = %q", r.last())
	}
}

func TestMsgBindOnlyInDM(t *testing.T) {
	m, _ := loadScript(t, `bind("msg","!secret",function(ev) reply("psst") end)`)
	if m.Handle(chanMsg("a", "!secret")) {
		t.Fatal("a msg bind must not fire in a channel")
	}
	pm := chanMsg("a", "!secret")
	pm.Private = true
	if !m.Handle(pm) {
		t.Fatal("a msg bind should fire in a DM")
	}
}

func TestCallbackErrorIsContained(t *testing.T) {
	m, _ := loadScript(t, `bind("pub","!boom",function(ev) error("kaboom") end)`)
	// Matched (claimed) and the error is caught by PCall — no panic.
	if !m.Handle(chanMsg("a", "!boom")) {
		t.Fatal("an erroring bind still claims the message")
	}
}

func TestSandboxHasNoOS(t *testing.T) {
	m, r := loadScript(t, `bind("pub","!probe",function(ev) reply(os == nil and "no os" or "has os") end)`)
	m.Handle(chanMsg("a", "!probe"))
	if r.last() != "no os" {
		t.Fatalf("the os library must not be available, got %q", r.last())
	}
}

func TestExampleScriptLoads(t *testing.T) {
	r := &recorder{}
	m := New(r, quiet())
	if n := m.Load("../../data/plugins"); n < 1 {
		t.Fatalf("expected the bundled example to load, got %d", n)
	}
	m.Handle(chanMsg("alice", "!hello"))
	if !r.has("hi alice") {
		t.Fatalf("example !hello should reply, got %v", r.lines)
	}
}
