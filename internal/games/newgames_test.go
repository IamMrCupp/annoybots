package games

import (
	"strings"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

func chanMsg(nick, text string) engine.Message {
	return engine.Message{Network: "net", Channel: "#c", Nick: nick, Text: text}
}

func TestSlotsAlwaysProducesAResult(t *testing.T) {
	r := &recorder{}
	m := newGames(r)
	for i := 0; i < 50; i++ {
		if !m.Handle(chanMsg("alice", "!slots")) {
			t.Fatal("!slots should be consumed")
		}
	}
	if len(r.lines) != 50 {
		t.Fatalf("every pull should say something, got %d", len(r.lines))
	}
	for _, l := range r.lines {
		if !strings.Contains(l, "[") || !strings.Contains(l, "]") {
			t.Fatalf("a pull should show the reels, got %q", l)
		}
	}
}

func TestTriviaAsksAndAcceptsTheAnswer(t *testing.T) {
	r := &recorder{}
	m := newGames(r)

	m.Handle(chanMsg("alice", "!trivia"))
	if !strings.Contains(r.last(), "trivia") {
		t.Fatalf("a question should be asked, got %q", r.last())
	}
	// find the open question so the test doesn't depend on which one was picked
	m.trivia.mu.Lock()
	q := m.trivia.open[chanKey("net", "#c")].q
	m.trivia.mu.Unlock()

	// a wrong answer is not consumed as a win
	if m.Handle(chanMsg("bob", "definitely not the answer")) {
		t.Fatal("a wrong answer should not win")
	}
	// the right one, with sloppy casing/punctuation, wins
	if !m.Handle(chanMsg("bob", strings.ToUpper(q.a[0])+"!")) {
		t.Fatal("the correct answer should be consumed as a win")
	}
	if !strings.Contains(r.last(), "correct") || !strings.Contains(r.last(), "bob") {
		t.Fatalf("the winner should be named, got %q", r.last())
	}
	// the question is closed now
	if _, won := m.answer("net", "#c", "carol", q.a[0]); won {
		t.Fatal("a solved question should be closed")
	}
}

func TestTriviaIsPerChannel(t *testing.T) {
	r := &recorder{}
	m := newGames(r)
	m.Handle(chanMsg("alice", "!trivia"))
	// a second ask in the same channel re-states rather than replacing
	m.Handle(chanMsg("alice", "!trivia"))
	if !strings.Contains(r.last(), "already open") {
		t.Fatalf("one question per channel, got %q", r.last())
	}
	// a different channel gets its own
	m.Handle(engine.Message{Network: "net", Channel: "#other", Nick: "alice", Text: "!trivia"})
	if strings.Contains(r.last(), "already open") {
		t.Fatalf("a different channel should get its own question, got %q", r.last())
	}
}

func TestHangmanPlaysThrough(t *testing.T) {
	r := &recorder{}
	m := newGames(r)
	m.Handle(chanMsg("alice", "!hangman"))
	if !strings.Contains(r.last(), "hangman") {
		t.Fatalf("a game should start, got %q", r.last())
	}
	m.hangman.mu.Lock()
	word := m.hangman.open[chanKey("net", "#c")].word
	m.hangman.mu.Unlock()

	// guessing the whole word wins
	m.Handle(chanMsg("bob", "!guess "+word))
	if !strings.Contains(r.last(), "spared") {
		t.Fatalf("guessing the word should win, got %q", r.last())
	}
	// and the game is over
	m.Handle(chanMsg("bob", "!guess a"))
	if !strings.Contains(r.last(), "no hangman running") {
		t.Fatalf("the game should be finished, got %q", r.last())
	}
}

func TestHangmanRunsOutOfLives(t *testing.T) {
	r := &recorder{}
	m := newGames(r)
	m.Handle(chanMsg("alice", "!hangman"))
	m.hangman.mu.Lock()
	word := m.hangman.open[chanKey("net", "#c")].word
	m.hangman.mu.Unlock()

	// burn every life on letters the word can't contain
	wrong := 0
	for _, c := range "qxzjvkwyubg" {
		if strings.ContainsRune(word, c) {
			continue
		}
		m.Handle(chanMsg("bob", "!guess "+string(c)))
		wrong++
		if wrong == hangmanLives {
			break
		}
	}
	if !strings.Contains(r.last(), "last life") {
		t.Fatalf("running out of lives should end it, got %q", r.last())
	}
	if !strings.Contains(r.last(), word) {
		t.Fatalf("the word should be revealed, got %q", r.last())
	}
}

func TestHangmanLetterFeedback(t *testing.T) {
	r := &recorder{}
	m := newGames(r)
	m.Handle(chanMsg("alice", "!hangman"))
	m.hangman.mu.Lock()
	g := m.hangman.open[chanKey("net", "#c")]
	first := []rune(g.word)[0]
	m.hangman.mu.Unlock()

	m.Handle(chanMsg("bob", "!guess "+string(first)))
	if !strings.Contains(r.last(), "yes") && !strings.Contains(r.last(), "finishes") {
		t.Fatalf("a correct letter should be acknowledged, got %q", r.last())
	}
	// repeating it is called out (unless that letter finished the word)
	if strings.Contains(r.last(), "yes") {
		m.Handle(chanMsg("bob", "!guess "+string(first)))
		if !strings.Contains(r.last(), "has been tried") {
			t.Fatalf("a repeat guess should be called out, got %q", r.last())
		}
	}
}
