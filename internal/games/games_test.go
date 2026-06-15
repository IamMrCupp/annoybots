package games

import (
	"math/rand"
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

func msg(nick, text string) engine.Message {
	return engine.Message{Network: "net", Channel: "#chan", Nick: nick, Text: text}
}

func newGames(r *recorder) *Manager {
	return NewWithRand(r, rand.New(rand.NewSource(1)))
}

func TestKarmaIncrementAndQuery(t *testing.T) {
	r := &recorder{}
	g := newGames(r)

	if !g.Handle(msg("alice", "bob++")) {
		t.Fatal("karma op should be consumed")
	}
	if r.last() != "bob: 1" {
		t.Fatalf("expected 'bob: 1', got %q", r.last())
	}
	g.Handle(msg("alice", "bob++ bob++"))
	if r.last() != "bob: 3" {
		t.Fatalf("expected cumulative 'bob: 3', got %q", r.last())
	}
	g.Handle(msg("x", "bob--"))
	if r.last() != "bob: 2" {
		t.Fatalf("expected 'bob: 2' after --, got %q", r.last())
	}

	g.Handle(msg("anyone", "!karma bob"))
	if r.last() != "bob has 2 karma." {
		t.Fatalf("expected karma report, got %q", r.last())
	}
}

func TestNoSelfKarma(t *testing.T) {
	r := &recorder{}
	g := newGames(r)
	g.Handle(msg("alice", "alice++"))
	if !strings.Contains(r.last(), "no self-karma") {
		t.Fatalf("self-karma should be blocked, got %q", r.last())
	}
	g.Handle(msg("anyone", "!karma alice"))
	if r.last() != "alice has 0 karma." {
		t.Fatalf("self-karma should not have scored, got %q", r.last())
	}
}

func TestKarmaIgnoresNonNicks(t *testing.T) {
	r := &recorder{}
	g := newGames(r)
	// "++" alone, numbers, and C++ mentions shouldn't all score blindly.
	if g.Handle(msg("alice", "i love 42++ and ++")) {
		// 42++ has no letter -> not a nick; "++" too short. Should not act.
		t.Fatalf("non-nick karma should be ignored, got %q", r.lines)
	}
}

func TestLeaderboard(t *testing.T) {
	r := &recorder{}
	g := newGames(r)
	g.Handle(msg("x", "alice++"))
	g.Handle(msg("x", "bob++"))
	g.Handle(msg("x", "bob++"))
	g.Handle(msg("anyone", "!top"))
	if !strings.Contains(r.last(), "bob (2)") || !strings.Contains(r.last(), "alice (1)") {
		t.Fatalf("leaderboard wrong: %q", r.last())
	}
	if strings.Index(r.last(), "bob") > strings.Index(r.last(), "alice") {
		t.Fatalf("bob should rank before alice: %q", r.last())
	}
}

func TestRollAndEightBall(t *testing.T) {
	r := &recorder{}
	g := newGames(r)

	g.Handle(msg("alice", "!roll 2d6"))
	if !strings.Contains(r.last(), "2d6:") {
		t.Fatalf("roll output wrong: %q", r.last())
	}
	g.Handle(msg("alice", "!8ball will this work"))
	if r.last() == "" || !strings.HasPrefix(r.last(), "alice: ") {
		t.Fatalf("8ball output wrong: %q", r.last())
	}
}

func TestGamesIgnoresPrivateAndNormal(t *testing.T) {
	r := &recorder{}
	g := newGames(r)
	p := msg("alice", "!roll")
	p.Private = true
	if g.Handle(p) {
		t.Fatal("private messages should not be handled")
	}
	if g.Handle(msg("alice", "just talking normally")) {
		t.Fatal("normal chatter should not be consumed")
	}
}
