package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ipLimiter limits how fast, and how many at once, a single address can open
// websocket connections. It is deliberately small: a token bucket per IP plus
// a cap on concurrent connections.
type ipLimiter struct {
	mu       sync.Mutex
	clients  map[string]*ipState
	rate     float64 // tokens per second
	burst    float64
	maxConns int
	now      func() time.Time
}

type ipState struct {
	tokens float64
	last   time.Time
	conns  int
}

func newIPLimiter(perSecond float64, burst, maxConns int) *ipLimiter {
	return &ipLimiter{
		clients:  make(map[string]*ipState),
		rate:     perSecond,
		burst:    float64(burst),
		maxConns: maxConns,
		now:      time.Now,
	}
}

// acquire reports whether a new connection from ip is allowed. If it is, the
// caller must call release when the connection ends.
func (l *ipLimiter) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	st, ok := l.clients[ip]
	if !ok {
		st = &ipState{tokens: l.burst, last: now}
		l.clients[ip] = st
	}
	st.tokens = min(l.burst, st.tokens+now.Sub(st.last).Seconds()*l.rate)
	st.last = now
	if st.tokens < 1 || st.conns >= l.maxConns {
		return false
	}
	st.tokens--
	st.conns++
	return true
}

func (l *ipLimiter) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if st, ok := l.clients[ip]; ok {
		st.conns--
		l.gcLocked(ip, st)
	}
}

// gcLocked forgets idle clients so the map cannot grow without bound.
func (l *ipLimiter) gcLocked(ip string, st *ipState) {
	if st.conns <= 0 && st.tokens >= l.burst-0.001 {
		delete(l.clients, ip)
	}
}

// sweep drops idle entries; run it periodically.
func (l *ipLimiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for ip, st := range l.clients {
		if st.conns <= 0 && now.Sub(st.last) > time.Minute {
			delete(l.clients, ip)
		}
	}
}

// clientIP returns the caller's address. X-Forwarded-For is only trusted when
// the operator says a reverse proxy is in front (TRUST_PROXY=1).
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if ip := strings.TrimSpace(first); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
