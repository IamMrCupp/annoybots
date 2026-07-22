package botnet

import (
	"testing"
	"time"
)

var testNow = time.Unix(1_700_000_000, 0)

func signed(e Event, secret string, at time.Time) Event {
	e.Ts = at.Unix()
	e.Sig = signEvent(e, []byte(secret))
	return e
}

func TestSignedEventVerifies(t *testing.T) {
	e := signed(Event{Type: EventPartyline, From: "arywen", Text: "hello"}, "s3cret", testNow)
	if !verifyEvent(e, []byte("s3cret"), testNow) {
		t.Fatal("a freshly signed event should verify")
	}
}

func TestForgedAndTamperedEventsAreRejected(t *testing.T) {
	secret := []byte("s3cret")

	// unsigned — the pre-auth shape, which is exactly what an attacker would send
	if verifyEvent(Event{Type: EventAdminAdd, Account: "mallory", Flags: "n"}, secret, testNow) {
		t.Fatal("an unsigned event must never verify")
	}
	// signed with the wrong secret
	e := signed(Event{Type: EventAdminAdd, Account: "mallory", Flags: "n"}, "wrong", testNow)
	if verifyEvent(e, secret, testNow) {
		t.Fatal("a differently-keyed signature must not verify")
	}
	// signed correctly, then tampered with — the privilege-escalation case
	good := signed(Event{Type: EventAdminAdd, Account: "alice", Flags: "f"}, "s3cret", testNow)
	tampered := good
	tampered.Flags = "n" // promote friend -> owner
	if verifyEvent(tampered, secret, testNow) {
		t.Fatal("altering a signed event must invalidate it")
	}
	tampered2 := good
	tampered2.Account = "mallory"
	if verifyEvent(tampered2, secret, testNow) {
		t.Fatal("altering the account must invalidate it")
	}
}

func TestStaleEventsAreRejected(t *testing.T) {
	secret := []byte("s3cret")
	e := signed(Event{Type: EventPartyline, Text: "replay me"}, "s3cret", testNow)

	if !verifyEvent(e, secret, testNow.Add(authWindow-time.Second)) {
		t.Fatal("an event inside the window should still verify")
	}
	if verifyEvent(e, secret, testNow.Add(authWindow+time.Second)) {
		t.Fatal("a captured event must expire, so it can't be replayed forever")
	}
	// clock skew in either direction is treated the same
	if verifyEvent(e, secret, testNow.Add(-(authWindow + time.Second))) {
		t.Fatal("an event from too far in the future must be rejected too")
	}
	if !verifyEvent(e, secret, testNow.Add(-(authWindow - time.Second))) {
		t.Fatal("modest negative skew should be tolerated")
	}
	// a signature with no timestamp can't be aged, so it's refused
	noTs := Event{Type: EventPartyline, Text: "x"}
	noTs.Sig = signEvent(noTs, secret)
	if verifyEvent(noTs, secret, testNow) {
		t.Fatal("a signed event without a timestamp must be rejected")
	}
}

func TestSignatureIgnoresAnyExistingSigField(t *testing.T) {
	// Signing must clear Sig first, or verification could never reproduce it.
	secret := []byte("s3cret")
	e := Event{Type: EventQuoteAdd, Pack: "p", Line: "l", Ts: testNow.Unix()}
	first := signEvent(e, secret)
	e.Sig = "garbage"
	if second := signEvent(e, secret); second != first {
		t.Fatal("signing must ignore any pre-existing Sig value")
	}
}
