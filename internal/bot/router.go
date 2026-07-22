// Package bot wires multiple chat transports (IRC, Discord, ...) to the single
// shared annoyance engine. Each Transport owns a set of named networks; the
// Router fans engine replies back out to whichever transport owns the target
// network, so a message that arrives on Discord is answered on Discord and one
// that arrives on IRC is answered on IRC.
package bot

import (
	"context"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Transport is one connection to a chat platform. It can send to its networks
// (engine.Sender), perform channel control (join/part/invite), and manages its
// own lifecycle.
type Transport interface {
	engine.Sender
	Join(network, channel string)
	Part(network, channel string)
	Invite(network, nick, channel string)
	// Identify (re)authenticates to services on a network. An empty password uses
	// the network's configured NickServ secret. Returns false if the network is
	// unknown, isn't IRC, or has no password to use.
	Identify(network, password string) bool
	Networks() []string
	NetworkStatus() map[string]bool // network -> connected
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

// Notice routes an IRC NOTICE to the transport that owns network if it supports
// one; otherwise it falls back to a normal message (engine.Noticer).
func (r *Router) Notice(network, target, text string) {
	s, ok := r.byNetwork[network]
	if !ok {
		return
	}
	if n, ok := s.(engine.Noticer); ok {
		n.Notice(network, target, text)
		return
	}
	s.Say(network, target, text)
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

// Op asks the owning transport to grant channel-operator status to nick, if the
// transport supports it (engine.Opper) and the bot holds ops there. Returns true
// only when the mode was actually sent — Discord and other non-IRC transports,
// which have no op mode, always return false.
func (r *Router) Op(network, channel, nick string) bool {
	if o, ok := r.opperFor(network); ok {
		return o.Op(network, channel, nick)
	}
	return false
}

// Mode asks the owning transport to apply a channel mode change to nick.
func (r *Router) Mode(network, channel, modes, nick string) bool {
	if o, ok := r.opperFor(network); ok {
		return o.Mode(network, channel, modes, nick)
	}
	return false
}

// Kick asks the owning transport to remove nick from the channel.
func (r *Router) Kick(network, channel, nick, reason string) bool {
	if o, ok := r.opperFor(network); ok {
		return o.Kick(network, channel, nick, reason)
	}
	return false
}

// opperFor returns the mode-capable transport owning network, if any.
func (r *Router) opperFor(network string) (engine.Opper, bool) {
	t, ok := r.byNetwork[network]
	if !ok {
		return nil, false
	}
	o, ok := t.(engine.Opper)
	return o, ok
}

// Identify asks the owning transport to (re)authenticate to services on network.
func (r *Router) Identify(network, password string) bool {
	if t, ok := r.byNetwork[network]; ok {
		return t.Identify(network, password)
	}
	return false
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

// NetworkStatus aggregates every network's connection state across transports.
func (r *Router) NetworkStatus() map[string]bool {
	out := map[string]bool{}
	for _, t := range r.transports {
		for n, up := range t.NetworkStatus() {
			out[n] = up
		}
	}
	return out
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
