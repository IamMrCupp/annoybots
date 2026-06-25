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
	if !r.has("found a") {
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
	if !r.has("is now neutral evil") {
		t.Fatalf("align failed: %v", r.lines)
	}
	m.Handle(chanMsg("alice", "!rpg class wizard"))
	if !r.has("is now a wizard") {
		t.Fatalf("class failed: %v", r.lines)
	}
	m.Handle(chanMsg("alice", "!rpg")) // status shows both
	if !strings.Contains(r.last(), "the neutral evil wizard") {
		t.Fatalf("status = %q; want 'the neutral evil wizard'", r.last())
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

func TestMapQuestTravelsAndCompletes(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")
	m.startMapQuest(ctx, m.draftParty())
	if m.quest == nil || m.quest.Kind != "map" {
		t.Fatalf("a map quest should be active, got %#v", m.quest)
	}
	if !r.has("a quest begins") {
		t.Fatalf("expected a map-quest announcement, got %v", r.lines)
	}
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 1000)
	st.HSet(ctx, sheetKey("net|bob"), "ttl", 1000)

	// Two legs across a 500-grid at 70/tick finish well within 100 ticks.
	sawWaypoint := false
	for i := 0; i < 100 && m.quest != nil; i++ {
		m.advanceMapQuest(ctx)
		if r.has("first waypoint") {
			sawWaypoint = true
		}
	}
	if m.quest != nil {
		t.Fatal("a map quest must complete within a bounded number of ticks")
	}
	if !sawWaypoint {
		t.Fatal("the party should announce reaching the first waypoint")
	}
	if !r.has("reached its destination") {
		t.Fatalf("expected an arrival announcement, got %v", r.lines)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["ttl"] >= 1000 {
		t.Fatalf("finishing should lower ttl, got %d", s["ttl"])
	}
}

func TestMapQuestFailsOnTalk(t *testing.T) {
	m, r, _ := newMgr()
	ctx := context.Background()
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")
	m.startMapQuest(ctx, m.draftParty())
	if m.Handle(chanMsg("alice", "are we there yet")) {
		t.Fatal("chatter is not a consumed command")
	}
	if m.quest != nil {
		t.Fatal("talking mid-journey must ruin the map quest")
	}
	if !r.has("RUINED") {
		t.Fatalf("expected a ruin announcement, got %v", r.lines)
	}
}

func TestStepTowardArrives(t *testing.T) {
	// A single step larger than the distance snaps onto the target.
	if x, y, reached := stepToward(0, 0, 30, 40, 100); !reached || x != 30 || y != 40 {
		t.Fatalf("expected arrival at (30,40), got (%d,%d) reached=%v", x, y, reached)
	}
	// A step short of the target moves toward it without arriving.
	if x, y, reached := stepToward(0, 0, 0, 100, 40); reached || y != 40 || x != 0 {
		t.Fatalf("expected (0,40) not reached, got (%d,%d) reached=%v", x, y, reached)
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

func TestInfoCommand(t *testing.T) {
	m, r, _ := newMgr()
	enrollOnline(m, "alice")
	m.Handle(chanMsg("alice", "!rpg info"))
	if !strings.Contains(r.last(), "idling now") || !strings.Contains(r.last(), "no quest") {
		t.Fatalf("info should summarize the realm, got %q", r.last())
	}
	m.startQuest(context.Background()) // needs 2 — won't start with one
	enrollOnline(m, "bob")             // now two online
	m.startQuest(context.Background()) // start a real quest
	m.Handle(chanMsg("bob", "!rpg info"))
	if !strings.Contains(r.last(), "quest is underway") {
		t.Fatalf("info should report the active quest, got %q", r.last())
	}
}

func TestQuestStatusCommand(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg quest"))
	if !strings.Contains(r.last(), "no quest underway") {
		t.Fatalf("expected no-quest reply, got %q", r.last())
	}
	enrollOnline(m, "alice")
	enrollOnline(m, "bob")
	m.startQuest(context.Background())
	m.Handle(chanMsg("alice", "!rpg quest"))
	if !strings.Contains(r.last(), "quest in progress") {
		t.Fatalf("expected active-quest detail, got %q", r.last())
	}
}

func TestStatusCommand(t *testing.T) {
	m, r, _ := newMgr()
	enrollOnline(m, "alice")
	// self
	m.Handle(chanMsg("alice", "!rpg status"))
	if !strings.Contains(r.last(), "alice the") || !strings.Contains(r.last(), "power 0") {
		t.Fatalf("self status wrong: %q", r.last())
	}
	// a named other
	m.Handle(chanMsg("bob", "!rpg status alice"))
	if !strings.Contains(r.last(), "alice the") {
		t.Fatalf("named status wrong: %q", r.last())
	}
	// an unknown player
	m.Handle(chanMsg("bob", "!rpg status ghost"))
	if !strings.Contains(r.last(), "isn't playing") {
		t.Fatalf("unknown status should say not playing, got %q", r.last())
	}
}

func TestMaxHP(t *testing.T) {
	sheet := map[string]int64{"level": 2, "con": 14} // CON 14 → +2
	if got := maxHP(sheet, "fighter"); got != 32 {   // d10: (5+1+2)*3 + 8
		t.Fatalf("fighter L2 CON14 maxHP = %d; want 32", got)
	}
	if got := maxHP(sheet, ""); got != 29 { // unclassed d8: (4+1+2)*3 + 8
		t.Fatalf("unclassed L2 CON14 maxHP = %d; want 29", got)
	}
}

func TestDamageDownedAndHeal(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.damage(ctx, "net|alice", 1000)
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); !isDowned(s, "") {
		t.Fatal("massive damage should leave the character downed")
	}
	m.heal(ctx, "net|alice", 1000)
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if isDowned(s, "") || s["dmg"] != 0 {
		t.Fatalf("a full heal should clear downed and clamp dmg to 0, got dmg=%d", s["dmg"])
	}
}

func TestDownedFreezesProgress(t *testing.T) {
	m, r, st := newMgr() // 1s tick/ttl: a healthy player would level
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.damage(ctx, "net|alice", 1000) // down alice
	m.Tick()
	if r.has("attained level") {
		t.Fatal("a downed player must not level up")
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["level"] != 0 {
		t.Fatalf("downed level = %d; want 0", s["level"])
	}
}

func TestClassMustBeCanonical(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg class paladin")) // not in the canonical set
	if !r.has("no such class") {
		t.Fatalf("a non-canonical class should be rejected, got %q", r.last())
	}
	m.Handle(chanMsg("alice", "!rpg class fighter"))
	if !r.has("is now a fighter") {
		t.Fatalf("fighter should be accepted, got %q", r.last())
	}
}

func TestClassAttackMod(t *testing.T) {
	sheet := map[string]int64{"str": 16, "dex": 8, "int": 10}
	if got := classAttackMod(sheet, "fighter"); got != 3 { // STR 16 → +3
		t.Fatalf("fighter STR16 attack mod = %d; want 3", got)
	}
	if got := classAttackMod(sheet, "rogue"); got != -1 { // DEX 8 → -1
		t.Fatalf("rogue DEX8 attack mod = %d; want -1", got)
	}
	if got := classAttackMod(sheet, "wanderer"); got != 0 { // legacy/unknown → 0
		t.Fatalf("unknown class attack mod = %d; want 0", got)
	}
}

func TestAbilityScoresRolledOnEnroll(t *testing.T) {
	m, r, st := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	sheet, _ := st.HGetAll(context.Background(), sheetKey("net|alice"))
	for _, a := range abilities {
		if sheet[a] < 3 || sheet[a] > 18 {
			t.Fatalf("ability %s = %d, out of 3–18", a, sheet[a])
		}
	}
	if !r.has("roll for stats") {
		t.Fatalf("enroll should mention stats, got %v", r.lines)
	}
}

func TestAbilityMod(t *testing.T) {
	for score, want := range map[int64]int64{3: -4, 7: -2, 8: -1, 9: -1, 10: 0, 11: 0, 12: 1, 14: 2, 18: 4} {
		if got := abilityMod(score); got != want {
			t.Errorf("abilityMod(%d) = %d; want %d", score, got, want)
		}
	}
}

func TestSheetCommand(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg sheet"))
	if !strings.Contains(r.last(), "STR") || !strings.Contains(r.last(), "CHA") || !strings.Contains(r.last(), "HP") {
		t.Fatalf("!rpg sheet should show HP + the ability block, got %q", r.last())
	}
}

func TestSheetLazyRollsLegacyChar(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	// A character enrolled before abilities existed (level set, no scores).
	st.HSet(ctx, sheetKey("net|bob"), "level", 5)
	st.ZIncr(ctx, boardKey(), "net|bob", 5)
	m.Handle(chanMsg("bob", "!rpg sheet"))
	if s, _ := st.HGetAll(ctx, sheetKey("net|bob")); s["str"] < 3 {
		t.Fatalf("a legacy character should roll abilities on sheet view, str=%d", s["str"])
	}
}

func TestWorldMapMovement(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg")) // enroll + online
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["mx"] != 0 {
		t.Fatal("a fresh player should start unplaced (mx=0)")
	}
	m.Tick() // moves (places) alice
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if s["mx"] < 1 || s["mx"] > worldSize || s["my"] < 1 || s["my"] > worldSize {
		t.Fatalf("alice should be placed in-bounds, got (%d,%d)", s["mx"], s["my"])
	}
}

func allowAll(engine.Message) bool { return true }

func TestAdminVerbsRequireAuthz(t *testing.T) {
	m, r, _ := newMgr() // no authz wired
	m.Handle(chanMsg("alice", "!rpg pause"))
	if !r.has("do not heed") {
		t.Fatalf("an unauthorized admin verb should be refused, got %v", r.lines)
	}
	if m.paused.Load() {
		t.Fatal("an unauthorized !rpg pause must not pause the game")
	}
}

func TestAdminPauseResume(t *testing.T) {
	m, r, _ := newMgr()
	m.SetAuthz(allowAll)
	m.Handle(chanMsg("alice", "!rpg")) // enroll, online, ttl=1
	m.Handle(chanMsg("boss", "!rpg pause"))
	if !m.paused.Load() {
		t.Fatal("!rpg pause should freeze the game")
	}
	m.Tick() // paused → no progress
	if r.has("attained level") {
		t.Fatal("a paused game must not level anyone up")
	}
	m.Handle(chanMsg("boss", "!rpg resume"))
	m.Tick() // ttl=1 → levels up
	if !r.has("attained level 1") {
		t.Fatalf("a resumed game should tick again, got %v", r.lines)
	}
}

func TestAdminPush(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.SetAuthz(allowAll)
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 1000)
	m.Handle(chanMsg("boss", "!rpg push alice 500"))
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["ttl"] != 1500 {
		t.Fatalf("push should move the clock, ttl=%d want 1500", s["ttl"])
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

func TestPickMonsterRespectsLevel(t *testing.T) {
	m, _, _ := newMgr()
	if mon := m.pickMonster(0, ""); mon.MinLvl > 0 {
		t.Fatalf("level 0 picked %q (MinLvl %d)", mon.Name, mon.MinLvl)
	}
	for i := 0; i < 30; i++ {
		if mon := m.pickMonster(5, ""); mon.MinLvl > 5 {
			t.Fatalf("level 5 picked too-strong %q (MinLvl %d)", mon.Name, mon.MinLvl)
		}
	}
}

func TestResolveFightWinRewards(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	// A level-20 fighter with STR/DEX 18 always hits a giant rat (AC 10) and one
	// hit (d8+4 ≥ 5) kills its 4 HP — a deterministic win regardless of seed.
	for _, f := range []string{"str", "dex", "con"} {
		st.HSet(ctx, sheetKey("net|alice"), f, 18)
	}
	st.HSet(ctx, sheetKey("net|alice"), "level", 20)
	st.SetStr(ctx, classKey("net|alice"), "fighter")
	sheet, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	m.resolveFight(ctx, p, sheet, "fighter", bestiary[0]) // a giant rat
	if !r.has("slew") {
		t.Fatalf("a juggernaut should slay a giant rat, got %v", r.lines)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["kills"] != 1 || s["gold"] < 1 {
		t.Fatalf("a kill should bump kills + gold, got kills=%d gold=%d", s["kills"], s["gold"])
	}
}

func TestRaceBakesModifiers(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "str", 10)
	st.HSet(ctx, sheetKey("net|alice"), "con", 10)
	m.Handle(chanMsg("alice", "!rpg race half-orc")) // +2 STR, +1 CON
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if s["str"] != 12 || s["con"] != 11 {
		t.Fatalf("half-orc should bake +2 STR +1 CON, got STR %d CON %d", s["str"], s["con"])
	}
	if !r.has("is now a half-orc") {
		t.Fatalf("race announcement wrong: %v", r.lines)
	}
}

func TestRaceSetOnce(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg race elf"))
	m.Handle(chanMsg("alice", "!rpg race dwarf")) // can't change heritage
	if !r.has("already set") {
		t.Fatalf("a second race should be rejected, got %q", r.last())
	}
}

func TestRaceInTitle(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg race elf"))
	m.Handle(chanMsg("alice", "!rpg")) // status line
	if !strings.Contains(r.last(), "elf") {
		t.Fatalf("status should show race, got %q", r.last())
	}
}

func TestRaceRejectsUnknown(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg race wookiee"))
	if !r.has("no such race") {
		t.Fatalf("unknown race should be rejected, got %q", r.last())
	}
}

func TestItemSumRarity(t *testing.T) {
	sheet := map[string]int64{
		itemField("weapon"): 10, rarityField("weapon"): 2, // rare → 170%
		itemField("helm"): 5, // common (unset rarity → 100%)
	}
	if got := itemSum(sheet); got != 22 { // 10*1.7=17 + 5
		t.Fatalf("itemSum with rarity = %d; want 22", got)
	}
}

func TestPickRarityInRange(t *testing.T) {
	m, _, _ := newMgr()
	for lvl := 0; lvl < 100; lvl++ {
		if idx := m.pickRarity(int64(lvl)); idx < 0 || idx >= len(rarities) {
			t.Fatalf("pickRarity(%d) = %d, out of range", lvl, idx)
		}
	}
}

func TestItemsShowRarityAndName(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 9)
	st.HSet(ctx, sheetKey("net|alice"), rarityField("weapon"), 4) // legendary
	st.SetStr(ctx, nameKey("net|alice", "weapon"), "Flametongue")
	m.Handle(chanMsg("alice", "!rpg items"))
	if !strings.Contains(r.last(), "legendary") || !strings.Contains(r.last(), "Flametongue") {
		t.Fatalf("items should show rarity + name, got %q", r.last())
	}
}

func TestRestAtInnHeals(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "mx", 420) // Mount AFK (inn)
	st.HSet(ctx, sheetKey("net|alice"), "my", 90)
	st.HSet(ctx, sheetKey("net|alice"), "dmg", 50)
	m.Handle(chanMsg("alice", "!rpg rest"))
	if !r.has("recovers to full") {
		t.Fatalf("rest at an inn should heal, got %q", r.last())
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["dmg"] != 0 {
		t.Fatalf("dmg should be cleared, got %d", s["dmg"])
	}
}

func TestServiceWrongTown(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "mx", 70) // Lurk Harbor (market)
	st.HSet(ctx, sheetKey("net|alice"), "my", 410)
	m.Handle(chanMsg("alice", "!rpg rest")) // no inn here
	if !r.has("no inn here") {
		t.Fatalf("rest at a market should be refused, got %q", r.last())
	}
}

func TestBuyAtMarket(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "mx", 70) // Lurk Harbor (market)
	st.HSet(ctx, sheetKey("net|alice"), "my", 410)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 1000)
	st.HSet(ctx, sheetKey("net|alice"), "level", 5)
	m.Handle(chanMsg("alice", "!rpg buy weapon"))
	if !r.has("buys a level-6 weapon") {
		t.Fatalf("buy wrong, got %q", r.last())
	}
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if s["gold"] != 952 || s[itemField("weapon")] != 6 { // 1000 - (5+1)*8
		t.Fatalf("buy should spend gold + set item, gold=%d weapon=%d", s["gold"], s[itemField("weapon")])
	}
}

func TestReviveAtTemple(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "mx", 250) // Idlecrest (temple)
	st.HSet(ctx, sheetKey("net|alice"), "my", 250)
	st.HSet(ctx, sheetKey("net|alice"), "dmg", 1000) // downed
	st.HSet(ctx, sheetKey("net|alice"), "gold", 1000)
	st.HSet(ctx, sheetKey("net|alice"), "level", 3)
	m.Handle(chanMsg("alice", "!rpg revive"))
	if !r.has("revives") {
		t.Fatalf("revive wrong, got %q", r.last())
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["dmg"] != 0 || s["gold"] != 973 { // 1000-(15+12)
		t.Fatalf("revive should clear dmg + spend gold, dmg=%d gold=%d", s["dmg"], s["gold"])
	}
}

func TestTravelAndArrive(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "mx", 400)
	st.HSet(ctx, sheetKey("net|alice"), "my", 400)
	m.Handle(chanMsg("alice", "!rpg travel Idlecrest"))
	if !r.has("sets out for Idlecrest") {
		t.Fatalf("travel should set out, got %q", r.last())
	}
	for i := 0; i < 80 && !r.has("arrives at Idlecrest"); i++ {
		m.Tick()
	}
	if !r.has("arrives at Idlecrest") {
		t.Fatalf("traveller should reach Idlecrest, lines: %v", r.lines)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["dest"] != 0 {
		t.Fatalf("dest should clear on arrival, got %d", s["dest"])
	}
}

func TestClassCombatMods(t *testing.T) {
	sheet := map[string]int64{"int": 16, "dex": 14, "wis": 12, "str": 10}
	if cm := classCombat("fighter", sheet); cm.extraAttacks != 1 || cm.ability != "Extra Attack" {
		t.Fatalf("fighter: %#v", cm)
	}
	if cm := classCombat("wizard", sheet); cm.autoDmg != 5 { // 2 + INT16 mod (+3)
		t.Fatalf("wizard autoDmg = %d; want 5", cm.autoDmg)
	}
	if cm := classCombat("rogue", sheet); cm.bonusOnHit != 5 { // 3 + DEX14 mod (+2)
		t.Fatalf("rogue bonusOnHit = %d; want 5", cm.bonusOnHit)
	}
	if cm := classCombat("ranger", sheet); cm.bonusOnHit != 4 { // 2 + DEX14 (+2)
		t.Fatalf("ranger bonusOnHit = %d; want 4", cm.bonusOnHit)
	}
	if cm := classCombat("cleric", sheet); cm.selfHeal != 2 { // 1 + WIS12 (+1)
		t.Fatalf("cleric selfHeal = %d; want 2", cm.selfHeal)
	}
	if cm := classCombat("bard", sheet); cm.negateChance != 3 {
		t.Fatalf("bard negateChance = %d; want 3", cm.negateChance)
	}
	if cm := classCombat("", sheet); cm.ability != "" { // unclassed → nothing
		t.Fatalf("unclassed should have no ability: %#v", cm)
	}
}

func TestClassAbilityInClassMessage(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg class rogue"))
	if !strings.Contains(r.last(), "Sneak Attack") {
		t.Fatalf("class message should name the ability, got %q", r.last())
	}
}

func TestResolveFightEveryClassRuns(t *testing.T) {
	// Smoke test: a fight resolves cleanly for each class (no panic, an announce).
	for class := range classes {
		m, r, st := newMgr()
		ctx := context.Background()
		m.Handle(chanMsg("alice", "!rpg"))
		st.SetStr(ctx, classKey("net|alice"), class)
		sheet, _ := st.HGetAll(ctx, sheetKey("net|alice"))
		p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
		m.resolveFight(ctx, p, sheet, class, bestiary[0])
		if r.last() == "" {
			t.Fatalf("class %q: fight produced no announcement", class)
		}
	}
}

func TestAlignmentGrid(t *testing.T) {
	if got := fullAlign(0, 0); got != "true neutral" {
		t.Fatalf("fullAlign(0,0) = %q; want 'true neutral'", got)
	}
	if got := fullAlign(1, 1); got != "lawful good" {
		t.Fatalf("fullAlign(1,1) = %q; want 'lawful good'", got)
	}
	if got := fullAlign(2, 2); got != "chaotic evil" {
		t.Fatalf("fullAlign(2,2) = %q; want 'chaotic evil'", got)
	}
	if got := fullAlign(0, 1); got != "neutral good" {
		t.Fatalf("fullAlign(0,1) = %q; want 'neutral good'", got)
	}
}

func TestSetAlignTwoAxes(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg align chaotic evil"))
	if !r.has("is now chaotic evil") {
		t.Fatalf("two-axis align failed: %q", r.last())
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["law"] != 2 || s["align"] != 2 {
		t.Fatalf("law/align = %d/%d; want 2/2", s["law"], s["align"])
	}
	// single ethical word adjusts just that axis
	m.Handle(chanMsg("alice", "!rpg align lawful"))
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["law"] != 1 || s["align"] != 2 {
		t.Fatalf("after 'lawful': law/align = %d/%d; want 1/2", s["law"], s["align"])
	}
}

func TestMagicNames(t *testing.T) {
	m, _, _ := newMgr()
	// legendary weapon: full title with an epithet + a weapon-pool noun.
	leg := m.magicName("weapon", true)
	if !strings.Contains(leg, " of ") {
		t.Fatalf("legendary name should have an epithet: %q", leg)
	}
	hasNoun := func(name string, nouns []string) bool {
		for _, n := range nouns {
			if strings.Contains(name, n) {
				return true
			}
		}
		return false
	}
	if !hasNoun(leg, slotNouns["weapon"]) {
		t.Fatalf("weapon name should use a weapon noun: %q", leg)
	}
	// epic boots: two words, no epithet, a boots-pool noun.
	ep := m.magicName("boots", false)
	if strings.Contains(ep, " of ") || !hasNoun(ep, slotNouns["boots"]) {
		t.Fatalf("epic boots name wrong: %q", ep)
	}
	// different slots can't collide (disjoint noun pools).
	for i := 0; i < 30; i++ {
		if m.magicName("weapon", true) == m.magicName("boots", true) {
			t.Fatal("weapon and boots names should never be equal")
		}
	}
	// variety: not all the same.
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		seen[m.magicName("ring", true)] = true
	}
	if len(seen) < 5 {
		t.Fatalf("expected varied names, got only %d distinct in 20", len(seen))
	}
}

func TestDMResetChar(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.SetAuthz(allowAll)
	m.Handle(chanMsg("alice", "!rpg")) // enroll → board + sheet
	m.Handle(chanMsg("boss", "!rpg reset alice"))
	if !r.has("has been erased") {
		t.Fatalf("reset reply: %q", r.last())
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); len(s) != 0 {
		t.Fatal("the sheet should be gone after reset")
	}
	if score, _ := st.ZScore(ctx, boardKey(), "net|alice"); score != 0 {
		t.Fatal("the character should be off the leaderboard")
	}
}

func TestDMResetRequiresAuthz(t *testing.T) {
	m, r, _ := newMgr() // no authz
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("alice", "!rpg reset alice"))
	if !r.has("do not heed") {
		t.Fatalf("an unauthorized reset must be refused, got %q", r.last())
	}
}

func TestDMResetAllNeedsConfirm(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.SetAuthz(allowAll)
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	m.Handle(chanMsg("boss", "!rpg reset all")) // no confirmation
	if !r.has("confirm with") {
		t.Fatalf("reset all should require confirmation, got %q", r.last())
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); len(s) == 0 {
		t.Fatal("reset all must not wipe without confirmation")
	}
	m.Handle(chanMsg("boss", "!rpg reset all yes"))
	if !r.has("wiped clean") {
		t.Fatalf("confirmed wipe reply: %q", r.last())
	}
	if top, _ := st.ZTop(ctx, boardKey(), 10); len(top) != 0 {
		t.Fatalf("the board should be empty after a full wipe, got %d", len(top))
	}
}

func TestDMSetLevelAndGold(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.SetAuthz(allowAll)
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("boss", "!rpg setlevel alice 40"))
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["level"] != 40 {
		t.Fatalf("setlevel: level = %d; want 40", s["level"])
	}
	if score, _ := st.ZScore(ctx, boardKey(), "net|alice"); score != 40 {
		t.Fatalf("setlevel should sync the board, score = %d; want 40", score)
	}
	m.Handle(chanMsg("boss", "!rpg gold alice 500"))
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["gold"] != 500 {
		t.Fatalf("gold = %d; want 500", s["gold"])
	}
	m.Handle(chanMsg("boss", "!rpg gold alice -1000")) // clamps at 0
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["gold"] != 0 {
		t.Fatalf("gold should clamp to 0, got %d", s["gold"])
	}
}
