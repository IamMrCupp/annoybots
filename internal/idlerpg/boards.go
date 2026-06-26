package idlerpg

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Alternate leaderboards rank players by a stat other than level. Level is a
// Redis sorted set (fast ZTop), but kills/gold/duel-wins aren't — so we take the
// level board's members as the roster of enrolled characters and sort them by
// the requested sheet field.

// boardField maps a !rpg top argument to its sheet field and a display label.
func boardField(arg string) (field, label string, ok bool) {
	switch strings.ToLower(arg) {
	case "kills", "kill":
		return "kills", "kills", true
	case "gold", "rich", "richest":
		return "gold", "gold", true
	case "duels", "duel", "wins", "spar":
		return "duelw", "duel wins", true
	}
	return "", "", false
}

// topBoard renders a leaderboard. An empty/"level" arg is the classic level
// board; "kills"/"gold"/"duels" rank by that stat instead.
func (m *Manager) topBoard(arg string) string {
	switch strings.ToLower(arg) {
	case "", "level", "lvl":
		return m.leaderboard()
	}
	field, label, ok := boardField(arg)
	if !ok {
		return "boards: !rpg top [level|kills|gold|duels]"
	}
	ctx := context.Background()
	members, err := m.store.ZTop(ctx, boardKey(), 1000)
	if err != nil {
		return "the realm is unreachable right now."
	}
	if len(members) == 0 {
		return "no idlers yet. !rpg to begin the grind."
	}
	type row struct {
		name string
		val  int64
	}
	rows := make([]row, 0, len(members))
	for _, e := range members {
		s, _ := m.store.HGetAll(ctx, sheetKey(e.Member))
		rows = append(rows, row{displayName(e.Member), s[field]})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].val != rows[j].val {
			return rows[i].val > rows[j].val
		}
		return rows[i].name < rows[j].name
	})
	if len(rows) > 5 {
		rows = rows[:5]
	}
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("%s (%d)", r.name, r.val))
	}
	return "top by " + label + ": " + strings.Join(parts, ", ")
}
