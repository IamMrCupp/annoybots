// Command dashboard serves a read-only web view of the IdleRPG realm (rankings +
// the active quest) over the shared Redis state store. It is a separate process
// from the bots and never writes, so it can run wherever it can reach Redis.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/IamMrCupp/annoybots/internal/rpgweb"
	"github.com/IamMrCupp/annoybots/internal/state"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	addr := envOr("DASHBOARD_ADDR", ":8080")
	redisAddr := envOr("REDIS_ADDR", "redis:6379")
	prefix := envOr("STATE_PREFIX", "annoybots:state:")

	store := state.NewRedis(redisAddr, os.Getenv("REDIS_PASSWORD"), prefix)
	defer func() { _ = store.Close() }()

	srv := rpgweb.New(store)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Info("idlerpg dashboard listening", "addr", addr, "redis", redisAddr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("dashboard server failed", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
