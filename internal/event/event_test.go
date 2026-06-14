package event

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestDispatcherDeliversByKind(t *testing.T) {
	d := New()
	var joins, parts int
	d.On(Join, func(Event) { joins++ })
	d.On(Part, func(Event) { parts++ })

	d.Emit(Event{Kind: Join, Nick: "a"})
	d.Emit(Event{Kind: Join, Nick: "b"})
	d.Emit(Event{Kind: Part, Nick: "a"})
	d.Emit(Event{Kind: Quit, Nick: "c"}) // no subscriber

	if joins != 2 || parts != 1 {
		t.Fatalf("joins=%d parts=%d, want 2 and 1", joins, parts)
	}
}

func TestDispatcherMultipleSubscribers(t *testing.T) {
	d := New()
	var n int
	for i := 0; i < 3; i++ {
		d.On(Join, func(Event) { n++ })
	}
	d.Emit(Event{Kind: Join})
	if n != 3 {
		t.Fatalf("got %d deliveries, want 3", n)
	}
}

func TestDispatcherConcurrent(t *testing.T) {
	d := New()
	var count int64
	d.On(Message, func(Event) { atomic.AddInt64(&count, 1) })

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.On(Join, func(Event) {})   // concurrent subscribe
			d.Emit(Event{Kind: Message}) // concurrent emit
		}()
	}
	wg.Wait()
	if count != 50 {
		t.Fatalf("got %d message deliveries, want 50", count)
	}
}

func TestSplitUserHost(t *testing.T) {
	cases := []struct{ in, ident, host string }{
		{"nick!~user@host.example.com", "~user", "host.example.com"},
		{"arwyen!zncuser@107-196-170-190.lightspeed.frokca.sbcglobal.net", "zncuser", "107-196-170-190.lightspeed.frokca.sbcglobal.net"},
		{"nick!ident", "ident", ""},
		{"justnick", "", ""},
	}
	for _, c := range cases {
		ident, host := SplitUserHost(c.in)
		if ident != c.ident || host != c.host {
			t.Errorf("SplitUserHost(%q) = (%q,%q), want (%q,%q)", c.in, ident, host, c.ident, c.host)
		}
	}
}

func TestKindString(t *testing.T) {
	if Join.String() != "join" || Mode.String() != "mode" || Kind(99).String() != "unknown" {
		t.Fatal("Kind.String mismatch")
	}
}
