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

	"github.com/redis/go-redis/v9"
)

// Event is a structured message passed between bots over the bus.
type Event struct {
	Type    string `json:"type"`            // skit_start | skit_advance
	From    string `json:"from"`            // originating bot name
	Network string `json:"network"`         // chat network the skit plays out on
	Channel string `json:"channel"`         // chat channel/ID the skit plays out on
	Skit    string `json:"skit,omitempty"`  // skit name
	Step    int    `json:"step,omitempty"`  // step index for skit_advance
	Nonce   string `json:"nonce,omitempty"` // unique per skit run
}

// Event type constants.
const (
	EventSkitStart   = "skit_start"
	EventSkitAdvance = "skit_advance"
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
}

// NewRedis builds a RedisBus. channel namespaces the pub/sub topic.
func NewRedis(addr, password, channel string) *RedisBus {
	return &RedisBus{
		client:  redis.NewClient(&redis.Options{Addr: addr, Password: password}),
		channel: channel,
	}
}

// Ping verifies connectivity.
func (b *RedisBus) Ping(ctx context.Context) error { return b.client.Ping(ctx).Err() }

// Publish marshals and sends an event.
func (b *RedisBus) Publish(ctx context.Context, e Event) error {
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
