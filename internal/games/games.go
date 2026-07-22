// Package games adds lightweight, public channel toys — a magic 8-ball, dice,
// and a karma/leaderboard system (`name++` / `name--`, `!karma`, `!top`). It is
// a vertical slice of the eggdrop "fun scripts" layer; karma rides the F3 state
// store, so scores are persistent and shared across every bot on the botnet.
package games

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"sync"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/state"
)

var eightBall = []string{
	"it is certain.", "without a doubt.", "yes, definitely.", "you may rely on it.",
	"as i see it, yes.", "most likely.", "outlook good.", "signs point to yes.",
	"reply hazy, try again.", "ask again later.", "better not tell you now.",
	"cannot predict now.", "don't count on it.", "my reply is no.",
	"my sources say no.", "outlook not so good.", "very doubtful.", "lol no.",
}

// Manager handles the toy commands; karma is persisted via the F3 state store.
type Manager struct {
	out     engine.Sender
	store   state.Store
	log     *slog.Logger
	resolve Resolver

	trivia  *triviaState
	hangman *hangmanState

	mu  sync.Mutex
	rng *rand.Rand
}

// Resolver maps a (network, account, nick) to a canonical player key — the
// account system, so linked identities are one person everywhere. Karma uses it
// so praise on IRC and praise on Discord land on the same ledger.
type Resolver func(network, account, nick string) string

// New returns a Manager with a time-seeded RNG.
func New(out engine.Sender, store state.Store, log *slog.Logger) *Manager {
	return NewWithRand(out, store, rand.New(rand.NewSource(rand.Int63())), log)
}

// NewWithRand lets tests inject a deterministic RNG.
func NewWithRand(out engine.Sender, store state.Store, rng *rand.Rand, log *slog.Logger) *Manager {
	return &Manager{out: out, store: store, rng: rng, log: log,
		trivia:  newTriviaState(),
		hangman: newHangmanState(),
		resolve: func(network, _, nick string) string {
			return strings.ToLower(network) + "|" + strings.ToLower(nick)
		}}
}

// SetResolver wires the account system in, so karma follows a linked person
// across networks. Without it, karma stays keyed per network identity.
func (m *Manager) SetResolver(r Resolver) {
	if r != nil {
		m.resolve = r
	}
}

// karmaKey is a single realm-wide ledger; members are resolved player keys, so a
// linked person carries one karma score across every network.
func karmaKey() string { return "karma:all" }

// legacyKarmaKey is the pre-account, per-network ledger, kept only for migration.
func legacyKarmaKey(network string) string { return "karma:" + strings.ToLower(network) }

// displayKarma strips a "network|" prefix from a resolved key for display.
func displayKarma(key string) string {
	if i := strings.IndexByte(key, '|'); i >= 0 {
		return key[i+1:]
	}
	return key
}

// MigrateKarma folds the old per-network ledgers into the single resolved one,
// so switching to account-keyed karma never orphans what people already earned.
// Safe to run on every start: once a legacy key is drained it's removed.
func (m *Manager) MigrateKarma(ctx context.Context, networks []string) {
	for _, n := range networks {
		old := legacyKarmaKey(n)
		entries, err := m.store.ZTop(ctx, old, 100000)
		if err != nil || len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			key := m.resolve(n, "", e.Member)
			if _, err := m.store.ZIncr(ctx, karmaKey(), key, e.Score); err != nil {
				m.log.Warn("karma migration failed", "network", n, "member", e.Member, "err", err)
				return // leave the legacy key intact so it can be retried
			}
		}
		_ = m.store.Del(ctx, old)
		m.log.Info("karma migrated to the shared ledger", "network", n, "entries", len(entries))
	}
}

// Handle processes a channel message. Returns true if it consumed it (a command
// or a karma op), false to let normal chatter flow on to the engine.
func (m *Manager) Handle(msg engine.Message) bool {
	if msg.Private || msg.Text == "" {
		return false
	}
	fields := strings.Fields(msg.Text)
	switch strings.ToLower(fields[0]) {
	case "!8ball":
		m.out.Say(msg.Network, msg.Channel, msg.Nick+": "+m.pick(eightBall))
		return true
	case "!roll":
		m.out.Say(msg.Network, msg.Channel, msg.Nick+" rolls "+m.roll(arg(fields, 1)))
		return true
	case "!karma":
		if len(fields) < 2 {
			m.out.Say(msg.Network, msg.Channel, "usage: !karma <nick>")
			return true
		}
		m.out.Say(msg.Network, msg.Channel, m.report(msg.Network, fields[1]))
		return true
	case "!top":
		m.out.Say(msg.Network, msg.Channel, m.leaderboard(msg.Network))
		return true
	case "!slots":
		m.out.Say(msg.Network, msg.Channel, m.spin(msg.Nick))
		return true
	case "!trivia":
		m.out.Say(msg.Network, msg.Channel, m.ask(msg.Network, msg.Channel, msg.Nick))
		return true
	case "!hangman":
		m.out.Say(msg.Network, msg.Channel, m.startHangman(msg.Network, msg.Channel))
		return true
	case "!guess":
		m.out.Say(msg.Network, msg.Channel, m.guess(msg.Network, msg.Channel, msg.Nick, arg(fields, 1)))
		return true
	}
	// An open trivia question turns ordinary chatter into an answer attempt.
	if line, won := m.answer(msg.Network, msg.Channel, msg.Nick, msg.Text); won {
		m.out.Say(msg.Network, msg.Channel, line)
		return true
	}
	return m.maybeKarma(msg, fields)
}

// maybeKarma scans tokens for "name++" / "name--" and applies them.
func (m *Manager) maybeKarma(msg engine.Message, fields []string) bool {
	acted := false
	for _, tok := range fields {
		var delta int
		switch {
		case strings.HasSuffix(tok, "++") && len(tok) > 2:
			delta = 1
		case strings.HasSuffix(tok, "--") && len(tok) > 2:
			delta = -1
		default:
			continue
		}
		target := tok[:len(tok)-2]
		if !validNick(target) {
			continue
		}
		if strings.EqualFold(target, msg.Nick) {
			m.out.Say(msg.Network, msg.Channel, "nice try, "+msg.Nick+". no self-karma.")
			acted = true
			continue
		}
		key := m.resolve(msg.Network, "", target)
		score, err := m.store.ZIncr(context.Background(), karmaKey(), key, int64(delta))
		if err != nil {
			m.log.Warn("karma store failed", "err", err)
			m.out.Say(msg.Network, msg.Channel, "karma's having a moment, try later.")
			return true
		}
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("%s: %d", target, score))
		acted = true
	}
	return acted
}

func (m *Manager) report(network, nick string) string {
	score, err := m.store.ZScore(context.Background(), karmaKey(), m.resolve(network, "", nick))
	if err != nil {
		m.log.Warn("karma report failed", "err", err)
		return "can't reach the karma vault right now."
	}
	return fmt.Sprintf("%s has %d karma.", nick, score)
}

func (m *Manager) leaderboard(_ string) string {
	top, err := m.store.ZTop(context.Background(), karmaKey(), 5)
	if err != nil {
		m.log.Warn("karma leaderboard failed", "err", err)
		return "can't reach the karma vault right now."
	}
	if len(top) == 0 {
		return "no karma yet. get to it."
	}
	parts := make([]string, 0, len(top))
	for _, e := range top {
		parts = append(parts, fmt.Sprintf("%s (%d)", displayKarma(e.Member), e.Score))
	}
	return "karma leaders: " + strings.Join(parts, ", ")
}

func (m *Manager) roll(spec string) string {
	n, sides := 1, 20
	if spec != "" {
		if a, b, ok := strings.Cut(strings.ToLower(spec), "d"); ok {
			if a != "" {
				if v, err := strconv.Atoi(a); err == nil {
					n = v
				}
			}
			if v, err := strconv.Atoi(b); err == nil {
				sides = v
			}
		}
	}
	if n < 1 || n > 100 {
		n = 1
	}
	if sides < 2 || sides > 1000 {
		sides = 20
	}
	m.mu.Lock()
	total := 0
	for i := 0; i < n; i++ {
		total += m.rng.Intn(sides) + 1
	}
	m.mu.Unlock()
	return fmt.Sprintf("%dd%d: %d", n, sides, total)
}

func (m *Manager) pick(list []string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return list[m.rng.Intn(len(list))]
}

// validNick guards against turning punctuation/operators into karma targets.
func validNick(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '[' || r == ']' || r == '\\' || r == '`' || r == '|' || r == '{' || r == '}':
		default:
			return false
		}
	}
	// must contain at least one letter (so "++" or "42" aren't nicks)
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func arg(fields []string, i int) string {
	if i < len(fields) {
		return fields[i]
	}
	return ""
}
