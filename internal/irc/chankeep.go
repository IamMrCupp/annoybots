package irc

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/IamMrCupp/annoybots/internal/cooldown"
)

// chankeeper implements eggdrop-style channel keeping for services-less nets: the
// bot, once opped, keeps its sibling bots (and configured "protect" nicks) opped —
// auto-opping them on join, re-opping them if a non-authorized user deops them.
// It tracks per-channel membership and op state from NAMES/JOIN/PART/QUIT/MODE so
// it only acts on users actually present, and only when the bot itself holds ops.
type chankeeper struct {
	protected map[string]bool                  // lowercased nicks to keep opped (siblings + protect list)
	selfNick  func() string                    // the bot's current nick on this network
	send      func(channel, modes, arg string) // send a MODE change
	cool      *cooldown.Manager                // per-(channel,nick) op cooldown, anti-flood
	log       *slog.Logger

	mu     sync.Mutex
	chans  map[string]*chanState
	naming map[string]bool // channels with a NAMES burst (353s) currently open
}

type chanState struct {
	members map[string]bool // present nicks (lower)
	ops     map[string]bool // opped nicks (lower)
}

const opCooldown = 5 * time.Second

func newChankeeper(protect []string, selfNick func() string, send func(channel, modes, arg string), log *slog.Logger) *chankeeper {
	p := make(map[string]bool, len(protect))
	for _, n := range protect {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			p[n] = true
		}
	}
	return &chankeeper{
		protected: p,
		selfNick:  selfNick,
		send:      send,
		cool:      cooldown.New(),
		log:       log,
		chans:     make(map[string]*chanState),
		naming:    make(map[string]bool),
	}
}

// reset drops every channel's tracked membership and op state. Call it on
// (re)connect: after a disconnect we know nothing about who is present or opped,
// and carrying the old state forward is worse than having none — a stale
// ops[sibling]=true makes enforce skip the very nick it exists to op, and a
// stale ops[self]=true makes HoldsOp lie to the !op/!kick commands. The next
// NAMES burst re-seeds everything.
func (k *chankeeper) reset() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.chans = make(map[string]*chanState)
	k.naming = make(map[string]bool)
}

// forget drops one channel's state — for when we leave it ourselves (our own
// PART, or a KICK of us). Whatever happens there while we're gone is invisible
// to us, so the state is stale the moment we walk out.
func (k *chankeeper) forget(channel string) {
	c := strings.ToLower(channel)
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.chans, c)
	delete(k.naming, c)
}

func (k *chankeeper) ensure(channel string) *chanState {
	c := strings.ToLower(channel)
	s := k.chans[c]
	if s == nil {
		s = &chanState{members: map[string]bool{}, ops: map[string]bool{}}
		k.chans[c] = s
	}
	return s
}

// onNames records a RPL_NAMREPLY (353) line's members + ops. Prefixes @/&/~ mean
// op-or-better; +/% are voice/halfop (not op for our purposes).
//
// A NAMES burst *replaces* the channel's state rather than merging into it: the
// first 353 for a channel wipes what we had, the rest of the burst accumulates,
// and 366 closes it. Merging would let a nick who has since been deopped keep a
// stale ops entry forever, since 353 can only ever add.
func (k *chankeeper) onNames(channel, names string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	c := strings.ToLower(channel)
	if !k.naming[c] {
		k.naming[c] = true
		delete(k.chans, c)
	}
	s := k.ensure(channel)
	for _, raw := range strings.Fields(names) {
		opped := false
		i := 0
		for i < len(raw) && strings.IndexByte("@&~+%", raw[i]) >= 0 {
			if raw[i] == '@' || raw[i] == '&' || raw[i] == '~' {
				opped = true
			}
			i++
		}
		nick := strings.ToLower(raw[i:])
		if nick == "" {
			continue
		}
		s.members[nick] = true
		if opped {
			s.ops[nick] = true
		}
	}
}

// onEndNames closes the NAMES burst (RPL_ENDOFNAMES, 366) and evaluates the
// channel now that its membership list is complete.
func (k *chankeeper) onEndNames(channel string) {
	k.mu.Lock()
	delete(k.naming, strings.ToLower(channel))
	k.mu.Unlock()
	k.enforce(channel)
}

func (k *chankeeper) onJoin(channel, nick string) {
	k.mu.Lock()
	s := k.ensure(channel)
	s.members[strings.ToLower(nick)] = true
	k.mu.Unlock()
	k.enforce(channel)
}

func (k *chankeeper) onLeave(channel, nick string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if s := k.chans[strings.ToLower(channel)]; s != nil {
		delete(s.members, strings.ToLower(nick))
		delete(s.ops, strings.ToLower(nick))
	}
}

func (k *chankeeper) onQuit(nick string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := strings.ToLower(nick)
	for _, s := range k.chans {
		delete(s.members, n)
		delete(s.ops, n)
	}
}

func (k *chankeeper) onNick(oldNick, newNick string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	o, nw := strings.ToLower(oldNick), strings.ToLower(newNick)
	for _, s := range k.chans {
		if s.members[o] {
			delete(s.members, o)
			s.members[nw] = true
		}
		if s.ops[o] {
			delete(s.ops, o)
			s.ops[nw] = true
		}
	}
}

// onModeOp records a single +o/-o change and re-enforces.
func (k *chankeeper) onModeOp(channel, nick string, add bool) {
	k.mu.Lock()
	s := k.ensure(channel)
	n := strings.ToLower(nick)
	if add {
		s.ops[n] = true
	} else {
		delete(s.ops, n)
	}
	k.mu.Unlock()
	k.enforce(channel)
}

// HoldsOp reports whether the bot itself currently holds channel-operator status
// in the channel — the precondition for granting anyone else ops.
func (k *chankeeper) HoldsOp(channel string) bool {
	self := strings.ToLower(k.selfNick())
	k.mu.Lock()
	defer k.mu.Unlock()
	s := k.chans[strings.ToLower(channel)]
	return s != nil && s.ops[self]
}

// enforce ops any present, un-opped protected nick — but only while the bot holds
// ops itself. Each op is rate-limited per (channel, nick) to avoid op wars.
func (k *chankeeper) enforce(channel string) {
	self := strings.ToLower(k.selfNick())
	k.mu.Lock()
	s := k.chans[strings.ToLower(channel)]
	if s == nil || !s.ops[self] {
		k.mu.Unlock()
		return // we're not opped — nothing we can do
	}
	var toOp []string
	for nick := range s.members {
		if nick == self || !k.protected[nick] || s.ops[nick] {
			continue
		}
		toOp = append(toOp, nick)
	}
	k.mu.Unlock()

	for _, nick := range toOp {
		if k.cool.Use("op:"+strings.ToLower(channel)+":"+nick, opCooldown) {
			k.send(channel, "+o", nick)
			k.log.Info("chankeep: opping protected nick", "channel", channel, "nick", nick)
		}
	}
}
