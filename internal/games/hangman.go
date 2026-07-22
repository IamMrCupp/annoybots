package games

import (
	"fmt"
	"strings"
	"sync"
)

// Hangman: one word per channel, everyone guesses together. Six wrong guesses
// and the channel loses collectively, which is the fun part.

const hangmanLives = 6

var hangmanWords = []string{
	"bouncer", "netsplit", "kickban", "hostmask", "eggdrop", "botnet",
	"channel", "operator", "quitmsg", "identify", "nickserv", "partyline",
	"markov", "trigger", "interject", "distroless", "kubernetes", "goroutine",
	"idlerpg", "bestiary", "legendary", "dungeon", "alignment", "rebirth",
}

// hangman is one channel's game in progress.
type hangman struct {
	word   string
	got    map[rune]bool // correctly guessed letters
	missed []rune        // wrong guesses, in order
}

// hangmanState is the per-channel game board.
type hangmanState struct {
	mu   sync.Mutex
	open map[string]*hangman
}

func newHangmanState() *hangmanState { return &hangmanState{open: map[string]*hangman{}} }

// startHangman begins a game, or re-draws the one already running.
func (m *Manager) startHangman(network, channel string) string {
	key := chanKey(network, channel)
	m.hangman.mu.Lock()
	defer m.hangman.mu.Unlock()
	if g, ok := m.hangman.open[key]; ok {
		return "a game is already running — " + g.render()
	}
	g := &hangman{word: hangmanWords[m.rng.Intn(len(hangmanWords))], got: map[rune]bool{}}
	m.hangman.open[key] = g
	return "🪢 hangman — " + g.render() + " · !guess <letter|word>"
}

// guess plays a letter or a whole word.
func (m *Manager) guess(network, channel, nick, attempt string) string {
	key := chanKey(network, channel)
	attempt = strings.ToLower(strings.TrimSpace(attempt))
	if attempt == "" {
		return "usage: !guess <letter|word>"
	}
	m.hangman.mu.Lock()
	defer m.hangman.mu.Unlock()
	g, ok := m.hangman.open[key]
	if !ok {
		return "no hangman running here — !hangman starts one."
	}

	// A whole-word guess wins or costs a life.
	if len([]rune(attempt)) > 1 {
		if attempt == g.word {
			delete(m.hangman.open, key)
			return fmt.Sprintf("🪢 %s calls it — “%s”. the channel is spared.", nick, g.word)
		}
		g.missed = append(g.missed, '?')
		if len(g.missed) >= hangmanLives {
			delete(m.hangman.open, key)
			return fmt.Sprintf("🪢 wrong, and that was the last life. the word was “%s”.", g.word)
		}
		return fmt.Sprintf("🪢 not “%s”. %s · %d live(s) left", attempt, g.render(), hangmanLives-len(g.missed))
	}

	r := []rune(attempt)[0]
	if g.got[r] || containsRune(g.missed, r) {
		return fmt.Sprintf("🪢 %c has been tried. %s", r, g.render())
	}
	if strings.ContainsRune(g.word, r) {
		g.got[r] = true
		if g.solved() {
			delete(m.hangman.open, key)
			return fmt.Sprintf("🪢 %s finishes it — “%s”. saved!", nick, g.word)
		}
		return fmt.Sprintf("🪢 yes, %c. %s", r, g.render())
	}
	g.missed = append(g.missed, r)
	if len(g.missed) >= hangmanLives {
		delete(m.hangman.open, key)
		return fmt.Sprintf("🪢 no %c — and that was the last life. the word was “%s”.", r, g.word)
	}
	return fmt.Sprintf("🪢 no %c. %s · %d live(s) left", r, g.render(), hangmanLives-len(g.missed))
}

// render shows the word with unguessed letters masked.
func (g *hangman) render() string {
	var b strings.Builder
	for _, r := range g.word {
		if g.got[r] {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		b.WriteByte(' ')
	}
	return strings.TrimSpace(b.String())
}

// solved reports whether every letter has been found.
func (g *hangman) solved() bool {
	for _, r := range g.word {
		if !g.got[r] {
			return false
		}
	}
	return true
}

func containsRune(rs []rune, r rune) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}
