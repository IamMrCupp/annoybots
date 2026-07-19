// Command annoybot runs a single annoying IRC/Twitch/Discord bot. The same
// binary takes on a different personality depending on the config file it is given.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/IamMrCupp/annoybots/internal/account"
	"github.com/IamMrCupp/annoybots/internal/admin"
	"github.com/IamMrCupp/annoybots/internal/bot"
	"github.com/IamMrCupp/annoybots/internal/botnet"
	"github.com/IamMrCupp/annoybots/internal/chanops"
	"github.com/IamMrCupp/annoybots/internal/config"
	"github.com/IamMrCupp/annoybots/internal/discord"
	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/event"
	"github.com/IamMrCupp/annoybots/internal/games"
	"github.com/IamMrCupp/annoybots/internal/health"
	"github.com/IamMrCupp/annoybots/internal/idlerpg"
	"github.com/IamMrCupp/annoybots/internal/irc"
	"github.com/IamMrCupp/annoybots/internal/markov"
	"github.com/IamMrCupp/annoybots/internal/plugin"
	"github.com/IamMrCupp/annoybots/internal/state"
	"github.com/IamMrCupp/annoybots/internal/tell"
)

func main() {
	configPath := flag.String("config", envOr("ANNOYBOT_CONFIG", "config.yaml"), "path to bot config YAML")
	quoteDir := flag.String("quotes", os.Getenv("ANNOYBOT_QUOTES_DIR"), "base dir for quote-pack files (defaults to config dir)")
	skitsFile := flag.String("skits", os.Getenv("ANNOYBOT_SKITS_FILE"), "override path to the shared skits file")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(*configPath, *quoteDir, *skitsFile)
	if err != nil {
		log.Error("load config", "err", err)
		os.Exit(1)
	}
	log = log.With("bot", cfg.Bot)

	// Load or create the Markov brain.
	var brain *markov.Chain
	if cfg.Brain.Path != "" {
		if b, lerr := markov.Load(cfg.Brain.Path); lerr == nil {
			brain = b
			log.Info("brain loaded", "path", cfg.Brain.Path, "states", b.Size())
		} else {
			log.Info("starting with a fresh brain", "path", cfg.Brain.Path)
		}
	}

	eng, err := engine.New(cfg.Personality, engine.Options{Brain: brain})
	if err != nil {
		log.Error("build engine", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The router fans engine replies back out to whichever transport a message
	// arrived on. Every transport feeds inbound messages through the handler.
	router := bot.NewRouter()

	// F3 shared state store: Redis when the botnet is on (so karma/awards are
	// network-wide + shared across bots/hosts), in-memory otherwise.
	var store state.Store
	if cfg.Botnet.Enabled {
		store = state.NewRedis(cfg.Botnet.RedisAddr, os.Getenv(cfg.Botnet.RedisPasswordEnv), "annoybots:state:")
	} else {
		store = state.NewMem()
	}

	// Optional command subsystems. Each defaults on; a dedicated bot (e.g. an
	// IdleRPG-only one) can switch any off in config. A nil manager means "not
	// running" and is skipped in the handler.

	// "Leave a message for someone" — !message <nick> <text>, delivered on their
	// next activity/JOIN (the latter via the event dispatcher, wired below).
	var tellMgr *tell.Manager
	if cfg.Tell.On() {
		tellMgr = tell.New(router)
	}

	// Account/identity linking — !register/!link/!whoami (DM); Resolve() maps a
	// sender to their cross-network character key for IdleRPG (and future features).
	var acctMgr *account.Manager
	if cfg.Accounts.On() {
		acctMgr = account.New(store, router, log)
	}

	// Public channel toys: !8ball, !roll, karma (name++ / !karma / !top).
	var gamesMgr *games.Manager
	if cfg.Games.On() {
		gamesMgr = games.New(router, store, log)
	}

	// IdleRPG — off by default; persists to the shared state store, keyed by
	// account when accounts are on, else by network identity.
	var rpgMgr *idlerpg.Manager
	if cfg.IdleRPG.Enabled {
		var resolve idlerpg.Resolver
		if acctMgr != nil {
			resolve = acctMgr.Resolve
		}
		rpgMgr = idlerpg.New(store, router, resolve, cfg.IdleRPG.Interval.D(), cfg.IdleRPG.BaseTTL.D(),
			cfg.IdleRPG.QuestInterval.D(), cfg.IdleRPG.QuestDuration.D(), log)
	}

	// Eggdrop-style Lua scripting: load command binds from the plugins dir.
	var pluginMgr *plugin.Manager
	if cfg.Plugins.Dir != "" {
		pluginMgr = plugin.New(router, log)
		pluginMgr.Load(cfg.Plugins.Dir)
		defer pluginMgr.Close()
	}

	log.Info("features", "games", gamesMgr != nil, "tell", tellMgr != nil,
		"accounts", acctMgr != nil, "idlerpg", rpgMgr != nil, "plugins", pluginMgr != nil)

	// Optional inter-bot bus + skit coordinator (the "botnet").
	var coord *botnet.Coordinator
	var bus botnet.Bus // nil interface when botnet is disabled
	if cfg.Botnet.Enabled {
		rb := botnet.NewRedis(cfg.Botnet.RedisAddr, os.Getenv(cfg.Botnet.RedisPasswordEnv), cfg.Botnet.Channel)
		bus = rb
		coord = botnet.NewCoordinator(cfg.Bot, rb, router, cfg.Skits, log, botnet.Options{})
		if rerr := coord.Run(ctx); rerr != nil {
			log.Error("botnet coordinator failed to start", "err", rerr)
			coord = nil
		} else {
			log.Info("botnet bus connected", "addr", cfg.Botnet.RedisAddr, "skits", len(cfg.Skits))
		}
	}

	// Optional chat-based admin console (DM-only, identity-authenticated).
	var adminMgr *admin.Manager
	if cfg.Admin.Enabled {
		adminPass := ""
		if cfg.Admin.PasswordEnv != "" {
			adminPass = os.Getenv(cfg.Admin.PasswordEnv)
		}
		adminMgr = admin.New(cfg.Bot, cfg.Admin, adminPass, eng, router, bus, log)
		// !reload re-reads quote packs and skits from disk without a restart.
		adminMgr.SetReload(func() (string, error) {
			fresh, lerr := config.Load(*configPath, *quoteDir, *skitsFile)
			if lerr != nil {
				return "", lerr
			}
			eng.SetQuotePacks(fresh.Personality.Quotes.Packs)
			if coord != nil {
				coord.SetSkits(fresh.Skits)
			}
			return fmt.Sprintf("%d quote packs, %d skits", len(fresh.Personality.Quotes.Packs), len(fresh.Skits)), nil
		})
		if rerr := adminMgr.Run(ctx); rerr != nil {
			log.Error("admin console failed to start", "err", rerr)
		} else {
			log.Info("admin console enabled", "admins", len(cfg.Admin.Admins))
		}
	}

	// Let bot admins drive the privileged !rpg verbs (pause/resume/push/hog),
	// authorized by the same identity model as the admin console.
	if rpgMgr != nil && adminMgr != nil {
		rpgMgr.SetAuthz(adminMgr.IsAdmin)
	}

	// In-channel !op: a recognized admin asks an opped bot to op them. Needs the
	// admin flag system for authorization; available whenever it's configured.
	var opMgr *chanops.Manager
	if adminMgr != nil {
		opMgr = chanops.New(router, router, log)
		opMgr.SetAuthz(adminMgr.IsAdmin)
	}

	handler := func(m engine.Message) {
		isOther := !strings.EqualFold(m.Nick, m.Self) && !eng.IsSibling(m.Nick)
		// Admin commands (DM-only) are handled first and never reach the engine.
		if adminMgr != nil && isOther && adminMgr.Handle(ctx, m) {
			return
		}
		if acctMgr != nil && isOther && acctMgr.Handle(m) {
			return
		}
		if tellMgr != nil && isOther && tellMgr.Handle(m) {
			return
		}
		if opMgr != nil && isOther && opMgr.Handle(m) {
			return
		}
		if gamesMgr != nil && isOther && gamesMgr.Handle(m) {
			return
		}
		if rpgMgr != nil && isOther && rpgMgr.Handle(m) {
			return
		}
		if pluginMgr != nil && isOther && pluginMgr.Handle(m) {
			return
		}
		eng.Handle(m, router)
		// Let humans (not the bots themselves) kick off coordinated skits.
		if coord != nil && isOther {
			coord.OnMessage(ctx, m)
		}
	}

	var ircNets, discordNets []config.Network
	for _, n := range cfg.Networks {
		if n.Kind == "discord" {
			discordNets = append(discordNets, n)
		} else {
			ircNets = append(ircNets, n)
		}
	}

	// The event dispatcher carries non-message events (JOIN/PART/QUIT/…) — the
	// foundation for tells, channel-keeping, idlerpg, and plugins. Today it just
	// logs presence at debug; features subscribe as they're built.
	disp := event.New()
	logPresence := func(ev event.Event) {
		log.Debug("presence", "kind", ev.Kind.String(), "network", ev.Network,
			"channel", ev.Channel, "nick", ev.Nick, "account", ev.Account)
	}
	disp.On(event.Join, logPresence)
	disp.On(event.Part, logPresence)
	disp.On(event.Quit, logPresence)
	if tellMgr != nil {
		disp.On(event.Join, tellMgr.OnJoin) // deliver pending !message notes on join
	}
	if rpgMgr != nil {
		disp.On(event.Join, rpgMgr.OnJoin)
		disp.On(event.Present, rpgMgr.OnPresent) // NAMES-seed idlers already in-channel
		disp.On(event.Part, rpgMgr.OnPart)
		disp.On(event.Quit, rpgMgr.OnQuit)
		disp.On(event.Kick, rpgMgr.OnKick)
		disp.On(event.Nick, rpgMgr.OnNick)
	}

	if len(ircNets) > 0 {
		mgr, merr := irc.NewManager(ircNets, handler, log, os.Getenv)
		if merr != nil {
			log.Error("build irc transport", "err", merr)
			os.Exit(1)
		}
		mgr.SetEventSink(disp.Emit)
		// The chankeeper is our per-channel op-state tracker. Channel-keeping uses
		// it to auto-op protected nicks; the !op command uses it to know whether we
		// hold ops. Enable it for either — with an empty protect list it purely
		// tracks and never auto-ops, so !op works without turning on channel-keeping.
		if cfg.ChanKeep.Enabled || opMgr != nil {
			var protect []string
			if cfg.ChanKeep.Enabled {
				protect = append(append([]string(nil), cfg.Personality.Siblings...), cfg.ChanKeep.Protect...)
			}
			mgr.EnableChanKeep(protect)
			log.Info("channel op tracking enabled", "chankeep", cfg.ChanKeep.Enabled, "protect", protect)
		}
		router.Add(mgr)
	}
	if len(discordNets) > 0 {
		dc, derr := discord.New(discordNets, handler, eng, log, os.Getenv)
		if derr != nil {
			log.Error("build discord transport", "err", derr)
			os.Exit(1)
		}
		router.Add(dc)
	}

	hs := health.New(cfg.Health.Addr, router.AnyConnected)
	hs.Start()
	log.Info("health server listening", "addr", cfg.Health.Addr)

	router.Run(ctx)
	log.Info("bot running", "networks", len(cfg.Networks))

	// Periodically persist the brain so learning survives restarts.
	saveEvery := cfg.Brain.SaveEvery.D()
	if saveEvery <= 0 {
		saveEvery = 5 * time.Minute
	}
	if cfg.Brain.Path != "" {
		go brainSaver(ctx, eng, cfg.Brain.Path, saveEvery, log)
	}

	// Self-initiated ambient chatter: periodically butt into quiet-but-live channels.
	if eng.AmbientEnabled() {
		go ambientTicker(ctx, eng, router, eng.AmbientInterval())
		log.Info("ambient timer enabled", "interval", eng.AmbientInterval().String())
	}

	// IdleRPG level ticks.
	if rpgMgr != nil {
		go rpgTicker(ctx, rpgMgr)
		log.Info("idlerpg enabled", "interval", rpgMgr.Interval().String())
	}

	<-ctx.Done()
	log.Info("shutting down")

	router.Quit()
	if bus != nil {
		_ = bus.Close()
	}
	_ = store.Close()
	saveBrain(eng, cfg.Brain.Path, log)

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = hs.Shutdown(shutCtx)

	// Give connections a moment to close cleanly.
	done := make(chan struct{})
	go func() { router.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		log.Warn("timed out waiting for connections to close")
	}
}

func brainSaver(ctx context.Context, eng *engine.Engine, path string, every time.Duration, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			saveBrain(eng, path, log)
		}
	}
}

// ambientTicker drives the engine's self-initiated ambient chatter on a timer.
func ambientTicker(ctx context.Context, eng *engine.Engine, out engine.Sender, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			eng.Tick(out)
		}
	}
}

// rpgTicker drives IdleRPG level progression on a timer.
func rpgTicker(ctx context.Context, m *idlerpg.Manager) {
	t := time.NewTicker(m.Interval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Tick()
		}
	}
}

func saveBrain(eng *engine.Engine, path string, log *slog.Logger) {
	if path == "" {
		return
	}
	if err := eng.Brain().Save(path); err != nil {
		log.Warn("brain save failed", "err", err)
		return
	}
	log.Info("brain saved", "path", path, "states", eng.Brain().Size())
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
