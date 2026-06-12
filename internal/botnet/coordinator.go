package botnet

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/IamMrCupp/annoybots/internal/cooldown"
	"github.com/IamMrCupp/annoybots/internal/engine"
)

// stepWaitTimeout bounds how long a bot waits for another bot's skit step before
// giving up, so a crashed or absent partner can't wedge a skit forever.
const stepWaitTimeout = 30 * time.Second

// Coordinator runs scripted skits for one bot. It listens on the bus, performs
// the steps assigned to this bot, and advances the exchange in lockstep with the
// other bots.
type Coordinator struct {
	name  string
	bus   Bus
	out   engine.Sender
	skits map[string]Skit
	order []string
	cool  *cooldown.Manager
	log   *slog.Logger
	now   func() time.Time

	rngMu sync.Mutex
	rng   *rand.Rand

	mu      sync.Mutex
	active  map[string]bool     // nonce -> running (single performance per nonce)
	waiters map[string]chan int // nonce -> advance-step channel
}

// Options carries test-injectable dependencies.
type Options struct {
	Now  func() time.Time
	Rand *rand.Rand
}

// NewCoordinator builds a coordinator for the named bot.
func NewCoordinator(name string, bus Bus, out engine.Sender, skits []Skit, log *slog.Logger, opts Options) *Coordinator {
	m := make(map[string]Skit, len(skits))
	order := make([]string, 0, len(skits))
	for _, s := range skits {
		key := strings.ToLower(s.Name)
		m[key] = s
		order = append(order, key)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rng := opts.Rand
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &Coordinator{
		name:    strings.ToLower(name),
		bus:     bus,
		out:     out,
		skits:   m,
		order:   order,
		cool:    cooldown.NewWithClock(now),
		log:     log,
		now:     now,
		rng:     rng,
		active:  make(map[string]bool),
		waiters: make(map[string]chan int),
	}
}

// Run subscribes to the bus and dispatches events until ctx ends.
func (c *Coordinator) Run(ctx context.Context) error {
	ch, err := c.bus.Subscribe(ctx)
	if err != nil {
		return err
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				c.onEvent(ctx, e)
			}
		}
	}()
	return nil
}

func (c *Coordinator) onEvent(ctx context.Context, e Event) {
	switch e.Type {
	case EventSkitStart:
		c.beginPerform(ctx, e)
	case EventSkitAdvance:
		if e.From == c.name {
			return // ignore our own advance echoes
		}
		c.mu.Lock()
		w := c.waiters[e.Nonce]
		c.mu.Unlock()
		if w != nil {
			select {
			case w <- e.Step:
			default:
			}
		}
	}
}

// getSkit returns a skit by name (concurrency-safe; skits can be hot-reloaded).
func (c *Coordinator) getSkit(name string) (Skit, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.skits[strings.ToLower(name)]
	return s, ok
}

// skitOrder returns a snapshot of skit keys in order.
func (c *Coordinator) skitOrder() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.order...)
}

// SetSkits replaces the loaded skit definitions (used by !reload).
func (c *Coordinator) SetSkits(skits []Skit) {
	m := make(map[string]Skit, len(skits))
	order := make([]string, 0, len(skits))
	for _, s := range skits {
		k := strings.ToLower(s.Name)
		m[k] = s
		order = append(order, k)
	}
	c.mu.Lock()
	c.skits = m
	c.order = order
	c.mu.Unlock()
}

func (c *Coordinator) beginPerform(ctx context.Context, e Event) {
	skit, ok := c.getSkit(e.Skit)
	if !ok || len(skit.Steps) == 0 {
		return
	}
	c.mu.Lock()
	if c.active[e.Nonce] {
		c.mu.Unlock()
		return // already performing this run
	}
	c.active[e.Nonce] = true
	w := make(chan int, len(skit.Steps))
	c.waiters[e.Nonce] = w
	c.mu.Unlock()

	go c.perform(ctx, e, skit, w)
}

func (c *Coordinator) perform(ctx context.Context, e Event, skit Skit, advance chan int) {
	defer func() {
		c.mu.Lock()
		delete(c.active, e.Nonce)
		delete(c.waiters, e.Nonce)
		c.mu.Unlock()
	}()

	for i, step := range skit.Steps {
		if strings.EqualFold(step.Bot, c.name) {
			if d := step.Delay.D(); d > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(d):
				}
			}
			c.emit(e.Network, e.Channel, step.Line, step.Action)
			_ = c.bus.Publish(ctx, Event{
				Type:    EventSkitAdvance,
				From:    c.name,
				Network: e.Network,
				Channel: e.Channel,
				Skit:    e.Skit,
				Step:    i,
				Nonce:   e.Nonce,
			})
			continue
		}
		if !c.waitForStep(ctx, advance, i) {
			return // partner timed out or ctx ended
		}
	}
}

// waitForStep blocks until an advance for the given step index arrives.
func (c *Coordinator) waitForStep(ctx context.Context, advance chan int, step int) bool {
	timer := time.NewTimer(stepWaitTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case s := <-advance:
			if s == step {
				return true
			}
		case <-timer.C:
			c.log.Warn("skit step timed out waiting for partner", "step", step)
			return false
		}
	}
}

// OnMessage lets a human message kick off a skit, via "!skit [name]" or a random
// auto-start. Callers must only pass human (non-bot) messages.
func (c *Coordinator) OnMessage(ctx context.Context, msg engine.Message) {
	if msg.Private {
		return
	}
	text := strings.TrimSpace(msg.Text)
	if strings.HasPrefix(strings.ToLower(text), "!skit") {
		name := ""
		if f := strings.Fields(text); len(f) > 1 {
			name = f[1]
		}
		c.tryStart(ctx, msg.Network, msg.Channel, name)
		return
	}
	c.maybeAutoStart(ctx, msg.Network, msg.Channel)
}

func (c *Coordinator) tryStart(ctx context.Context, network, channel, name string) {
	var skit Skit
	var ok bool
	if name != "" {
		skit, ok = c.getSkit(name)
	} else {
		skit, ok = c.randomLedSkit()
	}
	if !ok || len(skit.Steps) == 0 {
		return
	}
	// Only the lead bot initiates, so both bots don't start the same skit.
	if !strings.EqualFold(skit.Lead(), c.name) {
		return
	}
	cd := skit.Cooldown.D()
	if cd <= 0 {
		cd = 5 * time.Minute
	}
	if !c.cool.Use("skit:"+network+":"+channel, cd) {
		return
	}
	_ = c.bus.Publish(ctx, Event{
		Type:    EventSkitStart,
		From:    c.name,
		Network: network,
		Channel: channel,
		Skit:    skit.Name,
		Nonce:   c.newNonce(),
	})
}

func (c *Coordinator) maybeAutoStart(ctx context.Context, network, channel string) {
	for _, key := range c.skitOrder() {
		s, ok := c.getSkit(key)
		if !ok || s.Chance <= 0 || !strings.EqualFold(s.Lead(), c.name) {
			continue
		}
		if c.roll(s.Chance) {
			c.tryStart(ctx, network, channel, s.Name)
			return
		}
	}
}

func (c *Coordinator) randomLedSkit() (Skit, bool) {
	var led []string
	for _, key := range c.skitOrder() {
		if s, ok := c.getSkit(key); ok && strings.EqualFold(s.Lead(), c.name) {
			led = append(led, key)
		}
	}
	if len(led) == 0 {
		return Skit{}, false
	}
	c.rngMu.Lock()
	pick := led[c.rng.Intn(len(led))]
	c.rngMu.Unlock()
	return c.getSkit(pick)
}

func (c *Coordinator) emit(network, channel, line string, action bool) {
	if action {
		c.out.Action(network, channel, line)
		return
	}
	c.out.Say(network, channel, line)
}

func (c *Coordinator) roll(p float64) bool {
	if p >= 1 {
		return true
	}
	if p <= 0 {
		return false
	}
	c.rngMu.Lock()
	defer c.rngMu.Unlock()
	return c.rng.Float64() < p
}

func (c *Coordinator) newNonce() string {
	c.rngMu.Lock()
	defer c.rngMu.Unlock()
	return fmt.Sprintf("%s-%d-%d", c.name, c.now().UnixNano(), c.rng.Int63())
}
