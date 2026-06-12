// Package health exposes liveness/readiness probes for Kubernetes.
package health

import (
	"context"
	"net/http"
	"time"
)

// Server wraps an http.Server serving /healthz and /readyz.
type Server struct {
	http *http.Server
}

// New builds a health server. ready should report whether the bot is connected
// to at least one network (used for the readiness probe).
func New(addr string, ready func() bool) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready"))
	})
	return &Server{http: &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}}
}

// Start serves in a background goroutine.
func (s *Server) Start() {
	go func() {
		_ = s.http.ListenAndServe()
	}()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
