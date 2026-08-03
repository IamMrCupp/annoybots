package irc

import "testing"

func TestRejoinTargetOnlyWhenWeAreKickedFromAHomeChannel(t *testing.T) {
	home := []string{"#tns", "#secret pass123", "#Ops"}

	cases := []struct {
		name                  string
		kicked, self, channel string
		want                  string
	}{
		{"we are kicked from a home channel", "arwyen", "arwyen", "#tns", "#tns"},
		{"case-insensitive nick and channel", "ARWYEN", "arwyen", "#TNS", "#tns"},
		{"someone else was kicked", "troublemaker", "arwyen", "#tns", ""},
		{"kicked from a channel we don't hold", "arwyen", "arwyen", "#random", ""},
		{"keyed channel rejoins with its key preserved", "arwyen", "arwyen", "#secret", "#secret pass123"},
		{"channel name matched despite differing case in config", "arwyen", "arwyen", "#ops", "#Ops"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rejoinTarget(c.kicked, c.self, c.channel, home); got != c.want {
				t.Fatalf("rejoinTarget(%q,%q,%q) = %q; want %q", c.kicked, c.self, c.channel, got, c.want)
			}
		})
	}
}

func TestChannelName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"#tns", "#tns"},
		{"#secret key", "#secret"},
		{"  #padded  key  ", "#padded"},
		{"", ""},
	} {
		if got := channelName(c.in); got != c.want {
			t.Fatalf("channelName(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
