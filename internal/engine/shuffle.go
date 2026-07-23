package engine

import (
	"hash/fnv"
	"strings"
)

// Line selection used to be a plain uniform random pick, which reads as broken in
// a live channel: with a three-line banter pool you get the same line twice in a
// row about a third of the time, and the channel fills with "Must you, kurkutu?"
// over and over.
//
// Instead every pool gets a shuffle bag — deal the lines in a random order and
// don't reshuffle until they've all been used. That guarantees the maximum
// possible spacing between repeats (every other line appears first) while still
// feeling unordered. It's the same trick Tetris uses for piece selection.
//
// Bags are keyed by a fingerprint of the pool's contents, so pools stay separate
// without threading identifiers through every call site, and a pool that changes
// (a !reload, an !addquote) simply starts a fresh bag.

// poolKey fingerprints a pool by its contents.
func poolKey(list []string) string {
	h := fnv.New64a()
	for _, s := range list {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	return string(rune(len(list))) + ":" + strings.TrimSpace(string(h.Sum(nil)))
}

// nextFromBag returns the next index for a pool, refilling and reshuffling when
// the bag runs dry. Callers must hold e.mu (it uses the shared rng).
func (e *Engine) nextFromBag(key string, n int) int {
	if n <= 0 {
		return 0
	}
	if e.bags == nil {
		e.bags = make(map[string][]int)
		e.lastPick = make(map[string]int)
	}
	bag := e.bags[key]
	if len(bag) == 0 {
		bag = make([]int, n)
		for i := range bag {
			bag[i] = i
		}
		e.rng.Shuffle(n, func(i, j int) { bag[i], bag[j] = bag[j], bag[i] })
		// A fresh shuffle can open with the line we just used — the one repeat a
		// bag exists to prevent. Lines are dealt from the tail, so it's bag[n-1]
		// that comes next; push it to the front if it would repeat.
		if n > 1 {
			if last, ok := e.lastPick[key]; ok && bag[n-1] == last {
				bag[0], bag[n-1] = bag[n-1], bag[0]
			}
		}
	}
	idx := bag[len(bag)-1]
	e.bags[key] = bag[:len(bag)-1]
	e.lastPick[key] = idx
	return idx
}
