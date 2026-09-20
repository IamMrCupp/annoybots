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

func TestChanKeepHoldsOp(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r) // self is "arwyen"
	if k.HoldsOp("#tns") {
		t.Fatal("no state yet — should not report holding ops")
	}
	k.onJoin("#tns", "arwyen") // present but not opped
	if k.HoldsOp("#tns") {
		t.Fatal("present but un-opped — should not report holding ops")
	}
	k.onModeOp("#tns", "arwyen", true) // now opped
	if !k.HoldsOp("#tns") {
		t.Fatal("should report holding ops after +o on self")
	}
	if k.HoldsOp("#other") {
		t.Fatal("holding ops is per-channel")
	}
	k.onModeOp("#tns", "arwyen", false) // deopped
	if k.HoldsOp("#tns") {
		t.Fatal("should not report holding ops after -o on self")
	}
}

// The regression this fix exists for: both bots are opped, the connection drops,
// and on reconnect nobody holds ops any more. Without a reset the keeper still
// believes the sibling is opped and skips it forever — so a manual +o on the bot
// produces nothing at all.
func TestChanKeepResetClearsStaleOpsOnReconnect(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onNames("#tns", "@arwyen @kurkutu") // both opped before the split
	k.onEndNames("#tns")
	if r.count() != 0 {
		t.Fatalf("nothing to do while the sibling is already opped, got %#v", r.calls)
	}

	k.reset() // reconnect

	// Rejoin: nobody holds ops now.
	k.onNames("#tns", "arwyen kurkutu")
	k.onEndNames("#tns")
	if r.count() != 0 {
		t.Fatalf("bot isn't opped — it can't op anyone, got %#v", r.calls)
	}
	if k.HoldsOp("#tns") {
		t.Fatal("HoldsOp must not survive a reset")
	}

	k.onModeOp("#tns", "arwyen", true) // an op hands the bot ops
	if !r.has("#tns +o kurkutu") {
		t.Fatalf("sibling should be opped after the reset, got %#v", r.calls)
	}
}

// A NAMES burst replaces the channel's state. Without that, a rejoin merges into
// the old map and a since-deopped nick keeps a stale ops entry — 353 can only add.
func TestChanKeepNamesBurstReplacesState(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onNames("#tns", "@arwyen @kurkutu")
	k.onEndNames("#tns")

	// Fresh burst: the bot still holds ops, the sibling no longer does.
	k.onNames("#tns", "@arwyen")
	k.onNames("#tns", "kurkutu") // second 353 of the same burst accumulates
	k.onEndNames("#tns")

	if !r.has("#tns +o kurkutu") {
		t.Fatalf("a deopped sibling should be re-opped after a fresh NAMES, got %#v", r.calls)
	}
	if !k.HoldsOp("#tns") {
		t.Fatal("the bot's own op state should survive a burst that still lists it as @")
	}
}

// Someone who left between bursts must not linger as a member.
func TestChanKeepNamesBurstDropsDepartedMembers(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onNames("#tns", "@arwyen kurkutu")
	k.onEndNames("#tns")
	r.mu.Lock()
	r.calls = nil
	r.mu.Unlock()

	k.onNames("#tns", "@arwyen") // sibling gone while we weren't looking
	k.onEndNames("#tns")
	if r.count() != 0 {
		t.Fatalf("an absent sibling must not be opped, got %#v", r.calls)
	}
}

func TestChanKeepForgetDropsChannel(t *testing.T) {
	r := &modeRec{}
	k := newKeeper(r)
	k.onNames("#tns", "@arwyen @kurkutu")
	k.onEndNames("#tns")

	k.forget("#TNS") // case-insensitive — we parted or were kicked
	if k.HoldsOp("#tns") {
		t.Fatal("forgotten channel must not report op state")
	}
	k.onJoin("#tns", "kurkutu")
	if r.count() != 0 {
		t.Fatalf("no op state after forget means nothing to enforce, got %#v", r.calls)
	}
}
