// Command annoybot runs a single annoying IRC/Twitch bot. The same binary
// becomes Arywen or Kurkutu depending on the config file it is given.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mrcupp/annoybots/internal/config"
	"github.com/mrcupp/annoybots/internal/engine"
	"github.com/mrcupp/annoybots/internal/health"
	"github.com/mrcupp/annoybots/internal/irc"
	"github.com/mrcupp/annoybots/internal/markov"
)

func main() {
	configPath := flag.String("config", envOr("ANNOYBOT_CONFIG", "config.yaml"), "path to bot config YAML")
	quoteDir := flag.String("quotes", os.Getenv("ANNOYBOT_QUOTES_DIR"), "base dir for quote-pack files (defaults to config dir)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(*configPath, *quoteDir)
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

	mgr, err := irc.NewManager(cfg.Networks, eng.Handle, log, os.Getenv)
	if err != nil {
		log.Error("build manager", "err", err)
		os.Exit(1)
	}

	hs := health.New(cfg.Health.Addr, mgr.AnyConnected)
	hs.Start()
	log.Info("health server listening", "addr", cfg.Health.Addr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mgr.Run(ctx)
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

	mgr.Quit()
	saveBrain(eng, cfg.Brain.Path, log)

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = hs.Shutdown(shutCtx)

	// Give connections a moment to close cleanly.
	done := make(chan struct{})
	go func() { mgr.Wait(); close(done) }()
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
