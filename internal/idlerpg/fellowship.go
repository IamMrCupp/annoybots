package idlerpg

// Fellowship rewards playing together: when several heroes idle at once, every
// one of them advances a little faster. It never punishes soloing (1 online = no
// change) — it just makes company strictly better.

// fellowshipPct is the idle-speed multiplier (percent) for `online` heroes idling
// together: +5% per companion beyond the first, capped at +30% (7+ together).
func fellowshipPct(online int) int64 {
	if online <= 1 {
		return 100
	}
	bonus := int64((online - 1) * 5)
	if bonus > 30 {
		bonus = 30
	}
	return 100 + bonus
}
