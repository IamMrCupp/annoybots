package idlerpg

import (
	"strings"
	"testing"
)

func TestHelpListsCommands(t *testing.T) {
	m, r, _ := newMgr()
	m.Handle(chanMsg("alice", "!rpg help"))
	joined := strings.Join(r.lines, "\n")
	for _, want := range []string{"!rpg status", "!rpg duel", "quaff", "/help"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("help missing %q in:\n%s", want, joined)
		}
	}
	// a non-admin must NOT see the admin verbs
	if strings.Contains(joined, "reset all yes") {
		t.Fatalf("non-admin help leaked admin verbs:\n%s", joined)
	}
}

func TestHelpShowsAdminVerbsToAdmins(t *testing.T) {
	m, r, _ := newMgr()
	m.SetAuthz(allowAll)
	m.Handle(chanMsg("boss", "!rpg help"))
	if !strings.Contains(strings.Join(r.lines, "\n"), "reset all yes") {
		t.Fatalf("admin help should include the privileged verbs, got %v", r.lines)
	}
}

func TestCommandHelpSourceShared(t *testing.T) {
	// every public command line is non-empty and starts with !rpg
	for _, g := range CommandHelp() {
		if g.Title == "" || len(g.Items) == 0 {
			t.Fatalf("empty help group: %+v", g)
		}
		for _, it := range g.Items {
			if !strings.HasPrefix(it.Cmd, "!rpg") || it.Desc == "" {
				t.Fatalf("bad help item: %+v", it)
			}
		}
	}
	if len(AdminHelp().Items) == 0 {
		t.Fatal("admin help is empty")
	}
}
