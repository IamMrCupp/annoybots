package idlerpg

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/state"
)

// This file exposes a read-only projection of the game state for out-of-process
// readers (the web dashboard). It deliberately lives in the idlerpg package so
// the Redis key schema (sheetKey/boardKey/classKey/itemField) stays in one place
// — a consumer never reconstructs raw keys.

// CharView is a read-only snapshot of one character's sheet.
type CharView struct {
	Key   string           // canonical character key (network|nick, or a linked account)
	Name  string           // display name: the key with any "network|" prefix stripped
	Level int64            // current level
	TTL   int64            // seconds to the next level
	Power int64            // total equipment power (sum of item levels)
	Align string           // "good" / "neutral" / "evil"
	Class string           // free-text class, empty if unset
	Items map[string]int64 // equipped slots → level (only non-empty slots)
}

// QuestView is a read-only snapshot of the active quest.
type QuestView struct {
	Desc     string   // the objective flavor text
	Members  []string // party display nicks, sorted
	Deadline int64    // unix seconds the quest resolves
}

// ReadLeaderboard returns up to n characters ranked by level (highest first).
func ReadLeaderboard(ctx context.Context, store state.Store, n int) ([]CharView, error) {
	top, err := store.ZTop(ctx, boardKey(), n)
	if err != nil {
		return nil, err
	}
	out := make([]CharView, 0, len(top))
	for _, e := range top {
		out = append(out, readChar(ctx, store, e.Member))
	}
	return out, nil
}

func readChar(ctx context.Context, store state.Store, key string) CharView {
	sheet, _ := store.HGetAll(ctx, sheetKey(key))
	class, _ := store.GetStr(ctx, classKey(key))
	items := map[string]int64{}
	for _, s := range itemSlots {
		if v := sheet[itemField(s)]; v > 0 {
			items[s] = v
		}
	}
	return CharView{
		Key:   key,
		Name:  displayName(key),
		Level: sheet["level"],
		TTL:   sheet["ttl"],
		Power: itemSum(sheet),
		Align: alignName(sheet["align"]),
		Class: class,
		Items: items,
	}
}

// displayName strips a leading "network|" from a character key for presentation.
func displayName(key string) string {
	if i := strings.IndexByte(key, '|'); i >= 0 {
		return key[i+1:]
	}
	return key
}

// ReadQuest returns the active quest, or nil when none is running.
func ReadQuest(ctx context.Context, store state.Store) (*QuestView, error) {
	blob, err := store.GetStr(ctx, questKey())
	if err != nil {
		return nil, err
	}
	if blob == "" {
		return nil, nil
	}
	var q quest
	if json.Unmarshal([]byte(blob), &q) != nil {
		return nil, nil
	}
	members := questNicks(&q)
	sort.Strings(members)
	return &QuestView{Desc: q.Desc, Members: members, Deadline: q.Deadline}, nil
}
