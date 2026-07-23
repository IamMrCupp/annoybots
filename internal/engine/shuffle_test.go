package engine

import (
	"math/rand"
	"testing"
)

func bagEngine() *Engine { return &Engine{rng: rand.New(rand.NewSource(1))} }

// The reported symptom: a 3-line banter pool repeated the same line constantly.
// A bag must deal every line once before any line comes round again.
func TestPickCyclesWholePoolBeforeRepeating(t *testing.T) {
	e := bagEngine()
	pool := []string{"a", "b", "c"}

	for round := 0; round < 20; round++ {
		seen := map[string]int{}
		for i := 0; i < len(pool); i++ {
			seen[e.pick(pool)]++
		}
		if len(seen) != len(pool) {
			t.Fatalf("round %d: every line should appear once per cycle, got %v", round, seen)
		}
		for line, n := range seen {
			if n != 1 {
				t.Fatalf("round %d: %q appeared %d times in one cycle", round, line, n)
			}
		}
	}
}

// The worst-looking case in the log: the same line twice in a row.
func TestPickNeverRepeatsBackToBack(t *testing.T) {
	e := bagEngine()
	pool := []string{"a", "b", "c"}
	prev := ""
	for i := 0; i < 500; i++ {
		got := e.pick(pool)
		if got == prev {
			t.Fatalf("iteration %d: %q repeated back-to-back", i, got)
		}
		prev = got
	}
}

func TestPickKeepsPoolsIndependent(t *testing.T) {
	// Two pools must not share a bag, or one would starve the other.
	e := bagEngine()
	a := []string{"a1", "a2"}
	b := []string{"b1", "b2"}
	for i := 0; i < 50; i++ {
		if got := e.pick(a); got != "a1" && got != "a2" {
			t.Fatalf("pool A returned %q", got)
		}
		if got := e.pick(b); got != "b1" && got != "b2" {
			t.Fatalf("pool B returned %q", got)
		}
	}
}

func TestPickHandlesEdgeCases(t *testing.T) {
	e := bagEngine()
	if got := e.pick(nil); got != "" {
		t.Fatalf("an empty pool should yield an empty string, got %q", got)
	}
	// A single-line pool can only repeat; it must not spin or panic.
	one := []string{"only"}
	for i := 0; i < 5; i++ {
		if got := e.pick(one); got != "only" {
			t.Fatalf("a one-line pool should return its line, got %q", got)
		}
	}
}

func TestPickChangedPoolStartsFresh(t *testing.T) {
	// A !reload or !addquote changes the pool; it should just work.
	e := bagEngine()
	before := []string{"x", "y"}
	e.pick(before)
	after := []string{"x", "y", "z"}
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		seen[e.pick(after)] = true
	}
	if len(seen) != 3 {
		t.Fatalf("the enlarged pool should deal all three lines, got %v", seen)
	}
}
