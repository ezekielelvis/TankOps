package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"tank-ops/internal/bot"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// CheckOrigin is left at its default: the Origin header must match Host.
}

// roomRequestTimeout bounds how long the hub waits to hand a request to a room.
const roomRequestTimeout = 2 * time.Second

type RoomRequest struct {
	code  string
	chans RoomChans
	// bot is non-zero when the player asked for a game against the computer.
	bot bot.Level
	// resumeToken is set when a player is trying to rejoin a running game.
	resumeToken string
}

type Hub struct {
	roomRequests chan RoomRequest
	botGames     int

	limiter    *ipLimiter // nil means unlimited
	trustProxy bool
}

func NewHub() *Hub {
	return &Hub{roomRequests: make(chan RoomRequest)}
}

// WithLimits turns on per-IP connection limits.
func (h *Hub) WithLimits(perSecond float64, burst, maxConns int, trustProxy bool) *Hub {
	h.limiter = newIPLimiter(perSecond, burst, maxConns)
	h.trustProxy = trustProxy
	go func() {
		for range time.Tick(time.Minute) {
			h.limiter.sweep()
		}
	}()
	return h
}

// sendToRoom hands a request to a room without ever blocking the hub for long.
// If the room cannot take it, the requester is told the room is unavailable.
func sendToRoom(room *Room, req RoomRequest) bool {
	t := time.NewTimer(roomRequestTimeout)
	defer t.Stop()
	select {
	case room.requests <- req:
		return true
	case <-t.C:
		slog.Warn("room did not accept request", "room", room.code)
		close(req.chans.read)
		return false
	}
}

func (h *Hub) Run() {
	rooms := make(map[string]*Room)
	closeReq := make(chan string)

	for {
		select {
		case msg := <-h.roomRequests:
			if msg.bot != 0 {
				h.startBotGame(msg, rooms, closeReq)
				break
			}
			room, ok := rooms[msg.code]
			if msg.resumeToken != "" {
				if !ok {
					// nothing to resume: tell the client the room is gone
					close(msg.chans.read)
					break
				}
				sendToRoom(room, msg)
				break
			}
			if !ok {
				room = NewRoom(msg.code, closeReq)
				go room.Run()
				rooms[msg.code] = room
				metrics.rooms.Add(1)
			}
			sendToRoom(room, msg)
		case code := <-closeReq:
			room, ok := rooms[code]
			if !ok {
				slog.Warn("close requested for unknown room", "room", code)
				break
			}
			close(room.requests)
			delete(rooms, code)
			metrics.rooms.Add(-1)
		}
	}
}

// startBotGame creates a private room, seats the human as player one and the
// computer as player two.
func (h *Hub) startBotGame(msg RoomRequest, rooms map[string]*Room, closeReq chan string) {
	h.botGames++
	code := fmt.Sprintf("bot-%d", h.botGames)
	room := NewRoom(code, closeReq)
	go room.Run()
	rooms[code] = room
	metrics.rooms.Add(1)
	metrics.botGames.Add(1)

	if !sendToRoom(room, RoomRequest{code: code, chans: msg.chans}) {
		return
	}
	botChans := RoomChans{
		read: make(chan RoomMessage),
		send: make(chan PlayerMessage),
	}
	go runBot(msg.bot, botChans, time.Now().UnixNano())
	sendToRoom(room, RoomRequest{code: code, chans: botChans})
}

func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) {
	ip := ""
	if h.limiter != nil {
		ip = clientIP(r, h.trustProxy)
		if !h.limiter.acquire(ip) {
			metrics.rateLimited.Add(1)
			http.Error(w, "too many connections", http.StatusTooManyRequests)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("websocket upgrade failed", "err", err)
		if h.limiter != nil {
			h.limiter.release(ip)
		}
		return
	}

	metrics.connections.Add(1)
	metrics.connTotal.Add(1)
	player := NewPlayer(conn, h.roomRequests)
	go func() {
		player.Run()
		metrics.connections.Add(-1)
		if h.limiter != nil {
			h.limiter.release(ip)
		}
	}()
}
