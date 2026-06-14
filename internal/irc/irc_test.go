package irc

import (
	"io"
	"log/slog"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/config"
)

func TestIsChannel(t *testing.T) {
	cases := map[string]bool{
		"#chan":     true,
		"&local":    true,
		"+modeless": true,
		"!12345foo": true,
		"somenick":  false,
		"":          false,
	}
	for in, want := range cases {
		if got := isChannel(in); got != want {
			t.Errorf("isChannel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewConnNickServIdentify(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	getenv := func(k string) string {
		if k == "EMP_SASL" {
			return "s3cret"
		}
		return ""
	}
	cases := []struct {
		name string
		net  config.Network
		want string
	}{
		{"irc with nickserv pass", config.Network{Name: "emp", NickServPassEnv: "EMP_SASL"}, "s3cret"},
		{"irc without nickserv pass", config.Network{Name: "emp"}, ""},
		{"twitch ignores nickserv", config.Network{Name: "tw", Kind: "twitch", NickServPassEnv: "EMP_SASL"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newConn(tc.net, log, getenv)
			if c.nickservPass != tc.want {
				t.Errorf("nickservPass = %q, want %q", c.nickservPass, tc.want)
			}
		})
	}
}

func TestHostOnly(t *testing.T) {
	cases := map[string]string{
		"irc.chat.twitch.tv:6697": "irc.chat.twitch.tv",
		"irc.libera.chat:6697":    "irc.libera.chat",
		"localhost":               "localhost",
	}
	for in, want := range cases {
		if got := hostOnly(in); got != want {
			t.Errorf("hostOnly(%q) = %q, want %q", in, got, want)
		}
	}
}
