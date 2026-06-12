package engine

import (
	"testing"
	"time"
)

func quotePersonality() Personality {
	return Personality{
		Name:     "arywen",
		Commands: true,
		Quotes: Quotes{
			Enabled: true,
			Command: true,
			Packs: []QuotePack{
				{Name: "rickmorty", Lines: []string{"Wubba lubba dub dub"}},
				{Name: "southpark", Lines: []string{"Respect my authoritah"}},
			},
		},
	}
}

func TestQuoteCommandSpecificPack(t *testing.T) {
	now := time.Unix(0, 0)
	e := newTestEngine(t, quotePersonality(), &now)
	r := &recorder{}
	e.Handle(msg("!quote southpark"), r)
	got := r.all()
	if len(got) != 1 || got[0] != "SAY net #chan Respect my authoritah" {
		t.Fatalf("expected south park quote, got %#v", got)
	}
}

func TestQuoteCommandUnknownPack(t *testing.T) {
	now := time.Unix(0, 0)
	e := newTestEngine(t, quotePersonality(), &now)
	r := &recorder{}
	e.Handle(msg("!quote nope"), r)
	got := r.all()
	if len(got) != 1 || got[0] != "SAY net #chan no such quote pack: nope" {
		t.Fatalf("expected unknown-pack notice, got %#v", got)
	}
}

func TestPacksCommandListsPacks(t *testing.T) {
	now := time.Unix(0, 0)
	e := newTestEngine(t, quotePersonality(), &now)
	r := &recorder{}
	e.Handle(msg("!packs"), r)
	got := r.all()
	if len(got) != 1 || got[0] != "SAY net #chan quote packs: rickmorty, southpark — try !quote <name>" {
		t.Fatalf("unexpected packs listing: %#v", got)
	}
}

func TestAddQuoteCreatesPackAndMerges(t *testing.T) {
	now := time.Unix(0, 0)
	e := newTestEngine(t, quotePersonality(), &now)

	// Add to an existing file pack: should merge.
	if !e.AddQuote("rickmorty", "Get schwifty") {
		t.Fatal("expected AddQuote to succeed")
	}
	if got := e.quotesFromPack("rickmorty"); len(got) != 2 {
		t.Fatalf("expected merged file+runtime lines, got %#v", got)
	}

	// Add a brand-new runtime pack: should appear in PackNames.
	e.AddQuote("custom", "a fresh annoyance")
	names := e.PackNames()
	found := false
	for _, n := range names {
		if n == "custom" {
			found = true
		}
	}
	if !found {
		t.Fatalf("new runtime pack missing from PackNames: %#v", names)
	}
}

func TestAddQuoteDedupAndDel(t *testing.T) {
	now := time.Unix(0, 0)
	e := newTestEngine(t, quotePersonality(), &now)
	if !e.AddQuote("custom", "dupe") {
		t.Fatal("first add should succeed")
	}
	if e.AddQuote("custom", "dupe") {
		t.Fatal("duplicate add should be rejected")
	}
	if !e.DelQuote("custom", "dupe") {
		t.Fatal("del should remove the runtime line")
	}
	if e.DelQuote("custom", "dupe") {
		t.Fatal("second del should find nothing")
	}
}

func TestAmbientQuoteRespectsCooldown(t *testing.T) {
	now := time.Unix(0, 0)
	p := quotePersonality()
	p.Quotes.Chance = 1
	p.Quotes.Cooldown = Duration(time.Minute)
	e := newTestEngine(t, p, &now)
	r := &recorder{}
	e.Handle(msg("ramble one"), r)
	e.Handle(msg("ramble two"), r) // suppressed by cooldown
	if n := len(r.all()); n != 1 {
		t.Fatalf("expected 1 ambient quote within cooldown, got %d", n)
	}
}
