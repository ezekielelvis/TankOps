package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// serverMetrics are simple process-wide counters, exposed in the Prometheus
// text format at /metrics.
type serverMetrics struct {
	connections  atomic.Int64 // current websocket connections
	connTotal    atomic.Int64
	rooms        atomic.Int64 // current rooms
	gamesStarted atomic.Int64
	botGames     atomic.Int64
	resumes      atomic.Int64
	rateLimited  atomic.Int64
}

var metrics = &serverMetrics{}

func (m *serverMetrics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	row := func(name, kind, help string, v int64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s %d\n", name, help, name, kind, name, v)
	}
	row("tankops_connections", "gauge", "Open websocket connections.", m.connections.Load())
	row("tankops_connections_total", "counter", "Websocket connections accepted.", m.connTotal.Load())
	row("tankops_rooms", "gauge", "Rooms currently open.", m.rooms.Load())
	row("tankops_games_started_total", "counter", "Games started.", m.gamesStarted.Load())
	row("tankops_bot_games_total", "counter", "Games started against the computer.", m.botGames.Load())
	row("tankops_resumes_total", "counter", "Players who resumed after a dropped connection.", m.resumes.Load())
	row("tankops_rate_limited_total", "counter", "Connections refused by the rate limiter.", m.rateLimited.Load())
}
