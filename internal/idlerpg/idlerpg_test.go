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

func TestLeaderboard(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	m.Handle(chanMsg("anyone", "!rpg top"))
	if !r.has("alice") || !r.has("bob") {
		t.Fatalf("leaderboard should list players, got %q", r.last())
	}
}
