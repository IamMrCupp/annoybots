package admin

import (
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// maskAdmin authorizes by IRC hostmask instead of a services account — the
// eggdrop model, for networks that don't expose accounts (no account-tag/SASL).
// A stable cloak or static IP makes this secure; see plan-admin-auth-hardening.md.
type maskAdmin struct {
	network string // "" = any network
	mask    string // glob: nick!ident@host
	flags   string
}

// hostmask renders the sender's full IRC mask for matching.
func hostmask(msg engine.Message) string {
	return msg.Nick + "!" + msg.Ident + "@" + msg.Host
}

// sessKey binds a !login session to nick AND ident@host, so a stolen nick from a
// different host can't inherit an active session. Off-IRC (no host) it degrades
// to nick-scoped, which is fine there (account-based auth is used instead).
func sessKey(msg engine.Message) string {
	return key(msg.Network, msg.Nick) + "|" + strings.ToLower(msg.Ident+"@"+msg.Host)
}

// wildcardMatchFold reports whether glob `pattern` (only '*' and '?' are special)
// matches s, case-insensitively. A dedicated matcher avoids path.Match's special
// handling of '[' etc., which appear in some hostmasks.
func wildcardMatchFold(pattern, s string) bool {
	return wildcard(strings.ToLower(pattern), strings.ToLower(s))
}

func wildcard(p, s string) bool {
	star, ss, pi, si := -1, 0, 0, 0
	for si < len(s) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == s[si]):
			pi++
			si++
		case pi < len(p) && p[pi] == '*':
			star, ss = pi, si
			pi++
		case star >= 0:
			pi = star + 1
			ss++
			si = ss
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
