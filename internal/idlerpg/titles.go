package idlerpg

import "context"

// Titles are earned honorifics, derived purely from a character's sheet — kills
// measure combat renown, level measures legend. The most prestigious one a
// character qualifies for is shown next to their name. No extra state: a title
// is just a function of values already on the sheet, so existing characters earn
// theirs the moment this ships.

type titleDef struct {
	name  string // includes the leading "the"
	level int64  // minimum level (0 = ignore this axis)
	kills int64  // minimum kills (0 = ignore this axis)
}

// titles, most prestigious first. titleFor scans top-down and returns the first
// match, so order is the ranking. Each title gates on a single axis (the other
// is 0) to keep the ladder legible.
var titles = []titleDef{
	{"the Eternal", 100, 0},
	{"the Annihilator", 0, 1000},
	{"the Mythic", 75, 0},
	{"the Dragonslayer", 0, 500},
	{"the Ascended", 50, 0},
	{"the Bloodied", 0, 250},
	{"the Veteran", 30, 0},
	{"the Slayer", 0, 100},
	{"the Seasoned", 15, 0},
	{"the Brave", 0, 25},
}

// titleFor returns the highest earned honorific, or "" if none yet.
func titleFor(sheet map[string]int64) string {
	for _, t := range titles {
		if (t.level == 0 || sheet["level"] >= t.level) &&
			(t.kills == 0 || sheet["kills"] >= t.kills) {
			return t.name
		}
	}
	return ""
}

// charLine renders a character's name with its earned title and descriptor —
// "iammrcupp the Dragonslayer, true neutral wizard" once a title is earned, or
// the plain "iammrcupp the true neutral wizard" before then.
func (m *Manager) charLine(ctx context.Context, nick, pkey string, sheet map[string]int64) string {
	desc := m.charDesc(ctx, pkey, sheet)
	if t := titleFor(sheet); t != "" {
		return nick + " " + t + ", " + desc
	}
	return nick + " the " + desc
}
