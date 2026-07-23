// Package bot wires multiple chat transports (IRC, Discord, ...) to the single
// shared annoyance engine. Each Transport owns a set of named networks; the
// Router fans engine replies back out to whichever transport owns the target
// network, so a message that arrives on Discord is answered on Discord and one
// that arrives on IRC is answered on IRC.
package bot

import (
	"context"
	"sort"
	"strings"

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
	canonical  map[string]string // lowercased name -> the name as configured
}

// NewRouter returns an empty Router.
func NewRouter() *Router {
	return &Router{byNetwork: make(map[string]Transport), canonical: make(map[string]string)}
}

// Add registers a transport and indexes the networks it owns.
func (r *Router) Add(t Transport) {
	r.transports = append(r.transports, t)
	for _, n := range t.Networks() {
		r.byNetwork[n] = t
		r.canonical[strings.ToLower(n)] = n
	}
}

// Resolve maps a user-typed network name to the one as configured, matching
// case-insensitively. Network names are typed by hand in admin commands, so
// "EMPRadio" should find "empradio" rather than silently doing nothing.
func (r *Router) Resolve(network string) (string, bool) {
	if _, ok := r.byNetwork[network]; ok {
		return network, true
	}
	canon, ok := r.canonical[strings.ToLower(network)]
	return canon, ok
}

// Networks lists every network name the router knows, sorted — so callers can
// tell a user what the valid names actually are.
func (r *Router) Networks() []string {
	out := make([]string, 0, len(r.byNetwork))
	for n := range r.byNetwork {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// transportFor finds the transport owning a network, tolerating case.
func (r *Router) transportFor(network string) (Transport, string, bool) {
	canon, ok := r.Resolve(network)
	if !ok {
		return nil, "", false
	}
	return r.byNetwork[canon], canon, true
}

// Say routes a message to the transport that owns network.
func (r *Router) Say(network, target, text string) {
	if t, canon, ok := r.transportFor(network); ok {
		t.Say(canon, target, text)
	}
}

// Action routes an action/emote to the transport that owns network.
func (r *Router) Action(network, target, text string) {
	if t, canon, ok := r.transportFor(network); ok {
		t.Action(canon, target, text)
	}
}

// Notice routes an IRC NOTICE to the transport that owns network if it supports
// one; otherwise it falls back to a normal message (engine.Noticer).
func (r *Router) Notice(network, target, text string) {
	t, canon, ok := r.transportFor(network)
	if !ok {
		return
	}
	if n, ok := t.(engine.Noticer); ok {
		n.Notice(canon, target, text)
		return
	}
	t.Say(canon, target, text)
}

// Join asks the owning transport to join a channel.
func (r *Router) Join(network, channel string) {
	if t, canon, ok := r.transportFor(network); ok {
		t.Join(canon, channel)
	}
}

// Part asks the owning transport to leave a channel.
func (r *Router) Part(network, channel string) {
	if t, canon, ok := r.transportFor(network); ok {
		t.Part(canon, channel)
	}
}

// Invite asks the owning transport to invite a user to a channel.
func (r *Router) Invite(network, nick, channel string) {
	if t, canon, ok := r.transportFor(network); ok {
		t.Invite(canon, nick, channel)
	}
}

// Op asks the owning transport to grant channel-operator status to nick, if the
// transport supports it (engine.Opper) and the bot holds ops there. Returns true
// only when the mode was actually sent — Discord and other non-IRC transports,
// which have no op mode, always return false.
func (r *Router) Op(network, channel, nick string) bool {
	if o, canon, ok := r.opperFor(network); ok {
		return o.Op(canon, channel, nick)
	}
	return false
}

// Mode asks the owning transport to apply a channel mode change to nick.
func (r *Router) Mode(network, channel, modes, nick string) bool {
	if o, canon, ok := r.opperFor(network); ok {
		return o.Mode(canon, channel, modes, nick)
	}
	return false
}

// Kick asks the owning transport to remove nick from the channel.
func (r *Router) Kick(network, channel, nick, reason string) bool {
	if o, canon, ok := r.opperFor(network); ok {
		return o.Kick(canon, channel, nick, reason)
	}
	return false
}

// opperFor returns the mode-capable transport owning network, if any.
func (r *Router) opperFor(network string) (engine.Opper, string, bool) {
	t, canon, ok := r.transportFor(network)
	if !ok {
		return nil, "", false
	}
	o, ok := t.(engine.Opper)
	return o, canon, ok
}

// Identify asks the owning transport to (re)authenticate to services on network.
func (r *Router) Identify(network, password string) bool {
	if t, canon, ok := r.transportFor(network); ok {
		return t.Identify(canon, password)
	}
	return false
}

// HasNetwork reports whether any transport owns the named network.
func (r *Router) HasNetwork(network string) bool {
	_, ok := r.Resolve(network)
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
