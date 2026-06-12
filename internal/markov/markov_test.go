package markov

import (
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateEmptyChain(t *testing.T) {
	c := New(2)
	if got := c.Generate(rand.New(rand.NewSource(1)), 10); got != "" {
		t.Fatalf("empty chain should generate empty string, got %q", got)
	}
}

func TestLearnAndGenerateDeterministic(t *testing.T) {
	c := New(2)
	c.Learn("the cat sat on the mat")
	// With a single learned line and order 2, the only path reproduces the line.
	got := c.Generate(rand.New(rand.NewSource(42)), 30)
	if got != "the cat sat on the mat" {
		t.Fatalf("expected the learned line, got %q", got)
	}
}

func TestGenerateRespectsMaxWords(t *testing.T) {
	c := New(1)
	// A loop: "a b a b a b ..." so generation could run forever without a cap.
	for i := 0; i < 50; i++ {
		c.Learn("a b a b a b a b")
	}
	got := c.Generate(rand.New(rand.NewSource(7)), 5)
	if n := len(strings.Fields(got)); n > 5 {
		t.Fatalf("expected at most 5 words, got %d (%q)", n, got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	c := New(2)
	c.Learn("annoying bots never die")
	path := filepath.Join(t.TempDir(), "brain.gob")
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Order() != c.Order() {
		t.Fatalf("order mismatch: %d vs %d", loaded.Order(), c.Order())
	}
	if loaded.Size() != c.Size() {
		t.Fatalf("size mismatch: %d vs %d", loaded.Size(), c.Size())
	}
	if got := loaded.Generate(rand.New(rand.NewSource(1)), 30); got != "annoying bots never die" {
		t.Fatalf("loaded chain produced %q", got)
	}
}
