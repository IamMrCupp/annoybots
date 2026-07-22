package botnet

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Bot-to-bot link authentication. The bus is Redis pub/sub, and every consumer
// acts on what arrives — including EventAdminAdd, which grants admin flags. So
// anyone able to publish to the channel could otherwise make themselves an owner
// on every bot at once.
//
// With a shared secret configured, each event carries an HMAC over its canonical
// JSON plus a timestamp; receivers drop anything that doesn't verify or that has
// aged out. With no secret the bus behaves exactly as before, so this can be
// rolled out one bot at a time.

// authWindow is how far an event's timestamp may drift from our clock before we
// treat it as a replay. Generous enough for federated hosts with loose NTP.
const authWindow = 5 * time.Minute

// signEvent returns the hex HMAC-SHA256 of e with its signature field cleared,
// so signing and verifying always hash the same bytes.
func signEvent(e Event, secret []byte) string {
	e.Sig = ""
	data, err := json.Marshal(e)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyEvent reports whether e carries a valid, fresh signature for secret.
func verifyEvent(e Event, secret []byte, now time.Time) bool {
	if e.Sig == "" {
		return false
	}
	want := signEvent(e, secret)
	if want == "" || !hmac.Equal([]byte(want), []byte(e.Sig)) {
		return false
	}
	// A valid signature on an ancient event is a replay; bound how long one is
	// worth anything.
	if e.Ts == 0 {
		return false
	}
	drift := now.Unix() - e.Ts
	if drift < 0 {
		drift = -drift
	}
	return time.Duration(drift)*time.Second <= authWindow
}
