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

	mu    sync.Mutex
	chans map[string]*chanState
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
	}
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
func (k *chankeeper) onNames(channel, names string) {
	k.mu.Lock()
	defer k.mu.Unlock()
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

// onEndNames evaluates a channel once its NAMES list is complete.
func (k *chankeeper) onEndNames(channel string) { k.enforce(channel) }

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
