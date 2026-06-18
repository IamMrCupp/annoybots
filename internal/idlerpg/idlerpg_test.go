package idlerpg

import (
	"context"
	"io"
	"log/slog"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/event"
	"github.com/IamMrCupp/annoybots/internal/state"
)

type recorder struct{ lines []string }

func (r *recorder) Say(_, _, text string)    { r.lines = append(r.lines, text) }
func (r *recorder) Action(_, _, text string) { r.lines = append(r.lines, text) }
func (r *recorder) has(sub string) bool {
	for _, l := range r.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
func (r *recorder) last() string {
	if len(r.lines) == 0 {
		return ""
	}
	return r.lines[len(r.lines)-1]
}

func chanMsg(nick, text string) engine.Message {
	return engine.Message{Network: "net", Channel: "#chan", Nick: nick, Text: text}
}

func newMgr() (*Manager, *recorder, state.Store) {
	r := &recorder{}
	st := state.NewMem()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// 1s tick, 1s base ttl → one quiet tick levels you up. nil resolver = key by network|nick.
	m := New(st, r, nil, time.Second, time.Second, log)
	m.rng = rand.New(rand.NewSource(1)) // deterministic for item rolls
	return m, r, st
}

func TestEnrollThenStatus(t *testing.T) {
	m, r, _ := newMgr()
	if !m.Handle(chanMsg("alice", "!rpg")) {
		t.Fatal("!rpg should be consumed")
	}
	if !r.has("welcome to the grind") {
		t.Fatalf("expected enrollment, got %v", r.lines)
	}
	m.Handle(chanMsg("alice", "!rpg"))
	if !strings.Contains(r.last(), "level 0") {
		t.Fatalf("expected status, got %q", r.last())
	}
}

func TestIdleLevelsUp(t *testing.T) {
	m, r, st := newMgr()
	m.Handle(chanMsg("alice", "!rpg")) // enroll + online, ttl=1s
	m.Tick()                           // ttl -> 0 -> level up
	if !r.has("attained level 1") {
		t.Fatalf("expected level-up announcement, got %v", r.lines)
	}
	sheet, _ := st.HGetAll(context.Background(), sheetKey("net|alice"))
	if sheet["level"] != 1 {
		t.Fatalf("level = %d; want 1", sheet["level"])
	}
}

func TestTalkingPenalizes(t *testing.T) {
	m, _, st := newMgr()
	m.Handle(chanMsg("alice", "!rpg")) // enroll, ttl=1
	before, _ := st.HGetAll(context.Background(), sheetKey("net|alice"))
	if m.Handle(chanMsg("alice", "blah blah blah")) {
		t.Fatal("normal chatter is not consumed")
	}
	after, _ := st.HGetAll(context.Background(), sheetKey("net|alice"))
	if after["ttl"] <= before["ttl"] {
		t.Fatalf("talking should raise ttl: %d -> %d", before["ttl"], after["ttl"])
	}
}

func TestLeavingStopsProgress(t *testing.T) {
	m, r, st := newMgr()
	m.Handle(chanMsg("alice", "!rpg")) // online
	m.OnLeave(event.Event{Network: "net", Nick: "alice"})
	m.Tick() // alice is offline -> no progress
	if r.has("attained level") {
		t.Fatal("an offline player must not level up")
	}
	sheet, _ := st.HGetAll(context.Background(), sheetKey("net|alice"))
	if sheet["level"] != 0 {
		t.Fatalf("offline level = %d; want 0", sheet["level"])
	}
}

func TestFindsItemOnLevelUp(t *testing.T) {
	m, r, st := newMgr()
	m.Handle(chanMsg("alice", "!rpg")) // enroll, ttl=1
	m.Tick()                           // level up → finds an item (empty slot, so always > 0)
	if !r.has("found a level") {
		t.Fatalf("expected an item find on level-up, got %v", r.lines)
	}
	sheet, _ := st.HGetAll(context.Background(), sheetKey("net|alice"))
	if itemSum(sheet) <= 0 {
		t.Fatalf("item sum should be positive after a find: %#v", sheet)
	}
}

func TestItemsCommand(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))       // enroll, no items yet
	m.Handle(chanMsg("alice", "!rpg items")) // power 0, nothing yet
	if !strings.Contains(r.last(), "power 0") || !strings.Contains(r.last(), "nothing yet") {
		t.Fatalf("fresh player items wrong: %q", r.last())
	}
	m.Tick()                                 // level up → item
	m.Handle(chanMsg("alice", "!rpg items")) // now has gear
	if strings.Contains(r.last(), "nothing yet") {
		t.Fatalf("expected gear after a find, got %q", r.last())
	}
}

func TestBattleOnLevelUp(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	m.Tick() // both level up → each fights the other
	if !r.has("in combat and") {
		t.Fatalf("expected a battle on level-up, got %v", r.lines)
	}
}

func TestNoBattleWhenSolo(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Tick() // levels up, but there's no one to fight
	if r.has("in combat") {
		t.Fatal("a solo player should not battle")
	}
}

func TestGodsendAndCalamity(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 1000) // measurable %
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	m.godsend(ctx, p)
	if !r.has("godsend") {
		t.Fatalf("expected godsend, got %v", r.lines)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["ttl"] >= 1000 {
		t.Fatal("godsend should lower ttl")
	}

	st.HSet(ctx, sheetKey("net|alice"), "ttl", 1000)
	m.calamity(ctx, p) // no items → time penalty
	if !r.has("calamity") {
		t.Fatalf("expected calamity, got %v", r.lines)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["ttl"] <= 1000 {
		t.Fatal("calamity (no items) should raise ttl")
	}
}

func TestHandOfGod(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 1000)
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
	m.handOfGod(ctx, p)
	if !r.has("Hand of God") {
		t.Fatalf("expected hand of god, got %v", r.lines)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["ttl"] == 1000 {
		t.Fatal("hand of god should move the clock")
	}
}

func TestAlignAndClass(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg")) // enroll
	m.Handle(chanMsg("alice", "!rpg align evil"))
	if !r.has("is now evil") {
		t.Fatalf("align failed: %v", r.lines)
	}
	m.Handle(chanMsg("alice", "!rpg class wizard"))
	if !r.has("is now a wizard") {
		t.Fatalf("class failed: %v", r.lines)
	}
	m.Handle(chanMsg("alice", "!rpg")) // status shows both
	if !strings.Contains(r.last(), "the evil wizard") {
		t.Fatalf("status = %q; want 'the evil wizard'", r.last())
	}
}

func TestAlignRequiresEnroll(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg align good"))
	if !r.has("not playing") {
		t.Fatalf("expected not-playing, got %v", r.lines)
	}
}

func TestLeaderboard(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	m.Handle(chanMsg("anyone", "!rpg top"))
	if !r.has("alice") || !r.has("bob") {
		t.Fatalf("leaderboard should list players, got %q", r.last())
	}
}
