// Command annoybot runs a single annoying IRC/Twitch bot. The same binary
// becomes Arywen or Kurkutu depending on the config file it is given.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mrcupp/annoybots/internal/admin"
	"github.com/mrcupp/annoybots/internal/bot"
	"github.com/mrcupp/annoybots/internal/botnet"
	"github.com/mrcupp/annoybots/internal/config"
	"github.com/mrcupp/annoybots/internal/discord"
	"github.com/mrcupp/annoybots/internal/engine"
	"github.com/mrcupp/annoybots/internal/health"
	"github.com/mrcupp/annoybots/internal/irc"
	"github.com/mrcupp/annoybots/internal/markov"
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
		if rerr := adminMgr.Run(ctx); rerr != nil {
			log.Error("admin console failed to start", "err", rerr)
		} else {
			log.Info("admin console enabled", "admins", len(cfg.Admin.Admins))
		}
	}

	handler := func(m engine.Message) {
		isOther := !strings.EqualFold(m.Nick, m.Self) && !eng.IsSibling(m.Nick)
		// Admin commands (DM-only) are handled first and never reach the engine.
		if adminMgr != nil && isOther && adminMgr.Handle(ctx, m) {
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

	if len(ircNets) > 0 {
		mgr, merr := irc.NewManager(ircNets, handler, log, os.Getenv)
		if merr != nil {
			log.Error("build irc transport", "err", merr)
			os.Exit(1)
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

	<-ctx.Done()
	log.Info("shutting down")

	router.Quit()
	if bus != nil {
		_ = bus.Close()
	}
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
