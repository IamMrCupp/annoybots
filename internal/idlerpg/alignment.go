package idlerpg

import "strings"

// Alignment is the D&D 9-point grid: an ethical axis (the "law" field: 0 neutral,
// 1 lawful, 2 chaotic) crossed with the moral axis (the existing "align" field:
// 0 neutral, 1 good, 2 evil). The moral axis keeps its combat effects (good
// +power, evil +crit); the ethical axis adds a light touch in monster fights —
// lawful is disciplined (+AC), chaotic is reckless (+attack).

// ethicName renders the ethical axis.
func ethicName(v int64) string {
	switch v {
	case 1:
		return "lawful"
	case 2:
		return "chaotic"
	default:
		return "neutral"
	}
}

// fullAlign renders the 9-point alignment from the two axes.
func fullAlign(law, moral int64) string {
	e, m := ethicName(law), alignName(moral)
	if e == "neutral" && m == "neutral" {
		return "true neutral"
	}
	return e + " " + m
}

// parseEthic maps a word to an ethical-axis value ("true" aliases neutral).
func parseEthic(s string) (int64, bool) {
	switch strings.ToLower(s) {
	case "lawful":
		return 1, true
	case "chaotic":
		return 2, true
	case "neutral", "true":
		return 0, true
	}
	return 0, false
}

// parseMoral maps a word to a moral-axis value.
func parseMoral(s string) (int64, bool) {
	switch strings.ToLower(s) {
	case "good":
		return 1, true
	case "evil":
		return 2, true
	case "neutral":
		return 0, true
	}
	return 0, false
}
