package engine

import (
	"testing"
	"time"
)

func banterPersonality() Personality {
	return Personality{
		Name:     "arywen",
		Siblings: []string{"Kurkutu"},
		Triggers: []Trigger{{
			Name: "greet", Pattern: "hello", Responses: []string{"NORMAL-TRIGGER"},
		}},
		Banter: Banter{
			Enabled:      true,
			Chance:       1,
			Cooldown:     Duration(20 * time.Second),
			MaxPerWindow: 2,
			Window:       Duration(5 * time.Minute),
			Lines:        []string{"oh, it's {nick}."},
		},
	}
}

func siblingMsg(text string) Message {
	return Message{Network: "net", Channel: "#chan", Nick: "Kurkutu", Text: text, Self: "arywen"}
}

func TestSiblingGetsBanterNotNormalTrigger(t *testing.T) {
	now := time.Unix(0, 0)
	e := newTestEngine(t, banterPersonality(), &now)
	r := &recorder{}
	e.Handle(siblingMsg("hello"), r) // would match the greet trigger if not a sibling
	got := r.all()
	if len(got) != 1 || got[0] != "SAY net #chan oh, it's Kurkutu." {
		t.Fatalf("expected banter, not normal trigger: %#v", got)
	}
}

func TestBanterCooldown(t *testing.T) {
	now := time.Unix(0, 0)
	e := newTestEngine(t, banterPersonality(), &now)
	r := &recorder{}
	e.Handle(siblingMsg("a"), r)
	e.Handle(siblingMsg("b"), r) // within 20s cooldown -> suppressed
	if n := len(r.all()); n != 1 {
		t.Fatalf("expected 1 banter within cooldown, got %d", n)
	}
}

func TestBanterWindowCap(t *testing.T) {
	now := time.Unix(0, 0)
	p := banterPersonality()
	p.Banter.Cooldown = 0 // isolate the windowed cap
	e := newTestEngine(t, p, &now)
	r := &recorder{}

	for i := 0; i < 5; i++ {
		e.Handle(siblingMsg("x"), r)
		now = now.Add(time.Second) // advance past any per-call timing, stay in window
	}
	// MaxPerWindow is 2, window 5m: only 2 allowed.
	if n := len(r.all()); n != 2 {
		t.Fatalf("expected windowed cap of 2 banter lines, got %d", n)
	}

	now = now.Add(6 * time.Minute) // window elapses
	e.Handle(siblingMsg("x"), r)
	if n := len(r.all()); n != 3 {
		t.Fatalf("expected banter to resume after window, got %d", n)
	}
}

func TestNonSiblingUnaffectedByBanter(t *testing.T) {
	now := time.Unix(0, 0)
	e := newTestEngine(t, banterPersonality(), &now)
	r := &recorder{}
	e.Handle(msg("hello"), r) // "victim" is not a sibling -> normal trigger fires
	got := r.all()
	if len(got) != 1 || got[0] != "SAY net #chan NORMAL-TRIGGER" {
		t.Fatalf("expected normal trigger for non-sibling: %#v", got)
	}
}
