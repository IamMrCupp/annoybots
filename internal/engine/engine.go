// Package engine implements the network-agnostic "annoyance" behavior: keyword
// triggers, random ambient interjections with per-channel cooldowns, an optional
// Markov mangler, and a couple of bang-commands. It is the modern, containerized
// stand-in for the old BMotion TCL scripts.
package engine

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrcupp/annoybots/internal/cooldown"
	"github.com/mrcupp/annoybots/internal/markov"
)

// Engine evaluates inbound messages against a Personality and emits responses.
type Engine struct {
	p     Personality
	res   []*regexp.Regexp // compiled patterns, indexed alongside p.Triggers
	cool  *cooldown.Manager
	brain *markov.Chain

	mu  sync.Mutex // guards rng (math/rand is not concurrency-safe)
	rng *rand.Rand
}

// Options carries test-injectable dependencies. All fields are optional.
type Options struct {
	Rand  *rand.Rand
	Now   func() time.Time
	Brain *markov.Chain
}

// New compiles the personality's triggers and returns a ready Engine.
func New(p Personality, opts Options) (*Engine, error) {
	res := make([]*regexp.Regexp, len(p.Triggers))
	for i := range p.Triggers {
		re, err := regexp.Compile(p.Triggers[i].Pattern)
		if err != nil {
			return nil, fmt.Errorf("trigger %q: invalid pattern: %w", p.Triggers[i].Name, err)
		}
		res[i] = re
		if p.Triggers[i].Chance == 0 {
			p.Triggers[i].Chance = 1 // an unset chance means "always fire on match"
		}
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rng := opts.Rand
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	brain := opts.Brain
	if brain == nil {
		order := p.Markov.Order
		if order < 1 {
			order = 2
		}
		brain = markov.New(order)
	}

	return &Engine{
		p:     p,
		res:   res,
		cool:  cooldown.NewWithClock(now),
		brain: brain,
		rng:   rng,
	}, nil
}

// Brain exposes the Markov chain so the caller can persist it.
func (e *Engine) Brain() *markov.Chain { return e.brain }

// Handle processes one inbound message and may emit one response via out.
func (e *Engine) Handle(msg Message, out Sender) {
	if msg.Text == "" || strings.EqualFold(msg.Nick, msg.Self) {
		return // ignore empty lines and our own echoes
	}

	// Learn from real chatter (not commands) to grow the brain.
	if e.p.Markov.Enabled && e.p.Markov.Learn && !strings.HasPrefix(msg.Text, "!") {
		e.brain.Learn(msg.Text)
	}

	if strings.HasPrefix(msg.Text, "!") && (e.p.Commands || e.p.Quotes.Command) {
		if e.handleCommand(msg, out) {
			return
		}
	}

	if e.fireTriggers(msg, out) {
		return
	}

	// At most one ambient line per message: try a quote, then an interjection.
	if e.maybeQuote(msg, out) {
		return
	}
	e.maybeInterject(msg, out)
}

// fireTriggers walks triggers in order and responds to the first match that
// passes its chance roll and cooldown. Returns true if a trigger "claimed" the
// message (responded or suppressed on cooldown).
func (e *Engine) fireTriggers(msg Message, out Sender) bool {
	for i := range e.p.Triggers {
		t := &e.p.Triggers[i]
		m := e.res[i].FindStringSubmatch(msg.Text)
		if m == nil {
			continue
		}
		if !e.roll(t.Chance) {
			continue // matched but lost the dice roll; let other triggers try
		}
		key := "trig:" + t.Name + ":" + msg.Network + ":" + msg.Channel
		if t.Cooldown.D() > 0 && !e.cool.Use(key, t.Cooldown.D()) {
			return true // matched but on cooldown; stay quiet
		}
		resp := e.render(e.pick(t.Responses), msg, m)
		if resp == "" {
			return true
		}
		e.emit(out, msg.Network, msg.Channel, resp, t.Action)
		return true
	}
	return false
}

// maybeInterject randomly drops an ambient line into a channel.
func (e *Engine) maybeInterject(msg Message, out Sender) {
	in := e.p.Interjections
	if !in.Enabled || msg.Private {
		return
	}
	if !e.roll(in.Chance) {
		return
	}
	key := "interject:" + msg.Network + ":" + msg.Channel
	if in.Cooldown.D() > 0 && !e.cool.Use(key, in.Cooldown.D()) {
		return
	}
	var line string
	if in.UseMarkov && e.p.Markov.Enabled {
		line = e.markovLine()
	}
	if line == "" {
		line = e.render(e.pick(in.Lines), msg, nil)
	}
	if line != "" {
		out.Say(msg.Network, msg.Channel, line)
	}
}

// handleCommand implements a tiny set of bang-commands. Returns true if handled.
func (e *Engine) handleCommand(msg Message, out Sender) bool {
	fields := strings.Fields(msg.Text)
	switch strings.ToLower(fields[0]) {
	case "!quote":
		return e.handleQuoteCommand(msg, out)
	}
	if !e.p.Commands {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "!annoy":
		line := e.markovLine()
		if line == "" {
			line = e.render(e.pick(e.p.Interjections.Lines), msg, nil)
		}
		if line != "" {
			out.Say(msg.Network, msg.Channel, line)
		}
		return true
	case "!source":
		out.Say(msg.Network, msg.Channel, "I am "+e.p.Name+", reborn: github.com/mrcupp/annoybots")
		return true
	default:
		return false
	}
}

func (e *Engine) markovLine() string {
	if !e.p.Markov.Enabled {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.brain.Generate(e.rng, e.p.Markov.MaxWords)
}

// render substitutes placeholders in a response template.
func (e *Engine) render(tmpl string, msg Message, match []string) string {
	if tmpl == "" {
		return ""
	}
	r := tmpl
	r = strings.ReplaceAll(r, "{nick}", msg.Nick)
	r = strings.ReplaceAll(r, "{me}", msg.Self)
	r = strings.ReplaceAll(r, "{chan}", msg.Channel)
	for i, g := range match {
		r = strings.ReplaceAll(r, "{"+strconv.Itoa(i)+"}", g)
	}
	if strings.Contains(r, "{markov}") {
		r = strings.ReplaceAll(r, "{markov}", e.markovLine())
	}
	return strings.TrimSpace(r)
}

func (e *Engine) emit(out Sender, network, target, text string, action bool) {
	if action {
		out.Action(network, target, text)
		return
	}
	out.Say(network, target, text)
}

// roll returns true with probability p. p<=0 is never, p>=1 is always.
func (e *Engine) roll(p float64) bool {
	if p >= 1 {
		return true
	}
	if p <= 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rng.Float64() < p
}

func (e *Engine) pick(list []string) string {
	if len(list) == 0 {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return list[e.rng.Intn(len(list))]
}
