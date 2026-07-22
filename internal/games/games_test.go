package games

import (
	"context"
	"io"
	"log/slog"
	"math/rand"
	"strings"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/state"
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
	m, _ := newGamesWithStore(r)
	return m
}

// newGamesWithStore also hands back the store, for asserting on the ledger.
func newGamesWithStore(r *recorder) (*Manager, state.Store) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := state.NewMem()
	return NewWithRand(r, st, rand.New(rand.NewSource(1)), log), st
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

// TestKarmaFollowsALinkedAccountAcrossNetworks is the point of the whole change:
// praise on IRC and praise on Discord land on one ledger when the identities are
// linked to the same account.
func TestKarmaFollowsALinkedAccountAcrossNetworks(t *testing.T) {
	r := &recorder{}
	m, st := newGamesWithStore(r)
	// alice on either network resolves to the same account.
	m.SetResolver(func(network, _, nick string) string {
		if strings.EqualFold(nick, "alice") {
			return "acct:alice"
		}
		return strings.ToLower(network) + "|" + strings.ToLower(nick)
	})

	m.Handle(engine.Message{Network: "irc", Channel: "#c", Nick: "bob", Text: "alice++"})
	m.Handle(engine.Message{Network: "discord", Channel: "#c", Nick: "carol", Text: "alice++"})

	score, err := st.ZScore(context.Background(), karmaKey(), "acct:alice")
	if err != nil {
		t.Fatal(err)
	}
	if score != 2 {
		t.Fatalf("karma from both networks should total 2, got %d", score)
	}
	// and reading it from either side agrees
	if got := m.report("irc", "alice"); !strings.Contains(got, "2 karma") {
		t.Fatalf("irc report should see 2, got %q", got)
	}
	if got := m.report("discord", "alice"); !strings.Contains(got, "2 karma") {
		t.Fatalf("discord report should see 2, got %q", got)
	}
}

func TestKarmaMigrationFoldsLegacyLedgers(t *testing.T) {
	m, st := newGamesWithStore(&recorder{})
	ctx := context.Background()
	// Seed two pre-account per-network ledgers.
	_, _ = st.ZIncr(ctx, legacyKarmaKey("irc"), "alice", 3)
	_, _ = st.ZIncr(ctx, legacyKarmaKey("discord"), "alice", 4)
	_, _ = st.ZIncr(ctx, legacyKarmaKey("irc"), "bob", 1)

	m.SetResolver(func(network, _, nick string) string {
		if strings.EqualFold(nick, "alice") {
			return "acct:alice"
		}
		return strings.ToLower(network) + "|" + strings.ToLower(nick)
	})
	m.MigrateKarma(ctx, []string{"irc", "discord"})

	if got, _ := st.ZScore(ctx, karmaKey(), "acct:alice"); got != 7 {
		t.Fatalf("alice's two ledgers should merge to 7, got %d", got)
	}
	if got, _ := st.ZScore(ctx, karmaKey(), "irc|bob"); got != 1 {
		t.Fatalf("bob's unlinked karma should carry over, got %d", got)
	}
	// legacy keys are drained so a second run is a no-op
	if entries, _ := st.ZTop(ctx, legacyKarmaKey("irc"), 10); len(entries) != 0 {
		t.Fatalf("the legacy ledger should be cleared, got %v", entries)
	}
	m.MigrateKarma(ctx, []string{"irc", "discord"})
	if got, _ := st.ZScore(ctx, karmaKey(), "acct:alice"); got != 7 {
		t.Fatalf("re-running the migration must not double-count, got %d", got)
	}
}

func TestKarmaLeaderboardStripsTheNetworkPrefix(t *testing.T) {
	m, st := newGamesWithStore(&recorder{})
	ctx := context.Background()
	_, _ = st.ZIncr(ctx, karmaKey(), "irc|dave", 5)
	_, _ = st.ZIncr(ctx, karmaKey(), "acct:erin", 9)

	got := m.leaderboard("irc")
	if strings.Contains(got, "irc|dave") {
		t.Fatalf("the network prefix should be hidden, got %q", got)
	}
	for _, want := range []string{"dave", "erin"} {
		if !strings.Contains(got, want) {
			t.Fatalf("leaderboard missing %q, got %q", want, got)
		}
	}
}
