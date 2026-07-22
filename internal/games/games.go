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
	out   engine.Sender
	store state.Store
	log   *slog.Logger

	trivia  *triviaState
	hangman *hangmanState

	mu  sync.Mutex
	rng *rand.Rand
}

// New returns a Manager with a time-seeded RNG.
func New(out engine.Sender, store state.Store, log *slog.Logger) *Manager {
	return NewWithRand(out, store, rand.New(rand.NewSource(rand.Int63())), log)
}

// NewWithRand lets tests inject a deterministic RNG.
func NewWithRand(out engine.Sender, store state.Store, rng *rand.Rand, log *slog.Logger) *Manager {
	return &Manager{out: out, store: store, rng: rng, log: log,
		trivia: newTriviaState(), hangman: newHangmanState()}
}

func karmaKey(network string) string { return "karma:" + strings.ToLower(network) }

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
		score, err := m.store.ZIncr(context.Background(), karmaKey(msg.Network), strings.ToLower(target), int64(delta))
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
	score, err := m.store.ZScore(context.Background(), karmaKey(network), strings.ToLower(nick))
	if err != nil {
		m.log.Warn("karma report failed", "err", err)
		return "can't reach the karma vault right now."
	}
	return fmt.Sprintf("%s has %d karma.", nick, score)
}

func (m *Manager) leaderboard(network string) string {
	top, err := m.store.ZTop(context.Background(), karmaKey(network), 5)
	if err != nil {
		m.log.Warn("karma leaderboard failed", "err", err)
		return "can't reach the karma vault right now."
	}
	if len(top) == 0 {
		return "no karma yet. get to it."
	}
	parts := make([]string, 0, len(top))
	for _, e := range top {
		parts = append(parts, fmt.Sprintf("%s (%d)", e.Member, e.Score))
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
