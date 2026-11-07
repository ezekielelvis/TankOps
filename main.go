package main

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers on http.DefaultServeMux, served only when PPROF_ADDR is set
	"os"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	addr := envOr("ADDR", "localhost:8000")
	staticDir := envOr("STATIC_DIR", "web/dist")

	hub := NewHub()
	go hub.Run()

	// pprof is opt-in and bound to its own (normally loopback) address.
	if pprofAddr := os.Getenv("PPROF_ADDR"); pprofAddr != "" {
		go func() {
			slog.Info("pprof listening", "addr", pprofAddr)
			if err := http.ListenAndServe(pprofAddr, http.DefaultServeMux); err != nil {
				slog.Error("pprof server stopped", "err", err)
			}
		}()
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(staticDir)))
	mux.HandleFunc("/ws", hub.ServeWs)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	slog.Info("tank-ops listening", "addr", addr, "static", staticDir)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
