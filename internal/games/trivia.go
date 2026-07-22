package games

import (
	"fmt"
	"strings"
	"sync"
)

// Trivia: one question per channel at a time, and the first correct answer in
// channel takes it. Answers are matched loosely (case- and space-insensitive)
// so nobody loses on punctuation.

type triviaQ struct {
	q string
	a []string // accepted answers; the first is the canonical one
}

var triviaPack = []triviaQ{
	{"which protocol does IRC use to carry channel messages?", []string{"privmsg"}},
	{"what does the IRC command MODE +o grant?", []string{"op", "operator", "channel operator", "ops"}},
	{"in D&D, which die decides most attack rolls?", []string{"d20", "20", "twenty"}},
	{"what's the answer to life, the universe, and everything?", []string{"42", "forty-two", "forty two"}},
	{"which Go keyword starts a goroutine?", []string{"go"}},
	{"what does DNS stand for?", []string{"domain name system", "domain name service"}},
	{"which planet is known as the red planet?", []string{"mars"}},
	{"how many bits in a byte?", []string{"8", "eight"}},
	{"what year did IRC first appear?", []string{"1988"}},
	{"which company originally created Go?", []string{"google"}},
	{"in Unix, which signal does Ctrl-C send?", []string{"sigint", "2", "interrupt"}},
	{"what does the 'K' in K8s stand in for?", []string{"kubernetes"}},
	{"what's the default HTTPS port?", []string{"443"}},
	{"which sorting algorithm is O(n log n) on average and in-place?", []string{"quicksort", "quick sort"}},
	{"what does RAID 1 do?", []string{"mirror", "mirroring", "mirrors"}},
}

// trivia holds one channel's active question.
type trivia struct {
	q     triviaQ
	asker string
}

// triviaState is the per-channel question board.
type triviaState struct {
	mu   sync.Mutex
	open map[string]*trivia // "network|channel" -> active question
}

func newTriviaState() *triviaState { return &triviaState{open: map[string]*trivia{}} }

func chanKey(network, channel string) string {
	return strings.ToLower(network) + "|" + strings.ToLower(channel)
}

// ask starts a question in a channel, or re-states the open one.
func (m *Manager) ask(network, channel, nick string) string {
	key := chanKey(network, channel)
	m.trivia.mu.Lock()
	defer m.trivia.mu.Unlock()
	if t, ok := m.trivia.open[key]; ok {
		return "a question is already open — " + t.q.q
	}
	q := triviaPack[m.rng.Intn(len(triviaPack))]
	m.trivia.open[key] = &trivia{q: q, asker: nick}
	return "🧠 trivia — " + q.q
}

// answer checks a line against the channel's open question. It returns the
// winning line and true when someone gets it right.
func (m *Manager) answer(network, channel, nick, text string) (string, bool) {
	key := chanKey(network, channel)
	m.trivia.mu.Lock()
	defer m.trivia.mu.Unlock()
	t, ok := m.trivia.open[key]
	if !ok {
		return "", false
	}
	guess := normalizeAnswer(text)
	if guess == "" {
		return "", false
	}
	for _, a := range t.q.a {
		if guess == normalizeAnswer(a) {
			delete(m.trivia.open, key)
			return fmt.Sprintf("🧠 %s gets it — %s. correct!", nick, t.q.a[0]), true
		}
	}
	return "", false
}

// normalizeAnswer lowercases, trims, and collapses whitespace/punctuation so an
// answer isn't lost to a trailing question mark.
func normalizeAnswer(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, ".!?,'\"")
	return strings.Join(strings.Fields(s), " ")
}
