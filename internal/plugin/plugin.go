// Package plugin is annoybots' eggdrop-style scripting layer: drop a Lua script
// in the plugins dir and it can register command "binds" the way eggdrop TCL did.
// A script calls bind("pub", "!hello", fn) at load time; when someone types that
// command in a channel, the bot calls fn with the message and the script can
// reply. Scripts are operator-supplied (trusted) but run in a Lua state with the
// filesystem/OS libraries left out, so a typo can't read the host.
package plugin

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// bind is one registered command handler from a script.
type bind struct {
	kind    string // "pub" (channel) or "msg" (DM)
	trigger string // first-word command token, lowercased (e.g. "!hello")
	fn      *lua.LFunction
}

// Manager owns the Lua state and the registered binds. A single LState is shared
// across all transports, so every entry point serializes on mu (gopher-lua is not
// goroutine-safe).
type Manager struct {
	out engine.Sender
	log *slog.Logger

	mu    sync.Mutex
	L     *lua.LState
	binds []bind

	// reply target for the in-flight callback, set under mu before each PCall.
	curNet, curChan string
}

// safeLibs are the only standard libraries opened — no os, io, package, or debug,
// so a script can't touch the filesystem or shell out.
var safeLibs = []struct {
	name string
	fn   lua.LGFunction
}{
	{lua.BaseLibName, lua.OpenBase},
	{lua.TabLibName, lua.OpenTable},
	{lua.StringLibName, lua.OpenString},
	{lua.MathLibName, lua.OpenMath},
}

// New builds an empty plugin Manager. Call Load to bring scripts in.
func New(out engine.Sender, log *slog.Logger) *Manager {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	for _, lib := range safeLibs {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	// Even within base, drop the file-loading builtins.
	for _, g := range []string{"dofile", "loadfile", "load", "loadstring"} {
		L.SetGlobal(g, lua.LNil)
	}

	m := &Manager{out: out, log: log, L: L}
	L.SetGlobal("bind", L.NewFunction(m.lbind))
	L.SetGlobal("reply", L.NewFunction(m.lreply))
	L.SetGlobal("say", L.NewFunction(m.lsay))
	return m
}

// Load reads and runs every *.lua file in dir (sorted), registering their binds.
// A script that fails to load is logged and skipped — one bad plugin can't take
// the bot down. Returns the number of scripts loaded.
func (m *Manager) Load(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		m.log.Warn("plugin dir unreadable", "dir", dir, "err", err)
		return 0
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".lua") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)

	m.mu.Lock()
	defer m.mu.Unlock()
	loaded := 0
	for _, f := range files {
		if err := m.L.DoFile(f); err != nil {
			m.log.Warn("plugin load failed", "file", f, "err", err)
			continue
		}
		loaded++
	}
	m.log.Info("plugins loaded", "scripts", loaded, "binds", len(m.binds))
	return loaded
}

// Handle dispatches a message to any matching binds. Returns true if at least one
// bind claimed it (so the caller stops further processing).
func (m *Manager) Handle(msg engine.Message) bool {
	if msg.Text == "" {
		return false
	}
	fields := strings.Fields(msg.Text)
	if len(fields) == 0 {
		return false
	}
	cmd := strings.ToLower(fields[0])
	want := "pub"
	if msg.Private {
		want = "msg"
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	matched := false
	for _, b := range m.binds {
		if b.kind != want || b.trigger != cmd {
			continue
		}
		matched = true
		m.curNet, m.curChan = msg.Network, msg.Channel
		m.L.Push(b.fn)
		m.L.Push(m.eventTable(msg, fields))
		if err := m.L.PCall(1, 0, nil); err != nil {
			m.log.Warn("plugin callback failed", "trigger", b.trigger, "err", err)
		}
	}
	m.curNet, m.curChan = "", ""
	return matched
}

// eventTable builds the Lua table passed to a bind callback. Caller holds mu.
func (m *Manager) eventTable(msg engine.Message, fields []string) *lua.LTable {
	ev := m.L.NewTable()
	ev.RawSetString("nick", lua.LString(msg.Nick))
	ev.RawSetString("chan", lua.LString(msg.Channel))
	ev.RawSetString("network", lua.LString(msg.Network))
	ev.RawSetString("text", lua.LString(msg.Text))
	args := m.L.NewTable()
	for i, a := range fields[1:] {
		args.RawSetInt(i+1, lua.LString(a))
	}
	ev.RawSetString("args", args)
	return ev
}

// Close releases the Lua state.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.L.Close()
}

// --- Lua-callable globals (all invoked under mu, on the LState goroutine) ---

func (m *Manager) lbind(L *lua.LState) int {
	kind := strings.ToLower(L.CheckString(1))
	if kind != "pub" && kind != "msg" {
		L.ArgError(1, `bind kind must be "pub" or "msg"`)
		return 0
	}
	m.binds = append(m.binds, bind{
		kind:    kind,
		trigger: strings.ToLower(L.CheckString(2)),
		fn:      L.CheckFunction(3),
	})
	return 0
}

func (m *Manager) lreply(L *lua.LState) int {
	if text := L.CheckString(1); text != "" && m.curNet != "" {
		m.out.Say(m.curNet, m.curChan, text)
	}
	return 0
}

func (m *Manager) lsay(L *lua.LState) int {
	m.out.Say(L.CheckString(1), L.CheckString(2), L.CheckString(3))
	return 0
}
