package idlerpg

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// who lists the idlers currently online on the sender's network, highest level
// first — handy for finding someone to duel, trade with, or party for a quest.
func (m *Manager) who(msg engine.Message) string {
	m.mu.Lock()
	var keys, nicks []string
	for _, p := range m.online {
		if p.network == msg.Network {
			keys = append(keys, p.key)
			nicks = append(nicks, p.nick)
		}
	}
	m.mu.Unlock()
	if len(nicks) == 0 {
		return "no one is idling right now. (are they present and enrolled?)"
	}
	ctx := context.Background()
	type row struct {
		nick  string
		level int64
	}
	rows := make([]row, len(nicks))
	for i := range nicks {
		s, _ := m.store.HGetAll(ctx, sheetKey(keys[i]))
		rows[i] = row{nicks[i], s["level"]}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].level != rows[j].level {
			return rows[i].level > rows[j].level
		}
		return rows[i].nick < rows[j].nick
	})
	parts := make([]string, len(rows))
	for i, r := range rows {
		parts[i] = fmt.Sprintf("%s (lvl %d)", r.nick, r.level)
	}
	return fmt.Sprintf("🧍 idling now (%d): %s", len(rows), strings.Join(parts, ", "))
}
