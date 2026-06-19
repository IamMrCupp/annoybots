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
	if a.Name != "alice" || a.Level != 5 || a.Power != 7 || a.Align != "evil" || a.Class != "wizard" {
		t.Fatalf("alice view wrong: %#v", a)
	}
	if a.Items["weapon"] != 7 {
		t.Fatalf("alice items wrong: %#v", a.Items)
	}
	if board[1].Name != "bob" {
		t.Fatalf("expected bob second, got %q", board[1].Name)
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
