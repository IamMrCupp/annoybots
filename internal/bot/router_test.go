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
func (f *fakeTransport) Networks() []string  { return f.networks }
func (f *fakeTransport) Run(context.Context) {}
func (f *fakeTransport) Quit()               {}
func (f *fakeTransport) Wait()               {}
func (f *fakeTransport) AnyConnected() bool  { return f.connected }

func TestRouterDispatchesByNetwork(t *testing.T) {
	ircT := &fakeTransport{networks: []string{"libera", "testnet"}}
	discordT := &fakeTransport{networks: []string{"discord-main"}}

	r := NewRouter()
	r.Add(ircT)
	r.Add(discordT)

	r.Say("libera", "#chan", "hi there")
	r.Action("discord-main", "12345", "waves")
	r.Say("nonexistent", "#x", "dropped") // unknown network: no panic, no send

	if len(ircT.sent) != 1 || ircT.sent[0] != "SAY libera #chan hi there" {
		t.Fatalf("irc transport got %#v", ircT.sent)
	}
	if len(discordT.sent) != 1 || discordT.sent[0] != "ACT discord-main 12345 waves" {
		t.Fatalf("discord transport got %#v", discordT.sent)
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
