package irc

import "testing"

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
