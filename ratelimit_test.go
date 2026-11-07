package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterBurstAndRefill(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newIPLimiter(1, 3, 100)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.acquire("1.2.3.4") {
			t.Fatalf("connection %d within the burst should pass", i)
		}
		l.release("1.2.3.4")
	}
	if l.acquire("1.2.3.4") {
		t.Fatal("burst is used up")
	}
	if !l.acquire("5.6.7.8") {
		t.Fatal("other addresses are unaffected")
	}
	now = now.Add(2 * time.Second)
	if !l.acquire("1.2.3.4") {
		t.Fatal("tokens should have refilled")
	}
}

func TestLimiterCapsConcurrentConnections(t *testing.T) {
	l := newIPLimiter(1000, 1000, 2)
	if !l.acquire("a") || !l.acquire("a") {
		t.Fatal("two connections should pass")
	}
	if l.acquire("a") {
		t.Fatal("third concurrent connection should be refused")
	}
	l.release("a")
	if !l.acquire("a") {
		t.Fatal("a freed slot can be reused")
	}
}

func TestLimiterForgetsIdleClients(t *testing.T) {
	l := newIPLimiter(1000, 5, 5)
	l.acquire("x")
	l.release("x")
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.clients) > 1 {
		t.Fatalf("map should not grow, has %d", len(l.clients))
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.RemoteAddr = "9.9.9.9:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	if got := clientIP(r, false); got != "9.9.9.9" {
		t.Fatalf("untrusted proxy: got %q", got)
	}
	if got := clientIP(r, true); got != "1.1.1.1" {
		t.Fatalf("trusted proxy: got %q", got)
	}
}

func TestServeWsRefusesWhenLimited(t *testing.T) {
	hub := NewHub().WithLimits(0.0001, 1, 5, false)
	go hub.Run()
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer srv.Close()
	url := "ws" + srv.URL[len("http"):]

	c := dial(t, url) // the one allowed connection
	_ = c
	_, resp, err := websocketDialRaw(url)
	if err == nil {
		t.Fatal("second connection should be refused")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %v", resp)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	rec := httptest.NewRecorder()
	metrics.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, name := range []string{"tankops_connections", "tankops_rooms", "tankops_games_started_total"} {
		if !contains(body, name) {
			t.Errorf("metrics output is missing %s", name)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
