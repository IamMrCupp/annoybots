package engine

import (
	"testing"
	"time"
)

// ambientPersonality builds a personality with the ambient timer on and a single
// deterministic interjection line, so any Tick emission is unambiguous.
func ambientPersonality() Personality {
	return Personality{
		Name:          "arywen",
		Interjections: Interjections{Enabled: true, Lines: []string{"how dreary."}},
		AmbientTimer: AmbientTimer{
			Enabled:      true,
			Interval:     Duration(time.Minute),
			Chance:       1.0, // always roll yes, so eligibility/cooldown are what we test
			Cooldown:     Duration(5 * time.Minute),
			QuietFor:     Duration(90 * time.Second),
			ActiveWithin: Duration(30 * time.Minute),
		},
	}
}

func TestTickInterjectsIntoQuietChannel(t *testing.T) {
	now := time.Unix(1000, 0)
	e := newTestEngine(t, ambientPersonality(), &now)
	e.recordActivity("net", "#chan")

	now = now.Add(2 * time.Minute) // inside [90s, 30m] — a lull worth breaking
	rec := &recorder{}
	e.Tick(rec)

	if got := rec.all(); len(got) != 1 {
		t.Fatalf("expected one ambient line, got %v", got)
	}
}

func TestTickSkipsStillActiveChannel(t *testing.T) {
	now := time.Unix(1000, 0)
	e := newTestEngine(t, ambientPersonality(), &now)
	e.recordActivity("net", "#chan")

	now = now.Add(30 * time.Second) // < QuietFor: conversation still going
	rec := &recorder{}
	e.Tick(rec)

	if got := rec.all(); len(got) != 0 {
		t.Fatalf("expected silence over an active channel, got %v", got)
	}
}

func TestTickSkipsDeadChannel(t *testing.T) {
	now := time.Unix(1000, 0)
	e := newTestEngine(t, ambientPersonality(), &now)
	e.recordActivity("net", "#chan")

	now = now.Add(45 * time.Minute) // > ActiveWithin: dead, leave it be
	rec := &recorder{}
	e.Tick(rec)

	if got := rec.all(); len(got) != 0 {
		t.Fatalf("expected silence over a dead channel, got %v", got)
	}
}

func TestTickWithNoActivityEmitsNothing(t *testing.T) {
	now := time.Unix(1000, 0)
	e := newTestEngine(t, ambientPersonality(), &now)
	rec := &recorder{}
	e.Tick(rec)
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("expected silence with no tracked activity, got %v", got)
	}
}

func TestTickRespectsCooldown(t *testing.T) {
	now := time.Unix(1000, 0)
	e := newTestEngine(t, ambientPersonality(), &now)
	e.recordActivity("net", "#chan")
	now = now.Add(2 * time.Minute)

	rec := &recorder{}
	e.Tick(rec) // emits, starts the 5m cooldown
	e.Tick(rec) // still in cooldown — must stay quiet
	if got := rec.all(); len(got) != 1 {
		t.Fatalf("cooldown should allow only one line, got %v", got)
	}

	now = now.Add(6 * time.Minute) // past cooldown, still inside ActiveWithin
	e.Tick(rec)
	if got := rec.all(); len(got) != 2 {
		t.Fatalf("expected a second line after cooldown, got %v", got)
	}
}

func TestTickDisabledIsNoop(t *testing.T) {
	now := time.Unix(1000, 0)
	p := ambientPersonality()
	p.AmbientTimer.Enabled = false
	e := newTestEngine(t, p, &now)
	e.recordActivity("net", "#chan")
	now = now.Add(2 * time.Minute)
	rec := &recorder{}
	e.Tick(rec)
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("disabled ambient timer must emit nothing, got %v", got)
	}
}
