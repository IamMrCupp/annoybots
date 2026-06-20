// Package rpgweb is a tiny read-only web view of the IdleRPG world. It reads the
// shared Redis state the bots already fill (rankings, the active quest) and
// renders a single status page — idlerpg.net's XPM map, reimagined as HTML over
// the F3 store. It never writes, so it can run anywhere with read access to Redis.
package rpgweb

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/IamMrCupp/annoybots/internal/idlerpg"
	"github.com/IamMrCupp/annoybots/internal/state"
)

const boardSize = 25

const worldDots = 200 // most players to plot on the world map

// Server renders the dashboard from a read-only view of the state store.
type Server struct {
	store    state.Store
	tmpl     *template.Template
	charTmpl *template.Template
	mapTmpl  *template.Template
	now      func() time.Time
}

var tmplFuncs = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"dur": func(secs int64) string {
		if secs < 0 {
			secs = 0
		}
		return (time.Duration(secs) * time.Second).Round(time.Second).String()
	},
	"pathesc": url.PathEscape,
}

// New builds a Server over store.
func New(store state.Store) *Server {
	return &Server{
		store:    store,
		tmpl:     template.Must(template.New("index").Funcs(tmplFuncs).Parse(indexTmpl)),
		charTmpl: template.Must(template.New("char").Funcs(tmplFuncs).Parse(charTmpl)),
		mapTmpl:  template.Must(template.New("map").Funcs(tmplFuncs).Parse(mapTmpl)),
		now:      time.Now,
	}
}

// Handler returns the HTTP routes: the dashboard, per-character pages, and a probe.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/map", s.worldMap)
	mux.HandleFunc("/p/", s.char)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// pageData is the template's view model.
type pageData struct {
	Board     []idlerpg.CharView
	Quest     *idlerpg.QuestView
	QuestLeft string
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	board, err := idlerpg.ReadLeaderboard(ctx, s.store, boardSize)
	if err != nil {
		http.Error(w, "the realm is unreachable right now.", http.StatusServiceUnavailable)
		return
	}
	quest, _ := idlerpg.ReadQuest(ctx, s.store)

	data := pageData{Board: board, Quest: quest}
	if quest != nil && quest.Kind != "map" {
		data.QuestLeft = humanLeft(quest.Deadline - s.now().Unix())
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.tmpl.Execute(w, data)
}

// worldMap renders the persistent world map: every placed player + the towns.
func (s *Server) worldMap(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	world, err := idlerpg.ReadWorld(ctx, s.store, worldDots)
	if err != nil {
		http.Error(w, "the realm is unreachable right now.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.mapTmpl.Execute(w, world)
}

// char renders one character's sheet at /p/<key>.
func (s *Server) char(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/p/")
	key, err := url.PathUnescape(key)
	if key == "" || err != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	cv, ok := idlerpg.ReadChar(ctx, s.store, key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.charTmpl.Execute(w, cv)
}

// humanLeft renders a remaining-seconds count, clamped at zero.
func humanLeft(secs int64) string {
	if secs < 0 {
		secs = 0
	}
	return (time.Duration(secs) * time.Second).Round(time.Second).String()
}

const indexTmpl = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="30">
<title>annoybots · idle realm</title>
<style>
  :root { color-scheme: dark; }
  body { background:#0e0f13; color:#d6d8de; font:15px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace; margin:0; padding:2rem; }
  h1 { font-size:1.4rem; margin:0 0 1rem; color:#e9b949; letter-spacing:.04em; }
  h2 { font-size:1rem; color:#8aa0c6; margin:1.5rem 0 .5rem; }
  table { border-collapse:collapse; width:100%; max-width:760px; }
  th,td { text-align:left; padding:.35rem .75rem; border-bottom:1px solid #20232b; }
  th { color:#7c8290; font-weight:600; }
  tr:hover td { background:#15171d; }
  .rank { color:#7c8290; width:2.5rem; }
  .lvl { color:#7fd1a8; }
  .evil { color:#e06c75; } .good { color:#61afef; } .neutral { color:#abb2bf; }
  .quest { max-width:760px; background:#1a160c; border:1px solid #4a3c14; border-radius:8px; padding:1rem 1.25rem; margin:0 0 1rem; }
  .quest .obj { color:#e9b949; }
  .map { display:block; width:100%; max-width:420px; margin:1rem 0 .25rem; }
  .map-bg { fill:#0e0f13; stroke:#2a2f3a; stroke-width:2; }
  .leg { stroke-width:3; fill:none; }
  .leg1 { stroke:#61afef; }
  .leg2 { stroke:#4a3c14; stroke-dasharray:10 8; }
  .wp { fill:#3a3f4b; stroke:#8aa0c6; stroke-width:2; }
  .wp-end { stroke:#e9b949; }
  .party { fill:#7fd1a8; stroke:#0e0f13; stroke-width:3; }
  .muted { color:#6b7280; }
  a { color:#7fd1a8; text-decoration:none; }
  a:hover { text-decoration:underline; }
  footer { margin-top:2rem; color:#4b5563; font-size:.8rem; }
</style>
</head>
<body>
<h1>⚔ the idle realm</h1>
<p class="muted"><a href="/map">🗺 the realm map</a> — see where everyone's wandering.</p>
{{if .Quest}}
<div class="quest">
  <strong>A quest is underway.</strong>
  {{if eq .Quest.Kind "map"}}<span class="muted">(leg {{add .Quest.Stage 1}} of 2)</span>
  {{else}}<span class="muted">({{.QuestLeft}} left)</span>{{end}}<br>
  {{range $i, $m := .Quest.Members}}{{if $i}}, {{end}}{{$m}}{{end}}
  must <span class="obj">{{.Quest.Desc}}</span>.
  <div class="muted">One word or departure and the whole party is flung backward.</div>
  {{if eq .Quest.Kind "map"}}
  <svg class="map" viewBox="-20 -20 {{add .Quest.MapSize 40}} {{add .Quest.MapSize 40}}" role="img" aria-label="quest map">
    <rect x="0" y="0" width="{{.Quest.MapSize}}" height="{{.Quest.MapSize}}" class="map-bg"/>
    <line x1="{{.Quest.X}}" y1="{{.Quest.Y}}" x2="{{.Quest.X1}}" y2="{{.Quest.Y1}}" class="leg leg1"/>
    <line x1="{{.Quest.X1}}" y1="{{.Quest.Y1}}" x2="{{.Quest.X2}}" y2="{{.Quest.Y2}}" class="leg leg2"/>
    <circle cx="{{.Quest.X1}}" cy="{{.Quest.Y1}}" r="9" class="wp"/>
    <circle cx="{{.Quest.X2}}" cy="{{.Quest.Y2}}" r="9" class="wp wp-end"/>
    <circle cx="{{.Quest.X}}" cy="{{.Quest.Y}}" r="13" class="party"/>
  </svg>
  {{end}}
</div>
{{else}}
<p class="muted">No quest underway. The gods are watching.</p>
{{end}}

<h2>top idlers</h2>
<table>
  <tr><th class="rank">#</th><th>name</th><th>lvl</th><th>next</th><th>power</th><th>creed</th></tr>
  {{range $i, $c := .Board}}
  <tr>
    <td class="rank">{{add $i 1}}</td>
    <td><a href="/p/{{pathesc $c.Key}}">{{$c.Name}}</a></td>
    <td class="lvl">{{$c.Level}}</td>
    <td class="muted">{{dur $c.TTL}}</td>
    <td>{{$c.Power}}</td>
    <td class="{{$c.Align}}">{{$c.Align}}{{if $c.Class}} {{$c.Class}}{{end}}</td>
  </tr>
  {{else}}
  <tr><td colspan="6" class="muted">No idlers yet.</td></tr>
  {{end}}
</table>

<footer>annoybots · auto-refreshes every 30s · read-only view of the shared realm</footer>
</body>
</html>`

const charTmpl = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>annoybots · {{.Name}}</title>
<style>
  :root { color-scheme: dark; }
  body { background:#0e0f13; color:#d6d8de; font:15px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace; margin:0; padding:2rem; }
  h1 { font-size:1.4rem; margin:0 0 .25rem; color:#e9b949; }
  .sub { color:#8aa0c6; margin:0 0 1.5rem; }
  table { border-collapse:collapse; max-width:480px; width:100%; }
  th,td { text-align:left; padding:.35rem .75rem; border-bottom:1px solid #20232b; }
  th { color:#7c8290; }
  .k { color:#7c8290; width:9rem; }
  .lvl { color:#7fd1a8; }
  .evil { color:#e06c75; } .good { color:#61afef; } .neutral { color:#abb2bf; }
  .muted { color:#6b7280; }
  a { color:#7fd1a8; text-decoration:none; }
  a:hover { text-decoration:underline; }
  footer { margin-top:2rem; color:#4b5563; font-size:.8rem; }
</style>
</head>
<body>
<h1>{{.Name}}</h1>
<p class="sub">the <span class="{{.Align}}">{{.Align}}{{if .Race}} {{.Race}}{{end}}{{if .Class}} {{.Class}}{{end}}</span></p>

<table>
  <tr><td class="k">level</td><td class="lvl">{{.Level}}</td></tr>
  <tr><td class="k">hp</td><td>{{.HP}} <span class="muted">/ {{.MaxHP}}</span></td></tr>
  <tr><td class="k">gold</td><td>{{.Gold}}</td></tr>
  <tr><td class="k">kills</td><td>{{.Kills}}</td></tr>
  <tr><td class="k">time to next</td><td class="muted">{{dur .TTL}}</td></tr>
  <tr><td class="k">power</td><td>{{.Power}}</td></tr>
</table>

{{if .Abilities}}
<h2 style="font-size:1rem;color:#8aa0c6;margin:1.5rem 0 .5rem;">abilities</h2>
<table>
  {{range .Abilities}}<tr><td class="k">{{.Name}}</td><td>{{.Score}} <span class="muted">({{if ge .Mod 0}}+{{end}}{{.Mod}})</span></td></tr>
  {{end}}
</table>
{{end}}

<h2 style="font-size:1rem;color:#8aa0c6;margin:1.5rem 0 .5rem;">equipment</h2>
<table>
  {{range .Items}}<tr><td class="k">{{.Slot}}</td><td>{{.Rarity}} lvl {{.Level}}{{if .Name}} <span class="muted">“{{.Name}}”</span>{{end}}</td></tr>
  {{else}}<tr><td colspan="2" class="muted">nothing equipped yet.</td></tr>{{end}}
</table>

<footer><a href="/">&larr; back to the realm</a></footer>
</body>
</html>`

const mapTmpl = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="30">
<title>annoybots · the realm map</title>
<style>
  :root { color-scheme: dark; }
  body { background:#0e0f13; color:#d6d8de; font:15px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace; margin:0; padding:2rem; }
  h1 { font-size:1.4rem; margin:0 0 .25rem; color:#e9b949; }
  .sub { color:#8aa0c6; margin:0 0 1rem; }
  .world { display:block; width:100%; max-width:680px; margin:0 auto; background:#0b0c10; border:1px solid #2a2f3a; border-radius:8px; }
  .town { fill:#4a3c14; stroke:#e9b949; stroke-width:2; }
  .town-l { fill:#b9912f; font-size:11px; }
  .dot { fill:#7fd1a8; stroke:#0e0f13; stroke-width:1.5; }
  .dot-l { fill:#9aa0ac; font-size:9px; }
  .muted { color:#6b7280; }
  a { color:#7fd1a8; text-decoration:none; }
  a:hover { text-decoration:underline; }
  footer { margin-top:1.5rem; color:#4b5563; font-size:.8rem; text-align:center; }
</style>
</head>
<body>
<h1>🗺 the realm map</h1>
<p class="sub">{{len .Players}} adventurers roaming · towns in gold</p>
<svg class="world" viewBox="-12 -12 {{add .Size 24}} {{add .Size 24}}" role="img" aria-label="world map of player positions">
  <rect x="0" y="0" width="{{.Size}}" height="{{.Size}}" fill="#0b0c10" stroke="#20232b" stroke-width="1"/>
  {{range .Towns}}
  <rect x="{{add .X -5}}" y="{{add .Y -5}}" width="10" height="10" class="town"/>
  <text x="{{add .X 9}}" y="{{add .Y 4}}" class="town-l">{{.Name}}</text>
  {{end}}
  {{range .Players}}
  <circle cx="{{.X}}" cy="{{.Y}}" r="4" class="dot"/>
  <text x="{{add .X 6}}" y="{{add .Y 3}}" class="dot-l">{{.Name}}</text>
  {{else}}
  <text x="{{add .Size -250}}" y="250" class="dot-l">no one has wandered onto the map yet.</text>
  {{end}}
</svg>
<footer><a href="/">&larr; back to the realm</a> · auto-refreshes every 30s</footer>
</body>
</html>`
