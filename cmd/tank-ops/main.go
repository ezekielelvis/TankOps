package main

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers on http.DefaultServeMux, served only when PPROF_ADDR is set
	"os"
	"strconv"

	"tank-ops/internal/server"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil && v > 0 {
		return v
	}
	return fallback
}

func main() {
	addr := envOr("ADDR", "localhost:8000")
	staticDir := envOr("STATIC_DIR", "web/dist")

	hub := server.NewHub().WithLimits(
		envFloat("CONNECT_RATE", 2), // new connections per second per IP
		envInt("CONNECT_BURST", 10),
		envInt("MAX_CONNS_PER_IP", 20),
		os.Getenv("TRUST_PROXY") == "1",
	)
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

	mux.Handle("/metrics", server.MetricsHandler())

	slog.Info("tank-ops listening", "addr", addr, "static", staticDir)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
