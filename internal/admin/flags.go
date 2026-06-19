package admin

import (
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Access flags, eggdrop-style. A holder's flag grants itself AND everything to
// its right in the hierarchy: owner > master > op > voice > friend.
//
//	n owner   — full control, incl. managing admins
//	m master  — channels, reload, admin list
//	o op      — puppet (!say/!act), quote editing
//	v voice   — (reserved for channel-keep auto-voice)
//	f friend  — partyline, help; "known/trusted"
const (
	flagOwner  = 'n'
	flagMaster = 'm'
	flagOp     = 'o'
	flagVoice  = 'v'
	flagFriend = 'f'
)

// flagOrder lists flags most-powerful first; index = rank (lower = stronger).
const flagOrder = "nmovf"

func flagRank(f byte) int { return strings.IndexByte(flagOrder, f) }

// hasFlag reports whether flag-set `have` grants `want` via the hierarchy.
func hasFlag(have string, want byte) bool {
	wr := flagRank(want)
	if wr < 0 {
		return false
	}
	for i := 0; i < len(have); i++ {
		if r := flagRank(have[i]); r >= 0 && r <= wr {
			return true // holds `want` or something stronger
		}
	}
	return false
}

// normalizeFlags keeps only known flag chars; falls back to def when empty.
func normalizeFlags(f, def string) string {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		if flagRank(f[i]) >= 0 && strings.IndexByte(b.String(), f[i]) < 0 {
			b.WriteByte(f[i])
		}
	}
	if b.Len() == 0 {
		return def
	}
	return b.String()
}

// flagsFor returns the sender's effective flags: their configured/runtime admin
// flags, master for an active password session, else "".
func (m *Manager) flagsFor(msg engine.Message) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 1. Verified account — the strong path.
	if msg.Account != "" {
		if f, ok := m.admins[key(msg.Network, msg.Account)]; ok {
			return f
		}
		if f, ok := m.admins[key("", msg.Account)]; ok {
			return f
		}
	}
	// 2. Hostmask match — for services-less networks (no account-tag/SASL).
	if msg.Host != "" {
		hm := hostmask(msg)
		for _, a := range m.maskAdmins {
			if a.network != "" && !strings.EqualFold(a.network, msg.Network) {
				continue
			}
			if wildcardMatchFold(a.mask, hm) {
				return a.flags
			}
		}
	}
	// 3. Host-bound !login session — the weakest fallback.
	if exp, ok := m.sessions[sessKey(msg)]; ok && m.now().Before(exp) {
		return string(flagMaster)
	}
	return ""
}

// has reports whether the sender holds at least `flag`.
func (m *Manager) has(msg engine.Message, flag byte) bool {
	return hasFlag(m.flagsFor(msg), flag)
}

// cmdFlags maps each admin command to the minimum flag it needs. Unlisted
// commands default to master.
var cmdFlags = map[string]byte{
	"!help": flagFriend, "!admin": flagFriend,
	"!party": flagFriend, "!unparty": flagFriend, "!networks": flagFriend,
	"!say": flagOp, "!act": flagOp,
	"!addquote": flagOp, "!delquote": flagOp,
	"!join": flagMaster, "!part": flagMaster, "!invite": flagMaster,
	"!admins": flagMaster, "!reload": flagMaster,
	"!addadmin": flagOwner, "!deladmin": flagOwner,
}

func requiredFlag(cmd string) byte {
	if f, ok := cmdFlags[cmd]; ok {
		return f
	}
	return flagMaster
}
