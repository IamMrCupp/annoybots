// Package rpgweb is a tiny read-only web view of the IdleRPG world. It reads the
// shared Redis state the bots already fill (rankings, the active quest) and
// renders a single status page — idlerpg.net's XPM map, reimagined as HTML over
// the F3 store. It never writes, so it can run anywhere with read access to Redis.
package rpgweb

import (
	"context"
	"fmt"
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
	helpTmpl *template.Template
	hallTmpl *template.Template
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
		helpTmpl: template.Must(template.New("help").Funcs(tmplFuncs).Parse(helpTmpl)),
		hallTmpl: template.Must(template.New("hall").Funcs(tmplFuncs).Parse(hallTmpl)),
		now:      time.Now,
	}
}

// Handler returns the HTTP routes: the dashboard, per-character pages, and a probe.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/map", s.worldMap)
	mux.HandleFunc("/hall", s.hall)
	mux.HandleFunc("/help", s.help)
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
	Feed      []feedRow
	Stats     idlerpg.RealmStats
	Boss      *idlerpg.WorldBossView
	WEvent    *idlerpg.WorldEventView
}

// feedRow is one rendered activity-feed line: the text plus a relative timestamp.
type feedRow struct {
	Text string
	Ago  string
}

const feedSize = 30 // activity-feed entries shown on the dashboard

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
	events, _ := idlerpg.ReadFeed(ctx, s.store, feedSize)
	stats, _ := idlerpg.ReadStats(ctx, s.store)
	boss, _ := idlerpg.ReadWorldBoss(ctx, s.store)
	wev, _ := idlerpg.ReadWorldEvent(ctx, s.store, s.now().Unix())

	data := pageData{Board: board, Quest: quest, Stats: stats, Boss: boss, WEvent: wev}
	if quest != nil && quest.Kind != "map" {
		data.QuestLeft = humanLeft(quest.Deadline - s.now().Unix())
	}
	now := s.now().Unix()
	for _, e := range events {
		data.Feed = append(data.Feed, feedRow{Text: e.Text, Ago: humanAgo(now - e.Ts)})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.tmpl.Execute(w, data)
}

// humanAgo renders an elapsed-seconds count as a compact "just now / 5m / 2h / 3d".
func humanAgo(secs int64) string {
	switch {
	case secs < 10:
		return "just now"
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%dh", secs/3600)
	default:
		return fmt.Sprintf("%dd", secs/86400)
	}
}

// mapData is the map page view model: the world plus the current per-biome sky.
type mapData struct {
	idlerpg.WorldView
	Weather []idlerpg.WeatherView
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
	sky, _ := idlerpg.ReadWeather(ctx, s.store)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.mapTmpl.Execute(w, mapData{WorldView: world, Weather: sky})
}

// helpData is the /help page view model: the public command groups plus the
// admin group, sourced from the idlerpg package so it matches the in-channel
// !rpg help exactly.
type helpData struct {
	Groups     []idlerpg.HelpGroup
	Admin      idlerpg.HelpGroup
	Classes    []idlerpg.ClassInfo
	Races      []idlerpg.RaceInfo
	Pets       []idlerpg.PetInfo
	Alignments []string
}

// hallData is everything the Hall of Fame renders: the ranking columns, then the
// guild table beneath them.
type hallData struct {
	Boards []hallBoard
	Guilds []idlerpg.GuildView
}

// hallBoard is one named ranking column on the Hall of Fame.
type hallBoard struct {
	Title string
	Unit  string
	Rows  []idlerpg.RankRow
}

const hallSize = 10 // entries per Hall-of-Fame column

// hall renders the Hall of Fame at /hall — every leaderboard, side by side.
func (s *Server) hall(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	boards := []hallBoard{
		{"⚔ Level", "lvl", nil},
		{"💀 Kills", "kills", nil},
		{"💰 Gold", "g", nil},
		{"🗡 Duel wins", "wins", nil},
		{"🌟 Rebirths", "★", nil},
	}
	fields := []string{"level", "kills", "gold", "duelw", "reb"}
	for i := range boards {
		boards[i].Rows, _ = idlerpg.ReadRanking(ctx, s.store, fields[i], hallSize)
	}
	guilds, _ := idlerpg.ReadGuilds(ctx, s.store)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.hallTmpl.Execute(w, hallData{Boards: boards, Guilds: guilds})
}

// help renders the command reference + the character-options catalog at /help.
func (s *Server) help(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.helpTmpl.Execute(w, helpData{
		Groups:     idlerpg.CommandHelp(),
		Admin:      idlerpg.AdminHelp(),
		Classes:    idlerpg.Classes(),
		Races:      idlerpg.Races(),
		Pets:       idlerpg.Pets(),
		Alignments: idlerpg.Alignments(),
	})
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
	data := charData{CharView: cv}
	events, _ := idlerpg.ReadCharFeed(ctx, s.store, cv.Name, charFeedSize)
	now := s.now().Unix()
	for _, e := range events {
		data.Recent = append(data.Recent, feedRow{Text: e.Text, Ago: humanAgo(now - e.Ts)})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.charTmpl.Execute(w, data)
}

// charData is the character page view model: the character plus its recent
// activity (its slice of the realm feed).
type charData struct {
	idlerpg.CharView
	Recent []feedRow
}

const charFeedSize = 12 // recent events shown on a character page

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
  .ttl { color:#c9a227; font-style:italic; font-size:.85em; }
  .star { color:#e9b949; font-size:.85em; }
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
  .nav { margin:0 0 1.25rem; font-size:.95rem; color:#3a3f4b; }
  .nav a { margin-right:1rem; }
  .stats { display:flex; flex-wrap:wrap; gap:.6rem; margin:0 0 1.5rem; }
  .stat { flex:1 1 7rem; min-width:7rem; background:#15171d; border:1px solid #20232b; border-radius:8px; padding:.6rem .8rem; }
  .stat .num { display:block; font-size:1.5rem; color:#e9b949; line-height:1.1; }
  .stat .lab { font-size:.75rem; color:#7c8290; text-transform:uppercase; letter-spacing:.05em; }
  .wevent { max-width:760px; background:#12161d; border:1px solid #2c3b52; border-radius:8px; padding:.7rem 1rem; margin:0 0 1rem; }
  .wevent strong { color:#8aa0c6; }
  .boss { max-width:760px; background:#1d1212; border:1px solid #5a2b2b; border-radius:8px; padding:.8rem 1rem; margin:0 0 1.5rem; }
  .boss strong { color:#e06c75; }
  .bar { height:14px; background:#2a1414; border-radius:7px; overflow:hidden; margin-top:.5rem; }
  .bar-fill { height:100%; background:linear-gradient(90deg,#e06c75,#e9b949); transition:width .5s; }
  .feed { list-style:none; padding:0; margin:.5rem 0; max-width:760px; }
  .feed li { padding:.3rem 0; border-bottom:1px solid #15171d; }
  .feed .ago { display:inline-block; min-width:4rem; color:#5b6270; font-size:.82em; }
  a { color:#7fd1a8; text-decoration:none; }
  a:hover { text-decoration:underline; }
  footer { margin-top:2rem; color:#4b5563; font-size:.8rem; }
  @media (max-width:640px) {
    body { padding:1rem .85rem; }
    h1 { font-size:1.2rem; }
    .nav { display:flex; flex-wrap:wrap; gap:.4rem 1rem; }
    .nav a { margin-right:0; }
    /* wide tables scroll inside themselves instead of stretching the page */
    table { display:block; overflow-x:auto; white-space:nowrap; -webkit-overflow-scrolling:touch; }
  }
</style>
</head>
<body>
<nav class="nav"><a href="/">⚔ realm</a><a href="/map">🗺 map</a><a href="/hall">🏆 hall of fame</a><a href="/help">📖 how to play</a></nav>
<h1>⚔ the idle realm</h1>
<div class="stats">
  <div class="stat"><span class="num">{{.Stats.Heroes}}</span><span class="lab">heroes</span></div>
  <div class="stat"><span class="num">{{.Stats.Levels}}</span><span class="lab">levels gained</span></div>
  <div class="stat"><span class="num">{{.Stats.Kills}}</span><span class="lab">monsters slain</span></div>
  <div class="stat"><span class="num">{{.Stats.Bosses}}</span><span class="lab">bosses felled</span></div>
  <div class="stat"><span class="num">{{.Stats.Gold}}</span><span class="lab">gold minted</span></div>
  <div class="stat"><span class="num">{{.Stats.Legendaries}}</span><span class="lab">legendaries</span></div>
  <div class="stat"><span class="num">{{.Stats.Delves}}</span><span class="lab">dungeons cleared</span></div>
</div>
{{if .WEvent}}
<div class="wevent"><strong>🌕 {{.WEvent.Name}}</strong> — {{.WEvent.Desc}} <span class="muted">({{dur .WEvent.Left}} left)</span></div>
{{end}}
{{if .Boss}}
<div class="boss">
  <strong>🐲 WORLD BOSS — {{.Boss.Name}}</strong>
  <span class="muted">{{.Boss.HP}} / {{.Boss.MaxHP}} HP · {{.Boss.Players}} heroes fighting{{if .Boss.TopName}} · ⭐ {{.Boss.TopName}} leads ({{.Boss.TopDmg}} dmg){{end}}</span>
  <div class="bar"><div class="bar-fill" style="width:{{.Boss.Pct}}%"></div></div>
</div>
{{end}}
{{if .Quest}}
<div class="quest">
  <strong>{{if eq .Quest.Kind "hunt"}}A hunt is underway.{{else}}A quest is underway.{{end}}</strong>
  {{if eq .Quest.Kind "map"}}<span class="muted">(leg {{add .Quest.Stage 1}} of 2)</span>
  {{else if eq .Quest.Kind "hunt"}}<span class="muted">({{.Quest.Progress}} / {{.Quest.Target}} foes slain · {{.QuestLeft}} left)</span>
  {{else}}<span class="muted">({{.QuestLeft}} left)</span>{{end}}<br>
  {{range $i, $m := .Quest.Members}}{{if $i}}, {{end}}{{$m}}{{end}}
  {{if eq .Quest.Kind "hunt"}}must <span class="obj">slay {{.Quest.Target}} foes together</span>.
  {{else}}must <span class="obj">{{.Quest.Desc}}</span>.{{end}}
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
    <td>{{if $c.Rebirths}}<span class="star">★{{$c.Rebirths}}</span> {{end}}<a href="/p/{{pathesc $c.Key}}">{{$c.Name}}</a>{{if $c.Title}} <span class="ttl">{{$c.Title}}</span>{{end}}</td>
    <td class="lvl">{{$c.Level}}</td>
    <td class="muted">{{dur $c.TTL}}</td>
    <td>{{$c.Power}}</td>
    <td class="{{$c.AlignClass}}">{{$c.Align}}{{if $c.Class}} {{$c.Class}}{{end}}</td>
  </tr>
  {{else}}
  <tr><td colspan="6" class="muted">No idlers yet.</td></tr>
  {{end}}
</table>

{{if .Feed}}
<h2>realm activity</h2>
<ul class="feed">
  {{range .Feed}}<li><span class="ago">{{.Ago}}</span> {{.Text}}</li>
  {{end}}
</ul>
{{end}}

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
  h1 .ttl { color:#c9a227; font-style:italic; font-weight:400; }
  .nav { margin:0 0 1.25rem; font-size:.95rem; color:#3a3f4b; }
  .nav a { margin-right:1rem; }
  .feed { list-style:none; padding:0; margin:.5rem 0; max-width:760px; }
  .feed li { padding:.3rem 0; border-bottom:1px solid #15171d; }
  .feed .ago { display:inline-block; min-width:4rem; color:#5b6270; font-size:.82em; }
  .feat { display:inline-block; background:#1a160c; border:1px solid #4a3c14; border-radius:10px; padding:.1rem .55rem; margin:.15rem .15rem 0 0; font-size:.85em; color:#e9b949; }
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
  @media (max-width:640px) {
    body { padding:1rem .85rem; }
    h1 { font-size:1.2rem; }
    .nav { display:flex; flex-wrap:wrap; gap:.4rem 1rem; }
    .nav a { margin-right:0; }
    /* wide tables scroll inside themselves instead of stretching the page */
    table { display:block; overflow-x:auto; white-space:nowrap; -webkit-overflow-scrolling:touch; }
  }
</style>
</head>
<body>
<nav class="nav"><a href="/">⚔ realm</a><a href="/map">🗺 map</a><a href="/hall">🏆 hall of fame</a><a href="/help">📖 how to play</a></nav>
<h1>{{if .Rebirths}}<span class="ttl">★{{.Rebirths}}</span> {{end}}{{.Name}}{{if .Title}} <span class="ttl">{{.Title}}</span>{{end}}</h1>
<p class="sub">the <span class="{{.AlignClass}}">{{.Align}}{{if .Race}} {{.Race}}{{end}}{{if .Class}} {{.Class}}{{end}}</span></p>

<table>
  <tr><td class="k">level</td><td class="lvl">{{.Level}}</td></tr>
  <tr><td class="k">hp</td><td>{{.HP}} <span class="muted">/ {{.MaxHP}}</span>{{if .Poisoned}} <span title="poisoned">☠️</span>{{end}}{{if .Blessed}} <span title="blessed">🕊️</span>{{end}}</td></tr>
  <tr><td class="k">gold</td><td>{{.Gold}}</td></tr>
  <tr><td class="k">kills</td><td>{{.Kills}}</td></tr>
  {{if .DuelWins}}<tr><td class="k">duel wins</td><td>{{.DuelWins}}</td></tr>{{end}}
  <tr><td class="k">location</td><td class="muted">{{.Location}}</td></tr>
  {{if .Pet}}<tr><td class="k">companion</td><td>🐾 {{.Pet}}</td></tr>{{end}}
  {{if .Mount}}<tr><td class="k">mount</td><td>🐎 {{.Mount}}</td></tr>{{end}}
  {{if .Guild}}<tr><td class="k">guild</td><td>🛡 {{.Guild}}</td></tr>{{end}}
  {{if .Dungeon}}<tr><td class="k">delving</td><td>🏚 {{.Dungeon}} — {{.Rooms}} room(s) to go</td></tr>{{end}}
  {{if .Draughts}}<tr><td class="k">draughts</td><td>🧪 {{.Draughts}}</td></tr>{{end}}
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

{{if .Feats}}
<h2 style="font-size:1rem;color:#8aa0c6;margin:1.5rem 0 .5rem;">feats</h2>
<p>{{range .Feats}}<span class="feat">🎖️ {{.}}</span> {{end}}</p>
{{end}}

{{if .Recent}}
<h2 style="font-size:1rem;color:#8aa0c6;margin:1.5rem 0 .5rem;">recent</h2>
<ul class="feed">
  {{range .Recent}}<li><span class="ago">{{.Ago}}</span> {{.Text}}</li>
  {{end}}
</ul>
{{end}}

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
  body { background:#15110b; color:#d6c9a8; font:15px/1.5 Georgia,'Times New Roman',serif; margin:0; padding:2rem; }
  h1 { font-size:1.5rem; margin:0 0 .25rem; color:#e9b949; font-variant:small-caps; letter-spacing:.06em; }
  .sub { color:#9a8a64; margin:0 0 1rem; font-style:italic; }
  .nav { margin:0 0 1.25rem; font-size:.95rem; color:#3a3f4b; }
  .nav a { margin-right:1rem; }
  .world { display:block; width:100%; max-width:680px; margin:0 auto; background:#e7d9b5; border:2px solid #6b563b; border-radius:4px; box-shadow:0 4px 20px rgba(0,0,0,.5); }
  .coast { fill:#b9cfcb; stroke:#6b563b; stroke-width:1.3; }
  .wave { fill:none; stroke:#98b3ae; stroke-width:1; }
  .river { fill:none; stroke:#b9cfcb; stroke-width:4; stroke-linecap:round; }
  .river-l { fill:none; stroke:#8fb0ab; stroke-width:1; }
  .lake { fill:#b9cfcb; stroke:#6b563b; stroke-width:1.2; }
  .mtn { fill:#cdbb94; stroke:#6b563b; stroke-width:1.2; stroke-linejoin:round; }
  .snow { fill:#f4eede; }
  .tree { fill:#6f7d49; }
  .reed { stroke:#7f8a55; stroke-width:1.2; stroke-linecap:round; fill:none; }
  .sea-l { fill:#6e8a85; font-style:italic; font-size:13px; }
  .region-l { fill:#9a8255; font-style:italic; font-size:11px; }
  .frame { fill:none; stroke:#6b563b; }
  .sky { color:#6b5a3a; font-style:italic; margin:0 0 1rem; }
  .town-l { fill:#5a3a22; font-size:12px; font-variant:small-caps; }
  .biome-l { fill:#9a8255; font-size:8.5px; font-style:italic; }
  .dot { fill:#2f4a78; stroke:#e7d9b5; stroke-width:1.2; }
  .dot-l { fill:#463c26; font-size:9px; font-style:italic; }
  a { color:#e9b949; text-decoration:none; }
  a:hover { text-decoration:underline; }
  footer { margin-top:1.5rem; color:#6b5a3a; font-size:.8rem; text-align:center; }
  @media (max-width:640px) {
    body { padding:1rem .85rem; }
    h1 { font-size:1.3rem; }
    .nav { display:flex; flex-wrap:wrap; gap:.4rem 1rem; }
    .nav a { margin-right:0; }
  }
</style>
</head>
<body>
<nav class="nav"><a href="/">⚔ realm</a><a href="/map">🗺 map</a><a href="/hall">🏆 hall of fame</a><a href="/help">📖 how to play</a></nav>
<h1>🗺 the realm map</h1>
{{if .Weather}}<p class="sky">the sky — {{range $i, $w := .Weather}}{{if $i}} · {{end}}{{$w.Biome}}: {{$w.Kind}}{{end}}</p>{{end}}
<p class="sub">{{len .Players}} souls abroad in the realm</p>
<svg class="world" viewBox="0 0 {{.Size}} {{.Size}}" role="img" aria-label="fantasy map of the realm with towns and wandering players">
  <rect x="0" y="0" width="{{.Size}}" height="{{.Size}}" fill="#e7d9b5"/>

  <!-- the sea (southwest) -->
  <path class="coast" d="M0,392 Q60,430 100,452 Q140,470 130,490 L0,500 Z"/>
  <path class="coast" d="M210,500 Q250,486 300,492 L320,500 Z"/>
  <path class="wave" d="M24,440 q12,-7 24,0 t24,0"/>
  <path class="wave" d="M36,460 q12,-7 24,0 t24,0"/>
  <text class="sea-l" x="22" y="486">The Idle Sea</text>

  <!-- river: from the northeast peaks down to the sea -->
  <path class="river" d="M428,118 C360,168 300,228 250,278 C202,326 158,396 118,452"/>
  <path class="river-l" d="M428,118 C360,168 300,228 250,278 C202,326 158,396 118,452"/>
  <!-- the ford stream past Quietford, with a little bridge -->
  <path class="river" style="stroke-width:3" d="M34,108 Q92,138 168,132"/>
  <path class="reed" d="M122,142 l16,-8 M122,150 l16,-8"/>

  <!-- a lake -->
  <ellipse class="lake" cx="332" cy="196" rx="26" ry="15"/>

  <!-- the northeastern peaks -->
  <g class="mtn">
    <path d="M356,128 l20,-40 20,40 z"/>
    <path d="M388,134 l26,-54 26,54 z"/>
    <path d="M428,124 l18,-34 18,34 z"/>
    <path d="M334,132 l14,-26 14,26 z"/>
  </g>
  <path class="snow" d="M408,98 l6,-14 6,14 z"/>
  <path class="snow" d="M376,104 l4,-10 4,10 z"/>

  <!-- forests -->
  <g class="tree">
    <path d="M286,316 l-5,9 10,0 z"/><path d="M298,322 l-5,9 10,0 z"/><path d="M278,326 l-5,9 10,0 z"/>
    <path d="M306,312 l-5,9 10,0 z"/><path d="M292,332 l-5,9 10,0 z"/><path d="M314,326 l-5,9 10,0 z"/>
    <path d="M168,250 l-5,9 10,0 z"/><path d="M180,256 l-5,9 10,0 z"/><path d="M158,260 l-5,9 10,0 z"/>
    <path d="M190,248 l-5,9 10,0 z"/><path d="M174,266 l-5,9 10,0 z"/>
    <path d="M436,288 l-5,9 10,0 z"/><path d="M448,294 l-5,9 10,0 z"/><path d="M428,298 l-5,9 10,0 z"/>
    <path d="M250,150 l-4,8 8,0 z"/><path d="M260,156 l-4,8 8,0 z"/><path d="M242,158 l-4,8 8,0 z"/>
  </g>

  <!-- the marsh (southeast) -->
  <g class="reed">
    <path d="M330,360 l0,-10 M338,364 l0,-12 M324,368 l0,-9"/>
    <path d="M352,372 l0,-11 M362,366 l0,-10 M372,374 l0,-12 M384,368 l0,-9"/>
    <path d="M344,388 l0,-10 M356,392 l0,-11 M368,386 l0,-9"/>
  </g>
  <text class="region-l" x="316" y="412">The Lag Fens</text>

  <!-- compass rose (southeast corner) -->
  <g transform="translate(452,452)">
    <circle r="20" fill="none" stroke="#6b563b" stroke-width="1"/>
    <path d="M0,-22 L4,0 L0,22 L-4,0 Z" fill="#6b563b"/>
    <path d="M-22,0 L0,-4 L22,0 L0,4 Z" fill="#8a7a5e"/>
    <text x="-3.5" y="-25" fill="#6b563b" font-size="10" font-weight="bold">N</text>
  </g>

  <!-- decorative double frame -->
  <rect class="frame" x="6" y="6" width="{{add .Size -12}}" height="{{add .Size -12}}" stroke-width="3"/>
  <rect class="frame" x="12" y="12" width="{{add .Size -24}}" height="{{add .Size -24}}" stroke-width="1"/>

  <!-- towns -->
  {{range .Towns}}
  <circle cx="{{.X}}" cy="{{.Y}}" r="4.5" fill="#8c2f1c" stroke="#3a160d" stroke-width="1"/>
  <circle cx="{{.X}}" cy="{{.Y}}" r="1.6" fill="#f0e6c8"/>
  <text class="town-l" x="{{add .X 8}}" y="{{add .Y 4}}">{{.Name}}</text>
  <text class="biome-l" x="{{add .X 8}}" y="{{add .Y 15}}">{{.Terrain}} · {{.Service}}</text>
  {{end}}

  <!-- wandering souls -->
  {{range .Players}}
  <circle class="dot" cx="{{.X}}" cy="{{.Y}}" r="3"/>
  <text class="dot-l" x="{{add .X 5}}" y="{{add .Y 3}}">{{.Name}}</text>
  {{else}}
  <text class="region-l" x="186" y="248">no souls wander here yet…</text>
  {{end}}
</svg>
<footer><a href="/">&larr; back to the realm</a> · auto-refreshes every 30s</footer>
</body>
</html>`

const helpTmpl = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>annoybots · how to play</title>
<style>
  :root { color-scheme: dark; }
  body { background:#0e0f13; color:#d6d8de; font:15px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace; margin:0; padding:2rem; }
  h1 { font-size:1.4rem; color:#e9b949; margin:0 0 .25rem; }
  .sub { color:#8aa0c6; margin:0 0 1.5rem; max-width:720px; }
  h2 { font-size:1rem; color:#8aa0c6; margin:1.5rem 0 .5rem; }
  .admin h2 { color:#e9b949; }
  .nav { margin:0 0 1.25rem; font-size:.95rem; color:#3a3f4b; }
  .nav a { margin-right:1rem; }
  table { border-collapse:collapse; max-width:760px; width:100%; }
  td { text-align:left; padding:.35rem .75rem; border-bottom:1px solid #20232b; vertical-align:top; }
  .cmd { color:#7fd1a8; white-space:nowrap; }
  .opt { color:#e9b949; white-space:nowrap; }
  .bon { color:#7fd1a8; white-space:nowrap; }
  .muted { color:#6b7280; }
  a { color:#7fd1a8; text-decoration:none; }
  a:hover { text-decoration:underline; }
  footer { margin-top:2rem; color:#4b5563; font-size:.8rem; }
  @media (max-width:640px) {
    body { padding:1rem .85rem; }
    h1 { font-size:1.2rem; }
    .nav { display:flex; flex-wrap:wrap; gap:.4rem 1rem; }
    .nav a { margin-right:0; }
    /* wide tables scroll inside themselves instead of stretching the page */
    table { display:block; overflow-x:auto; white-space:nowrap; -webkit-overflow-scrolling:touch; }
  }
</style>
</head>
<body>
<nav class="nav"><a href="/">⚔ realm</a><a href="/map">🗺 map</a><a href="/hall">🏆 hall of fame</a><a href="/help">📖 how to play</a></nav>
<h1>📖 how to play</h1>
<p class="sub">You "play" IdleRPG by being present and <strong>quiet</strong> in the channel — every tick you idle, you advance toward the next level. Talking sets you back; leaving sets you back more. Everything below is typed <strong>in the channel</strong> as <code>!rpg …</code>.</p>
{{range .Groups}}
<h2>{{.Title}}</h2>
<table>
  {{range .Items}}<tr><td class="cmd">{{.Cmd}}</td><td>{{.Desc}}</td></tr>
  {{end}}
</table>
{{end}}

<h2>Character options</h2>
<p class="muted">Set once with <code>!rpg class</code>, <code>!rpg race</code>, and <code>!rpg align</code>. Companions can't be chosen — they're earned by slaying a boss.</p>
<h3 style="color:#8aa0c6;font-size:.95rem;margin:1rem 0 .35rem;">Classes</h3>
<table>
  {{range .Classes}}<tr><td class="opt">{{.Name}}</td><td><span class="bon">{{.Ability}}</span> · {{.Power}} <span class="muted">— {{.Blurb}}</span></td></tr>
  {{end}}
</table>
<h3 style="color:#8aa0c6;font-size:.95rem;margin:1rem 0 .35rem;">Races</h3>
<table>
  {{range .Races}}<tr><td class="opt">{{.Name}}</td><td><span class="bon">{{.Bonus}}</span> <span class="muted">— {{.Blurb}}</span></td></tr>
  {{end}}
</table>
<h3 style="color:#8aa0c6;font-size:.95rem;margin:1rem 0 .35rem;">Companions <span class="muted">(earned, not chosen)</span></h3>
<table>
  {{range .Pets}}<tr><td class="opt">{{.Name}}</td><td><span class="bon">+{{.Atk}} atk, +{{.Dmg}} dmg</span> <span class="muted">— {{.Blurb}}</span></td></tr>
  {{end}}
</table>
<h3 style="color:#8aa0c6;font-size:.95rem;margin:1rem 0 .35rem;">Alignments</h3>
<p>{{range .Alignments}}<span class="opt">{{.}}</span> · {{end}}</p>

<div class="admin">
<h2>{{.Admin.Title}}</h2>
<p class="muted">In-channel too, but only honored from a bot admin (same identity authorization as the admin console).</p>
<table>
  {{range .Admin.Items}}<tr><td class="cmd">{{.Cmd}}</td><td>{{.Desc}}</td></tr>
  {{end}}
</table>
</div>
<footer><a href="/">&larr; back to the realm</a> · <a href="/map">the map</a></footer>
</body>
</html>`

const hallTmpl = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="60">
<title>annoybots · hall of fame</title>
<style>
  :root { color-scheme: dark; }
  body { background:#0e0f13; color:#d6d8de; font:15px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace; margin:0; padding:2rem; }
  h1 { font-size:1.4rem; color:#e9b949; margin:0 0 .25rem; }
  .sub { color:#8aa0c6; margin:0 0 1.5rem; }
  .nav { margin:0 0 1.25rem; font-size:.95rem; color:#3a3f4b; }
  .nav a { margin-right:1rem; color:#7fd1a8; text-decoration:none; }
  .nav a:hover { text-decoration:underline; }
  .halls { display:flex; flex-wrap:wrap; gap:1.5rem; }
  .board { flex:1 1 13rem; min-width:12rem; }
  .board h2 { font-size:1rem; color:#8aa0c6; margin:0 0 .5rem; }
  table { border-collapse:collapse; width:100%; }
  td { padding:.25rem .4rem; border-bottom:1px solid #20232b; }
  .rank { color:#7c8290; width:1.8rem; }
  .val { color:#e9b949; text-align:right; white-space:nowrap; }
  .gold td:first-child + td a { color:#e9b949; }
  a { color:#7fd1a8; text-decoration:none; }
  a:hover { text-decoration:underline; }
  .muted { color:#6b7280; }
  .guilds-h { font-size:1.1rem; color:#e9b949; margin:2rem 0 .25rem; }
  table.guilds { margin-top:.5rem; max-width:44rem; }
  footer { margin-top:2rem; color:#4b5563; font-size:.8rem; }
  @media (max-width:640px) {
    body { padding:1rem .85rem; }
    h1 { font-size:1.2rem; }
    .nav { display:flex; flex-wrap:wrap; gap:.4rem 1rem; }
    .nav a { margin-right:0; }
    /* wide tables scroll inside themselves instead of stretching the page */
    table { display:block; overflow-x:auto; white-space:nowrap; -webkit-overflow-scrolling:touch; }
  }
</style>
</head>
<body>
<nav class="nav"><a href="/">⚔ realm</a><a href="/map">🗺 map</a><a href="/hall">🏆 hall of fame</a><a href="/help">📖 how to play</a></nav>
<h1>🏆 Hall of Fame</h1>
<p class="sub">the realm's greatest — ranked every way that matters.</p>
<div class="halls">
{{range .Boards}}
  {{$unit := .Unit}}
  <div class="board">
    <h2>{{.Title}}</h2>
    <table>
      {{range $i, $r := .Rows}}
      <tr><td class="rank">{{add $i 1}}</td><td><a href="/p/{{pathesc $r.Key}}">{{$r.Name}}</a></td><td class="val">{{$r.Value}}{{if $unit}}<span class="muted"> {{$unit}}</span>{{end}}</td></tr>
      {{else}}
      <tr><td colspan="3" class="muted">no one yet.</td></tr>
      {{end}}
    </table>
  </div>
{{end}}
</div>
{{if .Guilds}}
<h2 class="guilds-h">🛡 Guilds</h2>
<p class="sub">bands of heroes, ranked by their members' summed levels.</p>
<table class="guilds">
  <tr><td class="rank"></td><td class="muted">guild</td><td class="muted">founder</td><td class="val muted">members</td><td class="val muted">vault</td><td class="val muted">level</td></tr>
  {{range $i, $g := .Guilds}}
  <tr><td class="rank">{{add $i 1}}</td><td>{{$g.Name}}</td><td class="muted">{{$g.Founder}}</td><td class="val">{{$g.Members}}</td><td class="val">{{$g.Vault}}<span class="muted"> g</span></td><td class="val">{{$g.Level}}</td></tr>
  {{end}}
</table>
{{end}}
<footer>annoybots · auto-refreshes every 60s · <a href="/">&larr; back to the realm</a></footer>
</body>
</html>`
