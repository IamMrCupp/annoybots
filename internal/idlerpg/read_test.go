package idlerpg

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/state"
)

func TestReadLeaderboard(t *testing.T) {
	st := state.NewMem()
	ctx := context.Background()
	// alice: level 5, evil wizard, a weapon. bob: level 2, bare.
	st.HSet(ctx, sheetKey("net|alice"), "level", 5)
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 120)
	st.HSet(ctx, sheetKey("net|alice"), "align", 2) // evil
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 7)
	st.SetStr(ctx, classKey("net|alice"), "wizard")
	st.ZIncr(ctx, boardKey(), "net|alice", 5)
	st.HSet(ctx, sheetKey("net|bob"), "level", 2)
	st.ZIncr(ctx, boardKey(), "net|bob", 2)

	board, err := ReadLeaderboard(ctx, st, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(board) != 2 {
		t.Fatalf("board size = %d; want 2", len(board))
	}
	a := board[0]
	if a.Name != "alice" || a.Level != 5 || a.Power != 7 || a.Align != "neutral evil" || a.AlignClass != "evil" || a.Class != "wizard" {
		t.Fatalf("alice view wrong: %#v", a)
	}
	if len(a.Items) != 1 || a.Items[0].Slot != "weapon" || a.Items[0].Level != 7 {
		t.Fatalf("alice items wrong: %#v", a.Items)
	}
	if board[1].Name != "bob" {
		t.Fatalf("expected bob second, got %q", board[1].Name)
	}
}

func TestReadWorld(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Tick() // places alice on the map

	w, err := ReadWorld(ctx, st, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Towns) == 0 || w.Size != worldSize {
		t.Fatalf("world should carry towns + size, got %d towns size %d", len(w.Towns), w.Size)
	}
	found := false
	for _, p := range w.Players {
		if p.Name == "alice" {
			found = true
		}
	}
	if !found {
		t.Fatalf("alice should be on the map, got %+v", w.Players)
	}
}

func TestReadChar(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "pots", 3)
	st.HSet(ctx, sheetKey("net|alice"), "duelw", 5)
	st.HSet(ctx, sheetKey("net|alice"), "level", 60) // earns a title

	cv, ok := ReadChar(ctx, st, "net|alice")
	if !ok || cv.Name != "alice" {
		t.Fatalf("expected alice's char view, got %#v ok=%v", cv, ok)
	}
	if cv.Draughts != 3 || cv.DuelWins != 5 {
		t.Fatalf("char view stats: draughts=%d duelwins=%d; want 3/5", cv.Draughts, cv.DuelWins)
	}
	if cv.Title == "" {
		t.Fatal("a level-60 character should carry a title")
	}
	if _, ok := ReadChar(ctx, st, "net|ghost"); ok {
		t.Fatal("a non-enrolled key must not resolve to a character")
	}
	if len(cv.Abilities) != 6 || cv.Abilities[0].Name != "STR" {
		t.Fatalf("char view should carry the six abilities in order, got %+v", cv.Abilities)
	}
}

func TestReadQuestMap(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	m.startMapQuest(ctx, m.draftParty())

	q, err := ReadQuest(ctx, st)
	if err != nil || q == nil {
		t.Fatalf("expected a quest view, got %v (err %v)", q, err)
	}
	if q.Kind != "map" || q.MapSize != mapSize {
		t.Fatalf("map quest view wrong: kind=%q mapSize=%d", q.Kind, q.MapSize)
	}
	for _, v := range []int{q.X, q.Y, q.X1, q.Y1, q.X2, q.Y2} {
		if v < 0 || v > mapSize {
			t.Fatalf("coordinate out of bounds: %#v", q)
		}
	}
}

func TestReadQuest(t *testing.T) {
	st := state.NewMem()
	ctx := context.Background()

	if q, _ := ReadQuest(ctx, st); q != nil {
		t.Fatal("no quest stored → ReadQuest should return nil")
	}

	blob, _ := json.Marshal(quest{
		Kind: "time", Network: "net", Channel: "#c", Deadline: 12345,
		Desc: "retrieve the thing", Members: map[string]string{"net|bob": "bob", "net|alice": "alice"},
	})
	st.SetStr(ctx, questKey(), string(blob))

	q, err := ReadQuest(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if q == nil || q.Desc != "retrieve the thing" || q.Deadline != 12345 {
		t.Fatalf("quest view wrong: %#v", q)
	}
	if len(q.Members) != 2 || q.Members[0] != "alice" || q.Members[1] != "bob" {
		t.Fatalf("members should be sorted [alice bob], got %v", q.Members)
	}
}

func TestReadRanking(t *testing.T) {
	st := state.NewMem()
	ctx := context.Background()
	// alice: lvl 5, 300 kills, 900 gold; bob: lvl 8, 100 kills, 5000 gold
	st.HSet(ctx, sheetKey("net|alice"), "level", 5)
	st.HSet(ctx, sheetKey("net|alice"), "kills", 300)
	st.HSet(ctx, sheetKey("net|alice"), "gold", 900)
	st.ZIncr(ctx, boardKey(), "net|alice", 5)
	st.HSet(ctx, sheetKey("net|bob"), "level", 8)
	st.HSet(ctx, sheetKey("net|bob"), "kills", 100)
	st.HSet(ctx, sheetKey("net|bob"), "gold", 5000)
	st.ZIncr(ctx, boardKey(), "net|bob", 8)

	byLevel, _ := ReadRanking(ctx, st, "level", 10)
	if len(byLevel) != 2 || byLevel[0].Name != "bob" {
		t.Fatalf("level ranking wrong: %+v", byLevel)
	}
	byKills, _ := ReadRanking(ctx, st, "kills", 10)
	if byKills[0].Name != "alice" || byKills[0].Value != 300 {
		t.Fatalf("kills ranking wrong: %+v", byKills)
	}
	byGold, _ := ReadRanking(ctx, st, "gold", 10)
	if byGold[0].Name != "bob" || byGold[0].Value != 5000 {
		t.Fatalf("gold ranking wrong: %+v", byGold)
	}
}
