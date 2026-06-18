package irc

import (
	"io"
	"log/slog"
	"sync"
	"testing"
)

type modeRec struct {
	mu    sync.Mutex
	calls []string
}

func (r *modeRec) send(channel, modes, arg string) {
	r.mu.Lock()
	r.calls = append(r.calls, channel+" "+modes+" "+arg)
	r.mu.Unlock()
}
func (r *modeRec) has(s string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c == s {
			return true
		}
	}
	return false
}
func (r *modeRec) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) }

func newKeeper(r *modeRec) *chankeeper {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newChankeeper([]string{"kurkutu"}, func() string { return "arwyen" }, r.send, log)
}

func TestChanKeepOpsProtectedOnSelfOp(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onJoin("#tns", "kurkutu") // sibling present, bot not yet opped
	if r.count() != 0 {
		t.Fatal("must not op before the bot itself is opped")
	}
	k.onModeOp("#tns", "arwyen", true) // someone ops the bot
	if !r.has("#tns +o kurkutu") {
		t.Fatalf("bot should op its sibling once opped, got %#v", r.calls)
	}
}

func TestChanKeepOpsSiblingOnJoin(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onNames("#tns", "@arwyen") // bot already opped (from NAMES)
	k.onEndNames("#tns")
	k.onJoin("#tns", "kurkutu") // sibling arrives later
	if !r.has("#tns +o kurkutu") {
		t.Fatalf("sibling joining an opped bot should be opped, got %#v", r.calls)
	}
}

func TestChanKeepReopsOnDeop(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onNames("#tns", "@arwyen @kurkutu") // both opped already
	k.onEndNames("#tns")
	if r.count() != 0 {
		t.Fatal("nothing to do when the sibling is already opped")
	}
	k.onModeOp("#tns", "kurkutu", false) // someone deops the sibling
	if !r.has("#tns +o kurkutu") {
		t.Fatalf("bot should re-op a deopped sibling, got %#v", r.calls)
	}
}

func TestChanKeepDoesNothingWhenNotOpped(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onNames("#tns", "arwyen kurkutu") // present but bot NOT opped
	k.onEndNames("#tns")
	k.onJoin("#tns", "kurkutu")
	if r.count() != 0 {
		t.Fatalf("must not op anyone while unopped, got %#v", r.calls)
	}
}

func TestChanKeepIgnoresNonProtected(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onModeOp("#tns", "arwyen", true) // bot opped
	k.onJoin("#tns", "rando")          // a normal user joins
	if r.count() != 0 {
		t.Fatalf("must not op non-protected users, got %#v", r.calls)
	}
}

func TestChanKeepCooldownPreventsFlood(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onModeOp("#tns", "arwyen", true) // bot opped
	k.onJoin("#tns", "kurkutu")        // -> op (1)
	k.onJoin("#tns", "kurkutu")        // rapid repeat -> cooldown blocks
	if r.count() != 1 {
		t.Fatalf("cooldown should allow only one op, got %#v", r.calls)
	}
}

func TestOpChangesParser(t *testing.T) {
	// "+oo-v a b c" -> +o a, +o b, (-v c ignored)
	got := opChanges("+oo-v", []string{"a", "b", "c"})
	if len(got) != 2 || got[0].nick != "a" || !got[0].add || got[1].nick != "b" {
		t.Fatalf("opChanges parse wrong: %#v", got)
	}
	// "-o+b nick mask" -> -o nick (b consumes mask, ignored)
	got = opChanges("-o+b", []string{"nick", "*!*@bad"})
	if len(got) != 1 || got[0].nick != "nick" || got[0].add {
		t.Fatalf("opChanges parse wrong: %#v", got)
	}
}
