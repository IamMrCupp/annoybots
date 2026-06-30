package rpgweb

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	for _, want := range []string{"top idlers", "alice", "bob", "No quest underway",
		"heroes", "monsters slain", "bosses felled", `class="nav"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page missing %q\n%s", want, body)
		}
	}
}

func TestIndexShowsActivityFeed(t *testing.T) {
	st := state.NewMem()
	// a real Manager records drama into the feed the dashboard reads.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := idlerpg.New(st, noopSender{}, nil, time.Second, time.Second, time.Hour, time.Hour, log)
	m.Handle(engine.Message{Network: "net", Channel: "#c", Nick: "alice", Text: "!rpg"})
	m.Tick() // level-up → a feed entry

	rr := httptest.NewRecorder()
	New(st).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rr.Body.String()
	for _, want := range []string{"realm activity", "attained level"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q\n%s", want, body)
		}
	}
}

func TestIndexShowsMapQuest(t *testing.T) {
	st := state.NewMem()
	// A map quest exactly as internal/idlerpg persists one (key "rpg:quest").
	blob := `{"kind":"map","net":"net","chan":"#c","members":{"net|alice":"alice"},` +
		`"desc":"recover the lost socks","x":50,"y":60,"x1":200,"y1":220,"x2":400,"y2":450,"stage":1}`
	if err := st.SetStr(context.Background(), "rpg:quest", blob); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	New(st).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rr.Body.String()
	for _, want := range []string{"<svg", `class="party"`, "leg 2 of 2", "recover the lost socks"} {
		if !strings.Contains(body, want) {
			t.Fatalf("map quest page missing %q\n%s", want, body)
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

func TestCharPage(t *testing.T) {
	st := state.NewMem()
	seed(st) // enrolls net|alice, net|bob

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/p/"+url.PathEscape("net|alice"), nil)
	New(st).Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("char page status = %d; want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"alice", "level", "abilities", "STR", "equipment", "back to the realm"} {
		if !strings.Contains(body, want) {
			t.Fatalf("char page missing %q\n%s", want, body)
		}
	}

	// An unknown character is a 404.
	rr2 := httptest.NewRecorder()
	New(st).Handler().ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/p/"+url.PathEscape("net|ghost"), nil))
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("unknown char = %d; want 404", rr2.Code)
	}
}

func TestWorldMapPage(t *testing.T) {
	st := state.NewMem()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := idlerpg.New(st, noopSender{}, nil, time.Second, time.Second, time.Hour, time.Hour, log)
	m.Handle(engine.Message{Network: "net", Channel: "#c", Nick: "alice", Text: "!rpg"})
	m.Tick() // places alice on the map

	rr := httptest.NewRecorder()
	New(st).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/map", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("map status = %d; want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"<svg", "alice", "Idlecrest", "back to the realm", "coast · market", "mountain · inn"} {
		if !strings.Contains(body, want) {
			t.Fatalf("world map missing %q\n%s", want, body)
		}
	}
}

func TestHealthz(t *testing.T) {
	rr := httptest.NewRecorder()
	New(state.NewMem()).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "ok" {
		t.Fatalf("healthz = %d %q", rr.Code, rr.Body.String())
	}
}

func TestHelpPage(t *testing.T) {
	rr := httptest.NewRecorder()
	New(state.NewMem()).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/help", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("help page status = %d; want 200", rr.Code)
	}
	body := rr.Body.String()
	// public commands + the admin section + the play explanation all render.
	for _, want := range []string{"how to play", "!rpg status", "!rpg duel", "Admin", "reset all yes", "back to the realm"} {
		if !strings.Contains(body, want) {
			t.Fatalf("help page missing %q", want)
		}
	}
}

func TestUnknownPath404(t *testing.T) {
	rr := httptest.NewRecorder()
	New(state.NewMem()).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown path = %d; want 404", rr.Code)
	}
}
