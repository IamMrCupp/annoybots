package games

import (
	"fmt"
	"strings"
)

// Slots is the cheapest possible dopamine: three reels, a payout table, and no
// stakes at all. It exists to be pulled idly in a quiet channel.

var slotReel = []string{"🍒", "🍋", "🔔", "⭐", "💎", "7️⃣"}

// spin returns three symbols and the line that describes the result.
func (m *Manager) spin(nick string) string {
	a := slotReel[m.rng.Intn(len(slotReel))]
	b := slotReel[m.rng.Intn(len(slotReel))]
	c := slotReel[m.rng.Intn(len(slotReel))]
	line := fmt.Sprintf("[ %s | %s | %s ]", a, b, c)

	switch {
	case a == b && b == c && a == "7️⃣":
		return fmt.Sprintf("%s %s — JACKPOT! %s hits triple sevens. the channel erupts.", line, strings.Repeat("🎉", 3), nick)
	case a == b && b == c:
		return fmt.Sprintf("%s — three of a kind! %s wins nothing at all, gloriously.", line, nick)
	case a == b || b == c || a == c:
		return fmt.Sprintf("%s — two of a kind. %s is *this* close.", line, nick)
	default:
		return fmt.Sprintf("%s — nothing. %s walks away poorer in spirit only.", line, nick)
	}
}
