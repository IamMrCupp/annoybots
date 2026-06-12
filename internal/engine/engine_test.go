package engine

import (
	"math/rand"
	"sync"
	"testing"
	"time"
)

// recorder is a test Sender that captures emitted lines.
type recorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *recorder) Say(network, target, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, "SAY "+network+" "+target+" "+text)
}

func (r *recorder) Action(network, target, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, "ACT "+network+" "+target+" "+text)
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

func newTestEngine(t *testing.T, p Personality, now *time.Time) *Engine {
	t.Helper()
	clock := func() time.Time { return *now }
	e, err := New(p, Options{
		Rand: rand.New(rand.NewSource(1)),
		Now:  clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func msg(text string) Message {
	return Message{Network: "net", Channel: "#chan", Nick: "victim", Text: text, Self: "arywen"}
}

func TestTriggerFires(t *testing.T) {
	now := time.Unix(0, 0)
	p := Personality{
		Name: "arywen",
		Triggers: []Trigger{{
			Name:      "greet",
			Pattern:   `(?i)\bhello\b`,
			Responses: []string{"oh, it's {nick} again"},
		}},
	}
	e := newTestEngine(t, p, &now)
	r := &recorder{}
	e.Handle(msg("well hello there"), r)

	got := r.all()
	if len(got) != 1 || got[0] != "SAY net #chan oh, it's victim again" {
		t.Fatalf("unexpected output: %#v", got)
	}
}

func TestTriggerCooldownSuppresses(t *testing.T) {
	now := time.Unix(0, 0)
	p := Personality{
		Triggers: []Trigger{{
			Name:      "greet",
			Pattern:   `hello`,
			Cooldown:  Duration(time.Minute),
			Responses: []string{"hi"},
		}},
	}
	e := newTestEngine(t, p, &now)
	r := &recorder{}

	e.Handle(msg("hello"), r)
	e.Handle(msg("hello"), r) // within cooldown, should be suppressed
	if n := len(r.all()); n != 1 {
		t.Fatalf("expected 1 response during cooldown, got %d", n)
	}

	now = now.Add(2 * time.Minute)
	e.Handle(msg("hello"), r)
	if n := len(r.all()); n != 2 {
		t.Fatalf("expected 2 responses after cooldown, got %d", n)
	}
}

func TestIgnoresSelf(t *testing.T) {
	now := time.Unix(0, 0)
	p := Personality{Triggers: []Trigger{{Name: "x", Pattern: "hello", Responses: []string{"hi"}}}}
	e := newTestEngine(t, p, &now)
	r := &recorder{}
	m := msg("hello")
	m.Nick = "arywen" // same as Self
	e.Handle(m, r)
	if n := len(r.all()); n != 0 {
		t.Fatalf("should ignore own messages, got %d responses", n)
	}
}

func TestActionTriggerUsesAction(t *testing.T) {
	now := time.Unix(0, 0)
	p := Personality{Triggers: []Trigger{{
		Name: "poke", Pattern: "poke", Action: true, Responses: []string{"recoils from {nick}"},
	}}}
	e := newTestEngine(t, p, &now)
	r := &recorder{}
	e.Handle(msg("poke"), r)
	got := r.all()
	if len(got) != 1 || got[0] != "ACT net #chan recoils from victim" {
		t.Fatalf("expected action emit, got %#v", got)
	}
}

func TestCommandAnnoyUsesMarkov(t *testing.T) {
	now := time.Unix(0, 0)
	p := Personality{
		Commands: true,
		Markov:   MarkovConfig{Enabled: true, Learn: true, Order: 2, MaxWords: 20},
	}
	e := newTestEngine(t, p, &now)
	// Teach the brain a deterministic single-path line.
	e.Brain().Learn("you are all so very tiresome")
	r := &recorder{}
	e.Handle(msg("!annoy"), r)
	got := r.all()
	if len(got) != 1 || got[0] != "SAY net #chan you are all so very tiresome" {
		t.Fatalf("expected markov line, got %#v", got)
	}
}

func TestInterjectionRespectsCooldown(t *testing.T) {
	now := time.Unix(0, 0)
	p := Personality{
		Interjections: Interjections{
			Enabled:  true,
			Chance:   1, // always roll yes
			Cooldown: Duration(time.Minute),
			Lines:    []string{"nobody asked, {nick}"},
		},
	}
	e := newTestEngine(t, p, &now)
	r := &recorder{}
	e.Handle(msg("chatter one"), r)
	e.Handle(msg("chatter two"), r) // cooldown should suppress
	if n := len(r.all()); n != 1 {
		t.Fatalf("expected 1 interjection within cooldown, got %d", n)
	}
}

func TestInvalidPatternRejected(t *testing.T) {
	_, err := New(Personality{Triggers: []Trigger{{Name: "bad", Pattern: "("}}}, Options{})
	if err == nil {
		t.Fatal("expected error for invalid regexp")
	}
}
