package admin

import (
	"strings"

	"github.com/IamMrCupp/annoybots/internal/botnet"
	"github.com/IamMrCupp/annoybots/internal/engine"
)

// The partyline is the modern, cross-platform answer to the old eggdrop botnet
// partyline: admins "join" by DMing a bot, then everything they type is relayed —
// over the Redis bus — to every other joined admin, across all bots AND across
// networks (an admin on IRC chatting with one on Discord). Membership is local to
// each bot; the bus carries the messages between them.

// partyMember is one admin currently on the partyline, reachable by DMing target
// on network.
type partyMember struct {
	network string
	nick    string
	target  string // where to DM them (their nick on IRC, the DM channel on Discord)
}

func partyKey(network, nick string) string {
	return strings.ToLower(network) + "|" + strings.ToLower(nick)
}

// partyLine formats a relayed line; a "*" nick marks a system notice.
func partyLine(nick, text string) string {
	if nick == "*" {
		return "* " + text
	}
	return "[party] <" + nick + "> " + text
}

func (m *Manager) joinParty(msg engine.Message) (already bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := partyKey(msg.Network, msg.Nick)
	if _, ok := m.party[k]; ok {
		return true
	}
	m.party[k] = partyMember{network: msg.Network, nick: msg.Nick, target: msg.Channel}
	return false
}

func (m *Manager) leaveParty(msg engine.Message) (was bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := partyKey(msg.Network, msg.Nick)
	if _, ok := m.party[k]; !ok {
		return false
	}
	delete(m.party, k)
	return true
}

func (m *Manager) isPartyMember(msg engine.Message) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.party[partyKey(msg.Network, msg.Nick)]
	return ok
}

// partySnapshot returns the current members except skipKey, copied so the caller
// can DM them without holding the lock.
func (m *Manager) partySnapshot(skipKey string) []partyMember {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]partyMember, 0, len(m.party))
	for k, mem := range m.party {
		if k != skipKey {
			out = append(out, mem)
		}
	}
	return out
}

// bridgeTarget is a public channel the partyline is echoed into.
type bridgeTarget struct {
	network string
	channel string
}

// setBridge points the partyline at a public channel (nil clears it). Returns the
// previous target, so the caller can report what changed.
func (m *Manager) setBridge(b *bridgeTarget) *bridgeTarget {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.bridge
	m.bridge = b
	return prev
}

// bridgeOf returns the current bridge target, or nil.
func (m *Manager) bridgeOf() *bridgeTarget {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bridge
}

// relayLocal DMs an already-formatted line to every local member except skipKey,
// and echoes it into the bridged public channel when one is set.
//
// This is the single choke point every partyline line passes through — chat,
// join/leave notices, and lines arriving from other bots over the bus — so the
// bridge is wired here rather than at each call site. The bridge is deliberately
// one-way: public channel chatter never flows back onto the partyline, so there's
// no loop and nothing said in channel is mistaken for an admin's word.
func (m *Manager) relayLocal(line, skipKey string) {
	for _, mem := range m.partySnapshot(skipKey) {
		m.ctl.Say(mem.network, mem.target, line)
	}
	if b := m.bridgeOf(); b != nil {
		m.ctl.Say(b.network, b.channel, line)
	}
}

// sendPartyline relays a member's line to local members and publishes it to the
// bus for the other bots. We relay locally directly (rather than via bus
// loopback) so behavior doesn't depend on the bus echoing to its own publisher.
func (m *Manager) sendPartyline(msg engine.Message, text string) {
	if text == "" {
		return
	}
	self := partyKey(msg.Network, msg.Nick)
	m.relayLocal(partyLine(msg.Nick, text), self)
	m.publish(botnet.Event{Type: botnet.EventPartyline, Network: msg.Network, Nick: msg.Nick, Text: text})
}

// announceParty broadcasts a system notice (joins/leaves) to the partyline.
func (m *Manager) announceParty(msg engine.Message, text string) {
	self := partyKey(msg.Network, msg.Nick)
	m.relayLocal("* "+text, self)
	m.publish(botnet.Event{Type: botnet.EventPartyline, Network: msg.Network, Nick: "*", Text: text})
}

// onPartyline relays a partyline event from ANOTHER bot to local members. Our own
// echoes are dropped (we already relayed locally when we sent).
func (m *Manager) onPartyline(e botnet.Event) {
	if e.From == m.bot {
		return
	}
	m.relayLocal(partyLine(e.Nick, e.Text), partyKey(e.Network, e.Nick))
}
