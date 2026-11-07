package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"

	"tank-ops/internal/bot"
	"tank-ops/internal/game"
)

const (
	maxMessageSize = 8 * 1024
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	writeWait      = 10 * time.Second
)

type PlayerMessageType int

const (
	PlayerSendTurn PlayerMessageType = 1
	PlayerQuitRoom PlayerMessageType = 2
	// PlayerDisconnected means the connection dropped (not a deliberate quit),
	// so a running game may keep the seat open for a while.
	PlayerDisconnected PlayerMessageType = 3
)

type RoomChans struct {
	// player reads, room writes
	read chan RoomMessage
	// room reads, player writes
	send chan PlayerMessage
}

type PlayerMessage struct {
	msgType     PlayerMessageType
	roomCode    string
	tankActions []game.TankAction
	chans       RoomChans
}

type Player struct {
	notInRoom      PlayerStateNotInRoom
	waitingForRoom PlayerStateWaitingForRoom
	inRoom         PlayerStateInRoom
	waitForClose   PlayerStateWaitForClose
	state          PlayerState

	conn     *websocket.Conn
	wsClosed bool

	roomRequests chan RoomRequest
	clientRead   chan ClientMessage
	room         RoomChans

	done chan struct{}
}

func NewPlayer(conn *websocket.Conn, roomRequests chan RoomRequest) *Player {
	p := &Player{
		conn:         conn,
		roomRequests: roomRequests,
		clientRead:   make(chan ClientMessage),
	}
	p.notInRoom = PlayerStateNotInRoom{p}
	p.waitingForRoom = PlayerStateWaitingForRoom{p}
	p.inRoom = PlayerStateInRoom{p}
	p.waitForClose = PlayerStateWaitForClose{p}
	p.setState(p.notInRoom)

	return p
}

func (p *Player) Run() {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("player panicked", "panic", rec)
			p.conn.Close()
		}
	}()
	go p.readPump()
	for {
		select {
		case cm, ok := <-p.clientRead:
			if ok {
				if err := cm.validate(); err != nil {
					slog.Warn("ignoring invalid client message", "err", err)
					if (cm.Type == ClientJoinRoom || cm.Type == ClientResume) && p.state == PlayerState(p.notInRoom) {
						// let the client know the join failed
						if p.Write(newRoomDisconnectedMessage()) != nil {
							return
						}
					}
					continue
				}
			}
			if p.state.handleClientMessage(cm, ok) {
				return
			}
		case rm, ok := <-p.room.read:
			if p.state.handleRoomMessage(rm, ok) {
				return
			}
		}
	}
}

func (p *Player) setState(state PlayerState) {
	p.state = state
}

func (p *Player) Write(msg ServerMessage) error {
	if p.wsClosed {
		return errors.New("conn closed")
	}
	_ = p.conn.SetWriteDeadline(time.Now().Add(writeWait))
	err := p.conn.WriteJSON(msg)
	if err != nil {
		p.wsClosed = true
		p.conn.Close()
		return err
	}
	return nil
}

func (p *Player) readPump() {
	defer func() {
		p.conn.Close()
		close(p.clientRead)
	}()

	p.conn.SetReadLimit(maxMessageSize)
	_ = p.conn.SetReadDeadline(time.Now().Add(pongWait))
	p.conn.SetPongHandler(func(string) error {
		return p.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	stopPing := make(chan struct{})
	defer close(stopPing)
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// WriteControl is safe to call concurrently with other writes.
				err := p.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
				if err != nil {
					return
				}
			case <-stopPing:
				return
			}
		}
	}()

	for {
		msgType, msg, err := p.conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType != websocket.TextMessage {
			return
		}

		clientMsg := ClientMessage{}
		err = json.Unmarshal(msg, &clientMsg)
		if err != nil {
			return
		}
		// An idle but alive client keeps the deadline fresh through pongs;
		// any real message does too.
		_ = p.conn.SetReadDeadline(time.Now().Add(pongWait))

		p.clientRead <- clientMsg
	}
}

type PlayerState interface {
	handleClientMessage(cm ClientMessage, ok bool) bool
	handleRoomMessage(rm RoomMessage, ok bool) bool
}

type PlayerStateNotInRoom struct {
	player *Player
}

func (ps PlayerStateNotInRoom) handleClientMessage(cm ClientMessage, ok bool) bool {
	if !ok {
		ps.player.clientRead = nil
		return true
	}
	switch cm.Type {
	case ClientJoinRoom, ClientResume:
		chans := RoomChans{
			read: make(chan RoomMessage),
			send: make(chan PlayerMessage),
		}
		ps.player.room = chans
		ps.player.done = make(chan struct{})

		req := RoomRequest{code: cm.RoomCode, chans: chans}
		if cm.Type == ClientResume {
			req.resumeToken = cm.Token
		}
		if cm.VsBot {
			req.bot = bot.Level(cm.Difficulty)
		}
		go func(req RoomRequest) {
			ps.player.roomRequests <- req
		}(req)

		ps.player.setState(ps.player.waitingForRoom)
	case ClientQuitRoom:
		slog.Warn("illegal client message for current state")
	case ClientSendTurn:
		slog.Warn("illegal client message for current state")
	}
	return false
}

func (ps PlayerStateNotInRoom) handleRoomMessage(rm RoomMessage, ok bool) bool {
	slog.Warn("room message while not in a room")
	return false
}

type PlayerStateWaitingForRoom struct {
	player *Player
}

func (ps PlayerStateWaitingForRoom) handleClientMessage(cm ClientMessage, ok bool) bool {
	if !ok {
		ps.player.clientRead = nil
		ch := ps.player.room.send
		done := ps.player.done
		go func(ch chan PlayerMessage, done chan struct{}) {
			select {
			case ch <- PlayerMessage{msgType: PlayerDisconnected}:
			case <-done:
			}
		}(ch, done)
		ps.player.setState(ps.player.waitForClose)
		return false
	}
	switch cm.Type {
	case ClientJoinRoom:
		slog.Warn("illegal client message for current state")
	case ClientQuitRoom:
		ch := ps.player.room.send
		done := ps.player.done
		go func(ch chan PlayerMessage, done chan struct{}) {
			select {
			case ch <- PlayerMessage{msgType: PlayerQuitRoom}:
			case <-done:
			}
		}(ch, done)
	case ClientSendTurn:
		slog.Warn("illegal client message for current state")
	}
	return false
}

func (ps PlayerStateWaitingForRoom) handleRoomMessage(rm RoomMessage, ok bool) bool {
	if !ok {
		close(ps.player.done)
		ps.player.done = nil
		ps.player.room.read = nil
		ps.player.room.send = nil
		ps.player.setState(ps.player.notInRoom)
		err := ps.player.Write(newRoomDisconnectedMessage())
		if err != nil {
			return true
		}
		return false
	}

	switch rm.msgType {
	case RoomJoined:
		ps.player.setState(ps.player.inRoom)
		err := ps.player.Write(newRoomJoinedMessage())
		if err != nil {
			ch := ps.player.room.send
			done := ps.player.done
			go func(ch chan PlayerMessage, done chan struct{}) {
				select {
				case ch <- PlayerMessage{msgType: PlayerDisconnected}:
				case <-done:
				}
			}(ch, done)
			ps.player.setState(ps.player.waitForClose)
			return false
		}
	case RoomGameStarted:
		slog.Warn("game started before joining room")
	case RoomTurnResult:
		slog.Warn("turn result before joining room")
	case RoomGameFinished:
		slog.Warn("game finished before joining room")
	}
	return false
}

type PlayerStateInRoom struct {
	player *Player
}

func (ps PlayerStateInRoom) handleClientMessage(cm ClientMessage, ok bool) bool {
	if !ok {
		ps.player.clientRead = nil
		ch := ps.player.room.send
		done := ps.player.done
		go func(ch chan PlayerMessage, done chan struct{}) {
			select {
			case ch <- PlayerMessage{msgType: PlayerDisconnected}:
			case <-done:
			}
		}(ch, done)
		ps.player.setState(ps.player.waitForClose)
		return false
	}
	switch cm.Type {
	case ClientJoinRoom:
		slog.Warn("illegal client message for current state")
	case ClientQuitRoom:
		ch := ps.player.room.send
		done := ps.player.done
		go func(ch chan PlayerMessage, done chan struct{}) {
			select {
			case ch <- PlayerMessage{msgType: PlayerQuitRoom}:
			case <-done:
			}
		}(ch, done)
	case ClientSendTurn:
		ch := ps.player.room.send
		done := ps.player.done
		go func(ch chan PlayerMessage, done chan struct{}) {
			msg := PlayerMessage{
				msgType:     PlayerSendTurn,
				tankActions: cm.Actions,
			}
			select {
			case ch <- msg:
			case <-done:
			}
		}(ch, done)
	}
	return false
}

func (ps PlayerStateInRoom) handleRoomMessage(rm RoomMessage, ok bool) bool {
	if !ok {
		close(ps.player.done)
		ps.player.done = nil
		ps.player.room.read = nil
		ps.player.room.send = nil

		err := ps.player.Write(newRoomDisconnectedMessage())
		if err != nil {
			ps.player.conn.Close()
			return true
		}
		ps.player.setState(ps.player.notInRoom)
		return false
	}

	var err error

	switch rm.msgType {
	case RoomJoined:
		slog.Warn("room joined message while already in room")
	case RoomGameStarted:
		err = ps.player.Write(newStartGameMessage(rm.config, rm.roomCode, rm.token))
	case RoomTurnResult:
		err = ps.player.Write(newTurnResultsMessage(rm.turnResults))
	case RoomGameFinished:
		err = ps.player.Write(newGameFinishedMessage(rm.gameResult))
	}

	if err != nil {
		ch := ps.player.room.send
		done := ps.player.done
		go func(ch chan PlayerMessage, done chan struct{}) {
			select {
			case ch <- PlayerMessage{msgType: PlayerDisconnected}:
			case <-done:
			}
		}(ch, done)
		ps.player.setState(ps.player.waitForClose)
	}
	return false
}

type PlayerStateWaitForClose struct {
	player *Player
}

func (ps PlayerStateWaitForClose) handleClientMessage(cm ClientMessage, ok bool) bool {
	if ok {
		return false
	}
	ps.player.clientRead = nil
	if ps.player.room.read == nil {
		return true
	}
	return false
}

func (ps PlayerStateWaitForClose) handleRoomMessage(rm RoomMessage, ok bool) bool {
	if ok {
		return false
	}
	ps.player.room.read = nil
	close(ps.player.done)
	if ps.player.clientRead == nil {
		return true
	}
	return false
}
