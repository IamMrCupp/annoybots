// Package markov implements a small order-N Markov chain used to "mangle" the
// chatter a bot observes into vaguely-coherent annoying interjections. The chain
// can be persisted to disk so a bot's "brain" survives pod restarts.
package markov

import (
	"encoding/gob"
	"math/rand"
	"os"
	"strings"
	"sync"
)

// Sentinel tokens marking the beginning and end of a learned line. They use
// control characters that will never appear in real chat words.
const (
	startTok = "\x01"
	endTok   = "\x02"
	sep      = "\x00"
)

// Chain is a concurrency-safe order-N Markov chain.
type Chain struct {
	mu    sync.Mutex
	order int
	links map[string][]string
}

// New returns an empty chain of the given order (minimum 1, default 2).
func New(order int) *Chain {
	if order < 1 {
		order = 2
	}
	return &Chain{order: order, links: make(map[string][]string)}
}

// Order returns the chain order.
func (c *Chain) Order() int { return c.order }

// Size returns the number of distinct states learned.
func (c *Chain) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.links)
}

func keyOf(words []string) string { return strings.Join(words, sep) }

// Learn incorporates a line of text into the chain.
func (c *Chain) Learn(text string) {
	words := strings.Fields(text)
	if len(words) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	seq := make([]string, 0, len(words)+c.order+1)
	for i := 0; i < c.order; i++ {
		seq = append(seq, startTok)
	}
	seq = append(seq, words...)
	seq = append(seq, endTok)

	for i := 0; i+c.order < len(seq); i++ {
		k := keyOf(seq[i : i+c.order])
		c.links[k] = append(c.links[k], seq[i+c.order])
	}
}

// Generate produces up to maxWords of mangled text. It returns an empty string
// if the chain has not learned anything yet.
func (c *Chain) Generate(rng *rand.Rand, maxWords int) string {
	if maxWords <= 0 {
		maxWords = 30
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.links) == 0 {
		return ""
	}

	state := make([]string, c.order)
	for i := range state {
		state[i] = startTok
	}

	out := make([]string, 0, maxWords)
	for len(out) < maxWords {
		opts := c.links[keyOf(state)]
		if len(opts) == 0 {
			break
		}
		next := opts[rng.Intn(len(opts))]
		if next == endTok {
			break
		}
		out = append(out, next)
		state = append(state[1:], next)
	}
	return strings.Join(out, " ")
}

// persisted is the on-disk representation. Exported fields are required for gob.
type persisted struct {
	Order int
	Links map[string][]string
}

// Save atomically writes the chain to path using a temp file + rename.
func (c *Chain) Save(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(persisted{Order: c.order, Links: c.links}); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads a chain previously written by Save.
func Load(path string) (*Chain, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var p persisted
	if err := gob.NewDecoder(f).Decode(&p); err != nil {
		return nil, err
	}
	if p.Links == nil {
		p.Links = make(map[string][]string)
	}
	if p.Order < 1 {
		p.Order = 2
	}
	return &Chain{order: p.Order, links: p.Links}, nil
}
