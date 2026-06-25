package idlerpg

import (
	"context"
	"fmt"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Duels are friendly spars between two present players: a best-of-three contest
// of effective power (gear + alignment + class) plus luck. They're bragging
// rights only — no clock changes, no HP, no rewards — so they can't be farmed,
// just enjoyed. A vanity win counter keeps score.

func duelWinsField() string { return "duelw" }

// duelPower is a character's standing combat strength for a spar: item power,
// blessed by a good alignment, sharpened or dulled by class.
func (m *Manager) duelPower(ctx context.Context, key string) int64 {
	sheet, _ := m.store.HGetAll(ctx, sheetKey(key))
	pow := itemSum(sheet)
	if sheet["align"] == 1 { // good is blessed in combat
		pow = pow * 111 / 100
	}
	class, _ := m.store.GetStr(ctx, classKey(key))
	pow += classAttackMod(sheet, class)
	if pow < 0 {
		pow = 0
	}
	return pow
}

// onlineByNick finds a currently-online player on a network by nick (case-insensitive).
func (m *Manager) onlineByNick(network, nick string) (player, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.online {
		if p.network == network && strings.EqualFold(p.nick, nick) {
			return p, true
		}
	}
	return player{}, false
}

// duel resolves !rpg duel <name> — a friendly best-of-three spar with a present
// opponent. Bragging rights only.
func (m *Manager) duel(msg engine.Message, fields []string) {
	if len(fields) < 3 {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg duel <name>")
		return
	}
	ctx := context.Background()
	chKey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if s, _ := m.store.HGetAll(ctx, sheetKey(chKey)); len(s) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing yet — !rpg to enlist.")
		return
	}
	target := fields[2]
	if strings.EqualFold(target, msg.Nick) {
		m.out.Say(msg.Network, msg.Channel, msg.Nick+" shadowboxes for a while. it's a draw.")
		return
	}
	opp, ok := m.onlineByNick(msg.Network, target)
	if !ok {
		m.out.Say(msg.Network, msg.Channel, target+" isn't around to spar.")
		return
	}

	chPow, oppPow := m.duelPower(ctx, chKey), m.duelPower(ctx, opp.key)
	chWins, oppWins := 0, 0
	for r := 0; r < 3; r++ {
		if m.roll(int(chPow)+1) >= m.roll(int(oppPow)+1) {
			chWins++
		} else {
			oppWins++
		}
	}

	winnerNick, winnerKey, ws, ls := msg.Nick, chKey, chWins, oppWins
	if oppWins > chWins {
		winnerNick, winnerKey, ws, ls = opp.nick, opp.key, oppWins, chWins
	}
	wins, _ := m.store.HIncr(ctx, sheetKey(winnerKey), duelWinsField(), 1)
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf(
		"⚔️ %s [%d] and %s [%d] spar — %s takes it %d–%d! (friendly; %s's %s career win.)",
		msg.Nick, chPow, target, oppPow, winnerNick, ws, ls, winnerNick, ordinal(wins)))
}

// ordinal renders 1→"1st", 2→"2nd", 3→"3rd", 11→"11th", 21→"21st", …
func ordinal(n int64) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}
