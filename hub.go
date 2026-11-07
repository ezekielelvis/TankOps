package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
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
	bot BotLevel
}

type Hub struct {
	roomRequests chan RoomRequest
	botGames     int
}

func NewHub() *Hub {
	return &Hub{roomRequests: make(chan RoomRequest)}
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
			if !ok {
				room = NewRoom(msg.code, closeReq)
				go room.Run()
				rooms[msg.code] = room
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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}

	player := NewPlayer(conn, h.roomRequests)
	go player.Run()
}
