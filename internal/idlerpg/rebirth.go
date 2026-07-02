package idlerpg

import (
	"context"
	"fmt"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Rebirth is the prestige / new-game-plus layer. At rebirthLevel you may be
// reborn: your level (and leaderboard rank) reset to 0, but you keep your gold,
// gear, kills, feats, and titles — and gain a permanent leveling-speed bonus plus
// prestige stars by your name. It rewards the dedicated with a faster re-climb.

const rebirthLevel = 50 // minimum level required to be reborn

// rebSpeed is the leveling-time multiplier (percent) for a rebirth count: each
// rebirth shaves 5% off the time to level, floored at 50% (10 rebirths).
func rebSpeed(reb int64) int64 {
	s := 100 - reb*5
	if s < 50 {
		s = 50
	}
	return s
}

// ttlForReb is ttlFor scaled by a player's rebirth speed bonus.
func (m *Manager) ttlForReb(level, reb int64) int64 {
	t := m.ttlFor(level) * rebSpeed(reb) / 100
	if t < 1 {
		t = 1
	}
	return t
}

// stars renders a rebirth count as prestige stars (e.g. "★3 "), or "" for none.
func stars(reb int64) string {
	if reb <= 0 {
		return ""
	}
	return fmt.Sprintf("★%d ", reb)
}

// rebirth handles !rpg rebirth — the prestige reset, available at high level.
func (m *Manager) rebirth(msg engine.Message) {
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	if len(sheet) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	if sheet["level"] < rebirthLevel {
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf(
			"%s must reach level %d to be reborn — you're only level %d.", msg.Nick, rebirthLevel, sheet["level"]))
		return
	}
	reb, _ := m.store.HIncr(ctx, sheetKey(pkey), "reb", 1)
	old := sheet["level"]
	_ = m.store.HSet(ctx, sheetKey(pkey), "level", 0)
	_ = m.store.HSet(ctx, sheetKey(pkey), "ttl", m.ttlForReb(0, reb))
	_, _ = m.store.ZIncr(ctx, boardKey(), pkey, -old) // rank falls back to 0
	p := player{network: msg.Network, nick: msg.Nick, channel: msg.Channel, key: pkey}
	m.drama(p.network, p.channel, fmt.Sprintf(
		"🌟 %s is REBORN — prestige ★%d! their level falls to 0, but they keep their gold, gear, and glory, and now grind %d%% faster forever. a legend begins anew.",
		msg.Nick, reb, 100-rebSpeed(reb)))
}
