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
	// Long quest interval so quests never start unless a test forces one.
	m := New(st, r, nil, time.Second, time.Second, time.Hour, time.Hour, log)
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

func TestLeavePenalties(t *testing.T) {
	cases := []struct {
		name    string
		ev      event.Event
		penalty int64
	}{
		{"part", event.Event{Kind: event.Part, Network: "net", Nick: "alice"}, partPenalty},
		{"quit", event.Event{Kind: event.Quit, Network: "net", Nick: "alice"}, quitPenalty},
		{"kick", event.Event{Kind: event.Kick, Network: "net", Nick: "alice"}, kickPenalty},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, st := newMgr()
			ctx := context.Background()
			m.Handle(chanMsg("alice", "!rpg"))
			st.HSet(ctx, sheetKey("net|alice"), "ttl", 1000)
			switch c.ev.Kind {
			case event.Part:
				m.OnPart(c.ev)
			case event.Quit:
				m.OnQuit(c.ev)
			case event.Kick:
				m.OnKick(c.ev)
			}
			if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["ttl"] != 1000+c.penalty {
				t.Fatalf("%s: ttl = %d; want %d", c.name, s["ttl"], 1000+c.penalty)
			}
		})
	}
}

func TestNickPenaltyAndFollow(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 1000)
	m.OnNick(event.Event{Kind: event.Nick, Network: "net", Nick: "alice", Text: "alice2"})
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["ttl"] != 1000+nickPenalty {
		t.Fatalf("nick penalty: ttl = %d; want %d", s["ttl"], 1000+nickPenalty)
	}
	if _, ok := m.onlinePlayer("net", "alice2"); !ok {
		t.Fatal("player should follow to the new nick")
	}
	if _, ok := m.onlinePlayer("net", "alice"); ok {
		t.Fatal("old nick should no longer be online")
	}
}

func TestPresenceSeedsOnline(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg")) // enroll alice
	m.OnLeave(event.Event{Network: "net", Nick: "alice"})
	m.Tick() // offline → no progress
	if r.has("attained level") {
		t.Fatal("alice should not progress while offline")
	}
	// A NAMES sweep re-seeds her as present without a rejoin or a message.
	m.OnPresent(event.Event{Kind: event.Present, Network: "net", Channel: "#chan", Nick: "alice"})
	m.Tick() // now online → levels up
	if !r.has("attained level 1") {
		t.Fatalf("presence-seeded idler should resume progress, got %v", r.lines)
	}
}

func TestPresenceIgnoresUnenrolled(t *testing.T) {
	m, _, _ := newMgr()
	// nobody enrolled; a NAMES sweep must not online a non-player.
	m.OnPresent(event.Event{Kind: event.Present, Network: "net", Channel: "#chan", Nick: "stranger"})
	if _, ok := m.onlinePlayer("net", "stranger"); ok {
		t.Fatal("a non-enrolled nick must not be marked online by a NAMES seed")
	}
}

// enrollOnline enrolls a player and leaves them online (via the !rpg path).
func enrollOnline(m *Manager, nick string) { m.Handle(chanMsg(nick, "!rpg")) }

func TestQuestStartAndComplete(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	base := time.Unix(1000, 0)
	m.now = func() time.Time { return base }
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")

	m.startQuest(ctx)
	if m.quest == nil {
		t.Fatal("a quest should have started with two idlers online")
	}
	if !r.has("a quest begins") {
		t.Fatalf("expected quest announcement, got %v", r.lines)
	}
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 1000)
	st.HSet(ctx, sheetKey("net|bob"), "ttl", 1000)

	// Roll the clock past the deadline; the tick completes the quest.
	m.now = func() time.Time { return base.Add(2 * time.Hour) }
	m.questTick(ctx)
	if m.quest != nil {
		t.Fatal("quest should be cleared after completion")
	}
	if !r.has("quest is complete") {
		t.Fatalf("expected completion announcement, got %v", r.lines)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["ttl"] >= 1000 {
		t.Fatalf("completing a quest should lower ttl, got %d", s["ttl"])
	}
}

func TestQuestFailsOnTalk(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")
	m.startQuest(ctx)
	st.HSet(ctx, sheetKey("net|bob"), "ttl", 1000)

	if m.Handle(chanMsg("alice", "oops I talked")) {
		t.Fatal("chatter is not a consumed command")
	}
	if m.quest != nil {
		t.Fatal("talking during a quest must ruin it")
	}
	if !r.has("quest is RUINED") {
		t.Fatalf("expected ruin announcement, got %v", r.lines)
	}
	// The silent partner still eats the party-wide penalty.
	if s, _ := st.HGetAll(ctx, sheetKey("net|bob")); s["ttl"] <= 1000 {
		t.Fatalf("a ruined quest should push the whole party back, bob ttl=%d", s["ttl"])
	}
}

func TestQuestFailsOnPart(t *testing.T) {
	m, _, _ := newMgr()
	ctx := context.Background()
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")
	m.startQuest(ctx)
	m.OnPart(event.Event{Kind: event.Part, Network: "net", Nick: "alice"})
	if m.quest != nil {
		t.Fatal("a quester parting must ruin the quest")
	}
}

func TestQuestNeedsAParty(t *testing.T) {
	m, _, _ := newMgr()
	enrollOnline(m, "alice") // only one idler online
	m.startQuest(context.Background())
	if m.quest != nil {
		t.Fatal("a quest should not start with fewer than two idlers")
	}
}

func TestQuestTickStartsOnOdds(t *testing.T) {
	m, _, _ := newMgr()
	m.questInterval = m.interval // denom == 1 → a start is attempted every tick
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")
	m.questTick(context.Background())
	if m.quest == nil {
		t.Fatal("questTick should start a quest when the odds guarantee it")
	}
}

func TestQuestRehydratesAcrossRestart(t *testing.T) {
	m1, _, st := newMgr()
	ctx := context.Background()
	enrollOnline(m1, "alice")
	enrollOnline(m1, "bob")
	m1.startQuest(ctx)
	if m1.quest == nil {
		t.Fatal("setup: quest should be active")
	}
	// A fresh manager on the same store must recover the in-flight quest.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m2 := New(st, &recorder{}, nil, time.Second, time.Second, time.Hour, time.Hour, log)
	if m2.quest == nil {
		t.Fatal("quest should rehydrate from the store after a restart")
	}
	if len(m2.quest.Members) != 2 {
		t.Fatalf("recovered party size = %d; want 2", len(m2.quest.Members))
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
