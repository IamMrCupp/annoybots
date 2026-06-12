// Package bot wires multiple chat transports (IRC, Discord, ...) to the single
// shared annoyance engine. Each Transport owns a set of named networks; the
// Router fans engine replies back out to whichever transport owns the target
// network, so a message that arrives on Discord is answered on Discord and one
// that arrives on IRC is answered on IRC.
package bot

import (
	"context"

	"github.com/mrcupp/annoybots/internal/engine"
)

// Transport is one connection to a chat platform. It can send to its networks
// (engine.Sender), perform channel control (join/part/invite), and manages its
// own lifecycle.
type Transport interface {
	engine.Sender
	Join(network, channel string)
	Part(network, channel string)
	Invite(network, nick, channel string)
	Networks() []string
	Run(ctx context.Context)
	Quit()
	Wait()
	AnyConnected() bool
}

// Router implements engine.Sender (and channel control) by dispatching to the
// owning transport, and aggregates lifecycle calls across all registered
// transports.
type Router struct {
	transports []Transport
	byNetwork  map[string]Transport
}

// NewRouter returns an empty Router.
func NewRouter() *Router {
	return &Router{byNetwork: make(map[string]Transport)}
}

// Add registers a transport and indexes the networks it owns.
func (r *Router) Add(t Transport) {
	r.transports = append(r.transports, t)
	for _, n := range t.Networks() {
		r.byNetwork[n] = t
	}
}

// Say routes a message to the transport that owns network.
func (r *Router) Say(network, target, text string) {
	if s, ok := r.byNetwork[network]; ok {
		s.Say(network, target, text)
	}
}

// Action routes an action/emote to the transport that owns network.
func (r *Router) Action(network, target, text string) {
	if s, ok := r.byNetwork[network]; ok {
		s.Action(network, target, text)
	}
}

// Join asks the owning transport to join a channel.
func (r *Router) Join(network, channel string) {
	if t, ok := r.byNetwork[network]; ok {
		t.Join(network, channel)
	}
}

// Part asks the owning transport to leave a channel.
func (r *Router) Part(network, channel string) {
	if t, ok := r.byNetwork[network]; ok {
		t.Part(network, channel)
	}
}

// Invite asks the owning transport to invite a user to a channel.
func (r *Router) Invite(network, nick, channel string) {
	if t, ok := r.byNetwork[network]; ok {
		t.Invite(network, nick, channel)
	}
}

// HasNetwork reports whether any transport owns the named network.
func (r *Router) HasNetwork(network string) bool {
	_, ok := r.byNetwork[network]
	return ok
}

// Run starts every transport.
func (r *Router) Run(ctx context.Context) {
	for _, t := range r.transports {
		t.Run(ctx)
	}
}

// Quit asks every transport to disconnect.
func (r *Router) Quit() {
	for _, t := range r.transports {
		t.Quit()
	}
}

// Wait blocks until every transport has fully stopped.
func (r *Router) Wait() {
	for _, t := range r.transports {
		t.Wait()
	}
}

// AnyConnected reports whether any transport has a live connection.
func (r *Router) AnyConnected() bool {
	for _, t := range r.transports {
		if t.AnyConnected() {
			return true
		}
	}
	return false
}
