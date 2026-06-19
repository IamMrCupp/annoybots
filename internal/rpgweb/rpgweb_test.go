package rpgweb

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/idlerpg"
	"github.com/IamMrCupp/annoybots/internal/state"
)

type noopSender struct{}

func (noopSender) Say(_, _, _ string)    {}
func (noopSender) Action(_, _, _ string) {}

// seed enrolls a couple of players into store via a real idlerpg Manager, so the
// dashboard reads exactly what the bot writes (no hand-built keys).
func seed(st state.Store) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := idlerpg.New(st, noopSender{}, nil, time.Second, time.Second, time.Hour, time.Hour, log)
	msg := func(nick string) engine.Message {
		return engine.Message{Network: "net", Channel: "#c", Nick: nick, Text: "!rpg"}
	}
	m.Handle(msg("alice"))
	m.Handle(msg("bob"))
}

func TestIndexShowsPlayers(t *testing.T) {
	st := state.NewMem()
	seed(st)

	rr := httptest.NewRecorder()
	New(st).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"top idlers", "alice", "bob", "No quest underway"} {
		if !strings.Contains(body, want) {
			t.Fatalf("page missing %q\n%s", want, body)
		}
	}
}

func TestIndexEmptyRealm(t *testing.T) {
	rr := httptest.NewRecorder()
	New(state.NewMem()).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "No idlers yet") {
		t.Fatalf("empty realm should say so, got:\n%s", rr.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	rr := httptest.NewRecorder()
	New(state.NewMem()).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "ok" {
		t.Fatalf("healthz = %d %q", rr.Code, rr.Body.String())
	}
}

func TestUnknownPath404(t *testing.T) {
	rr := httptest.NewRecorder()
	New(state.NewMem()).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown path = %d; want 404", rr.Code)
	}
}
