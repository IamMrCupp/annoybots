package ratelimit

import (
	"testing"
	"time"
)

func TestAllowConsumesBurst(t *testing.T) {
	now := time.Unix(0, 0)
	l := newWithClock(3, 1, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if !l.Allow() {
			t.Fatalf("expected burst token %d to be allowed", i)
		}
	}
	if l.Allow() {
		t.Fatal("expected 4th immediate call to be denied")
	}
}

func TestAllowRefillsOverTime(t *testing.T) {
	now := time.Unix(0, 0)
	l := newWithClock(1, 2, func() time.Time { return now }) // 2 tokens/sec

	if !l.Allow() {
		t.Fatal("first call should be allowed")
	}
	if l.Allow() {
		t.Fatal("bucket should be empty")
	}
	now = now.Add(500 * time.Millisecond) // +1 token
	if !l.Allow() {
		t.Fatal("expected a token after 500ms")
	}
}

func TestReserveReturnsWaitWhenEmpty(t *testing.T) {
	now := time.Unix(0, 0)
	l := newWithClock(1, 2, func() time.Time { return now }) // 2 tokens/sec

	if d := l.Reserve(); d != 0 {
		t.Fatalf("first reserve should be immediate, got %v", d)
	}
	d := l.Reserve()
	// One token deficit at 2/sec => 500ms wait.
	if d < 490*time.Millisecond || d > 510*time.Millisecond {
		t.Fatalf("expected ~500ms wait, got %v", d)
	}
}
