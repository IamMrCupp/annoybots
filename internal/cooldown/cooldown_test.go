package cooldown

import (
	"testing"
	"time"
)

func TestUseRespectsWindow(t *testing.T) {
	now := time.Unix(0, 0)
	m := NewWithClock(func() time.Time { return now })

	if !m.Use("k", time.Minute) {
		t.Fatal("first use should succeed")
	}
	if m.Use("k", time.Minute) {
		t.Fatal("second immediate use should fail")
	}
	now = now.Add(30 * time.Second)
	if m.Use("k", time.Minute) {
		t.Fatal("use before window elapses should fail")
	}
	now = now.Add(31 * time.Second)
	if !m.Use("k", time.Minute) {
		t.Fatal("use after window should succeed")
	}
}

func TestReadyDoesNotConsume(t *testing.T) {
	now := time.Unix(0, 0)
	m := NewWithClock(func() time.Time { return now })

	if !m.Ready("k", time.Minute) {
		t.Fatal("unseen key should be ready")
	}
	if !m.Ready("k", time.Minute) {
		t.Fatal("Ready must not consume the window")
	}
	m.Use("k", time.Minute)
	if m.Ready("k", time.Minute) {
		t.Fatal("key should be on cooldown after Use")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	now := time.Unix(0, 0)
	m := NewWithClock(func() time.Time { return now })
	if !m.Use("a", time.Hour) {
		t.Fatal("a should fire")
	}
	if !m.Use("b", time.Hour) {
		t.Fatal("b should fire independently of a")
	}
}
