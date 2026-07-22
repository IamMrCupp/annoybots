// Package botnet provides the inter-bot communication bus and the coordinator
// that drives multi-bot "skits" -- the modern, cross-platform answer to the old
// eggdrop botnet partyline that BMotion used to set up coordinated trolling.
//
// The bus is deliberately separate from the chat platforms, so a cue published
// by a bot on Discord can drive a skit performed by another bot on Twitch.
package botnet

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Event is a structured message passed between bots over the bus. It carries
// both skit coordination and admin-console sync.
type Event struct {
	Type    string `json:"type"`
	From    string `json:"from"` // originating bot name
	Network string `json:"network,omitempty"`
	Channel string `json:"channel,omitempty"`
	// skit coordination
	Skit  string `json:"skit,omitempty"`
	Step  int    `json:"step,omitempty"`
	Nonce string `json:"nonce,omitempty"`
	// admin-console sync
	Pack     string `json:"pack,omitempty"`      // quote pack name
	Line     string `json:"line,omitempty"`      // quote text
	Account  string `json:"account,omitempty"`   // admin identity account
	AdminNet string `json:"admin_net,omitempty"` // admin identity network
	Flags    string `json:"flags,omitempty"`     // admin access flags (F2)
	// partyline (cross-bot, cross-platform admin chat)
	Text string `json:"text,omitempty"` // partyline message body / notice
	Nick string `json:"nick,omitempty"` // originating member's display nick ("*" = system notice)
	// link auth (see auth.go) — set only when a shared bus secret is configured
	Ts  int64  `json:"ts,omitempty"`  // unix seconds, for replay bounding
	Sig string `json:"sig,omitempty"` // HMAC-SHA256 over the event with Sig cleared
}

// Event type constants.
const (
	EventSkitStart   = "skit_start"
	EventSkitAdvance = "skit_advance"
	EventQuoteAdd    = "quote_add"
	EventQuoteDel    = "quote_del"
	EventAdminAdd    = "admin_add"
	EventAdminDel    = "admin_del"
	EventPartyline   = "partyline"
)

// Bus is a publish/subscribe channel shared by all bots.
type Bus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(ctx context.Context) (<-chan Event, error)
	Close() error
}

// RedisBus is a Bus backed by Redis pub/sub. It bridges bots across pods and
// across chat platforms.
type RedisBus struct {
	client  *redis.Client
	channel string

	secret  []byte           // when set, events are signed and verified (link auth)
	now     func() time.Time // injectable clock, for tests
	dropped atomic.Int64     // events rejected by link auth
}

// NewRedis builds a RedisBus. channel namespaces the pub/sub topic.
func NewRedis(addr, password, channel string) *RedisBus {
	return &RedisBus{
		client:  redis.NewClient(&redis.Options{Addr: addr, Password: password}),
		channel: channel,
		now:     time.Now,
	}
}

// SetSecret enables bot-to-bot link authentication: published events are signed
// and received ones must verify. An empty secret leaves the bus unauthenticated,
// exactly as before.
func (b *RedisBus) SetSecret(secret string) {
	if secret == "" {
		b.secret = nil
		return
	}
	b.secret = []byte(secret)
}

// Dropped reports how many events link auth has rejected — surfaced so a
// mismatched secret shows up as a number instead of silence.
func (b *RedisBus) Dropped() int64 { return b.dropped.Load() }

// Ping verifies connectivity.
func (b *RedisBus) Ping(ctx context.Context) error { return b.client.Ping(ctx).Err() }

// Publish marshals and sends an event.
func (b *RedisBus) Publish(ctx context.Context, e Event) error {
	if len(b.secret) > 0 {
		e.Ts = b.now().Unix()
		e.Sig = signEvent(e, b.secret)
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return b.client.Publish(ctx, b.channel, data).Err()
}

// Subscribe returns a channel of decoded events that closes when ctx ends.
func (b *RedisBus) Subscribe(ctx context.Context) (<-chan Event, error) {
	ps := b.client.Subscribe(ctx, b.channel)
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		defer func() { _ = ps.Close() }()
		in := ps.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-in:
				if !ok {
					return
				}
				var e Event
				if json.Unmarshal([]byte(m.Payload), &e) != nil {
					continue
				}
				if len(b.secret) > 0 && !verifyEvent(e, b.secret, b.now()) {
					b.dropped.Add(1) // forged, unsigned, or stale — never act on it
					continue
				}
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// Close releases the Redis client.
func (b *RedisBus) Close() error { return b.client.Close() }

// MemBus is an in-process Bus used for tests and for running both bots inside a
// single process during local development. It fans every published event out to
// all current subscribers.
type MemBus struct {
	mu   sync.Mutex
	subs []chan Event
}

// NewMem returns an empty in-memory bus.
func NewMem() *MemBus { return &MemBus{} }

// Publish delivers e to every subscriber (non-blocking).
func (m *MemBus) Publish(_ context.Context, e Event) error {
	m.mu.Lock()
	subs := append([]chan Event(nil), m.subs...)
	m.mu.Unlock()
	for _, s := range subs {
		select {
		case s <- e:
		default:
		}
	}
	return nil
}

// Subscribe registers a new subscriber.
func (m *MemBus) Subscribe(_ context.Context) (<-chan Event, error) {
	ch := make(chan Event, 64)
	m.mu.Lock()
	m.subs = append(m.subs, ch)
	m.mu.Unlock()
	return ch, nil
}

// Close is a no-op for the in-memory bus.
func (m *MemBus) Close() error { return nil }
