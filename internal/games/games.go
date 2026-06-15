// Package games adds lightweight, public channel toys — a magic 8-ball, dice,
// and a karma/leaderboard system (`name++` / `name--`, `!karma`, `!top`). It is
// a vertical slice of the eggdrop "fun scripts" layer and a first taste of the
// per-user state the F3 store will later persist (for now, in-memory).
package games

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

var eightBall = []string{
	"it is certain.", "without a doubt.", "yes, definitely.", "you may rely on it.",
	"as i see it, yes.", "most likely.", "outlook good.", "signs point to yes.",
	"reply hazy, try again.", "ask again later.", "better not tell you now.",
	"cannot predict now.", "don't count on it.", "my reply is no.",
	"my sources say no.", "outlook not so good.", "very doubtful.", "lol no.",
}

// Manager handles the toy commands and tracks karma per (network, nick).
type Manager struct {
	out engine.Sender

	mu  sync.Mutex
	rng *rand.Rand
	// karma[network][nick] = score
	karma map[string]map[string]int
}

// New returns a Manager with a time-seeded RNG.
func New(out engine.Sender) *Manager {
	return NewWithRand(out, rand.New(rand.NewSource(rand.Int63())))
}

// NewWithRand lets tests inject a deterministic RNG.
func NewWithRand(out engine.Sender, rng *rand.Rand) *Manager {
	return &Manager{out: out, rng: rng, karma: make(map[string]map[string]int)}
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
		score := m.adjust(msg.Network, target, delta)
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("%s: %d", target, score))
		acted = true
	}
	return acted
}

func (m *Manager) adjust(network, nick string, delta int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	net := m.karma[strings.ToLower(network)]
	if net == nil {
		net = make(map[string]int)
		m.karma[strings.ToLower(network)] = net
	}
	net[strings.ToLower(nick)] += delta
	return net[strings.ToLower(nick)]
}

func (m *Manager) report(network, nick string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	score := m.karma[strings.ToLower(network)][strings.ToLower(nick)]
	return fmt.Sprintf("%s has %d karma.", nick, score)
}

func (m *Manager) leaderboard(network string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	net := m.karma[strings.ToLower(network)]
	if len(net) == 0 {
		return "no karma yet. get to it."
	}
	type kv struct {
		nick  string
		score int
	}
	rows := make([]kv, 0, len(net))
	for n, s := range net {
		rows = append(rows, kv{n, s})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].nick < rows[j].nick
	})
	parts := make([]string, 0, 5)
	for i, r := range rows {
		if i >= 5 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", r.nick, r.score))
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
