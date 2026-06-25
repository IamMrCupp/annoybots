package state

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

// runContract exercises the Store interface; run against every implementation.
func runContract(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	// counters
	if v, err := s.Incr(ctx, "c", 3); err != nil || v != 3 {
		t.Fatalf("Incr = %d, %v; want 3", v, err)
	}
	if v, _ := s.Incr(ctx, "c", 2); v != 5 {
		t.Fatalf("Incr cumulative = %d; want 5", v)
	}
	if v, _ := s.Get(ctx, "c"); v != 5 {
		t.Fatalf("Get = %d; want 5", v)
	}
	if v, _ := s.Get(ctx, "missing"); v != 0 {
		t.Fatalf("Get missing = %d; want 0", v)
	}

	// leaderboard (distinct scores so ordering is deterministic across backends)
	if v, err := s.ZIncr(ctx, "board", "alice", 1); err != nil || v != 1 {
		t.Fatalf("ZIncr = %d, %v; want 1", v, err)
	}
	s.ZIncr(ctx, "board", "bob", 3)
	s.ZIncr(ctx, "board", "bob", -1) // bob: 2
	s.ZIncr(ctx, "board", "carol", 3)

	if v, _ := s.ZScore(ctx, "board", "bob"); v != 2 {
		t.Fatalf("ZScore bob = %d; want 2", v)
	}
	if v, _ := s.ZScore(ctx, "board", "nobody"); v != 0 {
		t.Fatalf("ZScore missing = %d; want 0", v)
	}

	top, err := s.ZTop(ctx, "board", 2)
	if err != nil || len(top) != 2 {
		t.Fatalf("ZTop = %#v, %v; want 2 entries", top, err)
	}
	if top[0].Member != "carol" || top[0].Score != 3 || top[1].Member != "bob" || top[1].Score != 2 {
		t.Fatalf("ZTop ordering wrong: %#v", top)
	}

	// ZRem drops a member from the sorted set
	if err := s.ZRem(ctx, "board", "bob"); err != nil {
		t.Fatalf("ZRem: %v", err)
	}
	if v, _ := s.ZScore(ctx, "board", "bob"); v != 0 {
		t.Fatalf("after ZRem, ZScore bob = %d; want 0", v)
	}
	if all, _ := s.ZTop(ctx, "board", 10); len(all) != 2 {
		t.Fatalf("after ZRem, board has %d entries; want 2", len(all))
	}

	// hash (player sheet)
	if v, err := s.HIncr(ctx, "player:x", "level", 1); err != nil || v != 1 {
		t.Fatalf("HIncr = %d, %v; want 1", v, err)
	}
	if err := s.HSet(ctx, "player:x", "ttl", 600); err != nil {
		t.Fatalf("HSet: %v", err)
	}
	s.HIncr(ctx, "player:x", "ttl", -100) // ttl: 500
	sheet, err := s.HGetAll(ctx, "player:x")
	if err != nil || sheet["level"] != 1 || sheet["ttl"] != 500 {
		t.Fatalf("HGetAll = %#v, %v; want level 1 ttl 500", sheet, err)
	}
	if got, _ := s.HGetAll(ctx, "player:none"); len(got) != 0 {
		t.Fatalf("HGetAll missing = %#v; want empty", got)
	}

	// strings
	if err := s.SetStr(ctx, "id:x", "account-bob"); err != nil {
		t.Fatalf("SetStr: %v", err)
	}
	if v, _ := s.GetStr(ctx, "id:x"); v != "account-bob" {
		t.Fatalf("GetStr = %q; want account-bob", v)
	}
	if v, _ := s.GetStr(ctx, "id:none"); v != "" {
		t.Fatalf("GetStr missing = %q; want empty", v)
	}
	if err := s.Del(ctx, "id:x"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if v, _ := s.GetStr(ctx, "id:x"); v != "" {
		t.Fatalf("after Del, GetStr = %q; want empty", v)
	}
}

func TestMemStore(t *testing.T) {
	runContract(t, NewMem())
}

func TestRedisStore(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	s := NewRedis(mr.Addr(), "", "test:")
	defer s.Close()
	runContract(t, s)
}
