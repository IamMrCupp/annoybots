package bot

import (
	"context"
	"testing"
)

// fakeTransport records what it was asked to send and which networks it owns.
type fakeTransport struct {
	networks  []string
	sent      []string
	connected bool
}

func (f *fakeTransport) Say(network, target, text string) {
	f.sent = append(f.sent, "SAY "+network+" "+target+" "+text)
}
func (f *fakeTransport) Action(network, target, text string) {
	f.sent = append(f.sent, "ACT "+network+" "+target+" "+text)
}
func (f *fakeTransport) Join(network, channel string) {
	f.sent = append(f.sent, "JOIN "+network+" "+channel)
}
func (f *fakeTransport) Part(network, channel string) {
	f.sent = append(f.sent, "PART "+network+" "+channel)
}
func (f *fakeTransport) Invite(network, nick, channel string) {
	f.sent = append(f.sent, "INVITE "+network+" "+nick+" "+channel)
}
func (f *fakeTransport) Identify(network, password string) bool {
	f.sent = append(f.sent, "IDENTIFY "+network+" "+password)
	return true
}
func (f *fakeTransport) Networks() []string  { return f.networks }
func (f *fakeTransport) Run(context.Context) {}
func (f *fakeTransport) Quit()               {}
func (f *fakeTransport) Wait()               {}
func (f *fakeTransport) AnyConnected() bool  { return f.connected }
func (f *fakeTransport) NetworkStatus() map[string]bool {
	out := map[string]bool{}
	for _, n := range f.networks {
		out[n] = f.connected
	}
	return out
}

func TestRouterDispatchesByNetwork(t *testing.T) {
	ircT := &fakeTransport{networks: []string{"libera", "testnet"}}
	discordT := &fakeTransport{networks: []string{"discord-main"}}

	r := NewRouter()
	r.Add(ircT)
	r.Add(discordT)

	r.Say("libera", "#chan", "hi there")
	r.Action("discord-main", "12345", "waves")
	r.Invite("testnet", "bob", "#secret")
	r.Say("nonexistent", "#x", "dropped") // unknown network: no panic, no send

	if len(ircT.sent) != 2 || ircT.sent[0] != "SAY libera #chan hi there" || ircT.sent[1] != "INVITE testnet bob #secret" {
		t.Fatalf("irc transport got %#v", ircT.sent)
	}
	if len(discordT.sent) != 1 || discordT.sent[0] != "ACT discord-main 12345 waves" {
		t.Fatalf("discord transport got %#v", discordT.sent)
	}
}

// opTransport is a transport that also implements engine.Opper.
type opTransport struct {
	fakeTransport
	held bool // whether it "holds ops" (what Op returns)
	ops  []string
}

func (o *opTransport) Op(network, channel, nick string) bool {
	o.ops = append(o.ops, network+"|"+channel+"|"+nick)
	return o.held
}

func TestRouterOpRoutesToOpperOnly(t *testing.T) {
	opped := &opTransport{fakeTransport: fakeTransport{networks: []string{"irc"}}, held: true}
	plain := &fakeTransport{networks: []string{"discord"}} // no Op method
	r := NewRouter()
	r.Add(opped)
	r.Add(plain)

	if !r.Op("irc", "#chan", "boss") {
		t.Fatal("an opped Opper transport should grant and return true")
	}
	if len(opped.ops) != 1 || opped.ops[0] != "irc|#chan|boss" {
		t.Fatalf("expected Op(irc,#chan,boss), got %v", opped.ops)
	}
	// A transport that doesn't implement Opper (Discord) returns false, no panic.
	if r.Op("discord", "#chan", "boss") {
		t.Fatal("a non-Opper transport must return false")
	}
	// Unknown network: false, no panic.
	if r.Op("nope", "#chan", "boss") {
		t.Fatal("unknown network must return false")
	}
	// An Opper that doesn't currently hold ops returns false.
	opped.held = false
	if r.Op("irc", "#chan", "boss") {
		t.Fatal("an un-opped Opper should return false")
	}
}

func TestRouterAnyConnected(t *testing.T) {
	a := &fakeTransport{networks: []string{"a"}, connected: false}
	b := &fakeTransport{networks: []string{"b"}, connected: false}
	r := NewRouter()
	r.Add(a)
	r.Add(b)
	if r.AnyConnected() {
		t.Fatal("expected not connected")
	}
	b.connected = true
	if !r.AnyConnected() {
		t.Fatal("expected connected once one transport is up")
	}
}
