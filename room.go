package main

import (
	"log/slog"
	"math/rand/v2"
	"time"
)

type RoomMessageType int

const (
	RoomJoined       RoomMessageType = 1
	RoomGameStarted  RoomMessageType = 2
	RoomGameFinished RoomMessageType = 3
	RoomTurnResult   RoomMessageType = 4
)

const (
	// deliverTimeout bounds how long a room waits for a player goroutine to
	// accept a message, so one stuck client cannot wedge a room forever.
	deliverTimeout = 5 * time.Second
	// defaultTurnTimeout is how long a room waits for both players' orders.
	defaultTurnTimeout = 90 * time.Second
)

type RoomMessage struct {
	msgType     RoomMessageType
	config      ClientConfig
	turnResults []TurnResult
	gameResult  GameResult
}

type QueuedActions struct {
	actions []TankAction
	queued  bool
}

type Room struct {
	waitingForP1 RoomStateWaitingForP1
	waitingForP2 RoomStateWaitingForP2
	running      RoomStateRunning
	closing      RoomStateClosing
	state        RoomState

	code     string
	requests chan RoomRequest
	close    chan string

	player1chans RoomChans
	player2chans RoomChans

	gamestate *GameState

	queuedP1 QueuedActions
	queuedP2 QueuedActions

	turnTimeout time.Duration
	turnTimer   *time.Timer
}

func NewRoom(code string, close chan string) *Room {
	return NewRoomWithTimeout(code, close, defaultTurnTimeout)
}

func NewRoomWithTimeout(code string, close chan string, turnTimeout time.Duration) *Room {
	room := &Room{
		code: code,
		// buffered so the hub never blocks on a busy room
		requests:    make(chan RoomRequest, 8),
		close:       close,
		turnTimeout: turnTimeout,
	}
	room.waitingForP1 = RoomStateWaitingForP1{room}
	room.waitingForP2 = RoomStateWaitingForP2{room}
	room.running = RoomStateRunning{room}
	room.closing = RoomStateClosing{room}
	room.state = room.waitingForP1
	return room
}

func (r *Room) Run() {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("room panicked", "room", r.code, "panic", rec)
			r.abort()
		}
	}()
	defer r.stopTurnTimer()

	for {
		select {
		case joinRequest, ok := <-r.requests:
			if r.state.handleJoinRequest(joinRequest, ok) {
				return
			}
		case msg := <-r.player1chans.send:
			r.state.handlePlayerMessage(msg, true)
		case msg := <-r.player2chans.send:
			r.state.handlePlayerMessage(msg, false)
		case <-r.turnTimerC():
			r.running.handleTurnTimeout()
		}
	}
}

// abort tears the room down after an unexpected failure so players are not
// left hanging and the hub forgets the room.
func (r *Room) abort() {
	r.closePlayer(&r.player1chans)
	r.closePlayer(&r.player2chans)
	r.signalClose()
}

func (r *Room) closePlayer(c *RoomChans) {
	if c.read != nil {
		func() {
			defer func() { _ = recover() }() // already closed
			close(c.read)
		}()
	}
	c.read = nil
	c.send = nil
}

func (r *Room) signalClose() {
	ch, code := r.close, r.code
	go func() { ch <- code }()
	r.setState(r.closing)
}

func (r *Room) setState(state RoomState) {
	r.state = state
}

// deliver sends a message to a player's goroutine, giving up after
// deliverTimeout. It reports whether the message was accepted.
func (r *Room) deliver(c RoomChans, msg RoomMessage) bool {
	if c.read == nil {
		return false
	}
	t := time.NewTimer(deliverTimeout)
	defer t.Stop()
	select {
	case c.read <- msg:
		return true
	case <-t.C:
		slog.Warn("player not accepting messages", "room", r.code)
		return false
	}
}

func (r *Room) turnTimerC() <-chan time.Time {
	if r.turnTimer == nil {
		return nil
	}
	return r.turnTimer.C
}

func (r *Room) resetTurnTimer() {
	r.stopTurnTimer()
	if r.turnTimeout > 0 {
		r.turnTimer = time.NewTimer(r.turnTimeout)
	}
}

func (r *Room) stopTurnTimer() {
	if r.turnTimer != nil {
		r.turnTimer.Stop()
		r.turnTimer = nil
	}
}

func (r *Room) QueueActions(actions []TankAction, p1 bool) bool {
	if p1 {
		r.queuedP1.queued = true
		r.queuedP1.actions = actions
	} else {
		r.queuedP2.queued = true
		r.queuedP2.actions = actions
	}
	return r.queuedP1.queued && r.queuedP2.queued
}

func (r *Room) ClearActions() {
	r.queuedP1.queued = false
	r.queuedP1.actions = []TankAction{}
	r.queuedP2.queued = false
	r.queuedP2.actions = []TankAction{}
}

func (r *Room) Actions() ([]TankAction, []TankAction) {
	return r.queuedP1.actions, r.queuedP2.actions
}

// resolveTurn plays the queued actions and, if the game ended, tells both
// players and closes the room.
func (r *Room) resolveTurn() {
	p1, p2 := r.Actions()
	r.ClearActions()

	results1, results2 := r.gamestate.ResolveActions(p1, p2)
	r.deliver(r.player1chans, RoomMessage{msgType: RoomTurnResult, turnResults: results1})
	r.deliver(r.player2chans, RoomMessage{msgType: RoomTurnResult, turnResults: results2})

	res1, res2, finished := r.gamestate.Result()
	if !finished {
		r.resetTurnTimer()
		return
	}
	r.finish(res1, res2)
}

// finish reports the result to both players and closes the room.
func (r *Room) finish(res1, res2 GameResult) {
	r.stopTurnTimer()
	r.deliver(r.player1chans, RoomMessage{msgType: RoomGameFinished, gameResult: res1})
	r.deliver(r.player2chans, RoomMessage{msgType: RoomGameFinished, gameResult: res2})
	r.closePlayer(&r.player1chans)
	r.closePlayer(&r.player2chans)
	r.signalClose()
}

type RoomState interface {
	handleJoinRequest(req RoomRequest, ok bool) bool
	handlePlayerMessage(msg PlayerMessage, isP1 bool)
}

type RoomStateWaitingForP1 struct {
	r *Room
}

func (s RoomStateWaitingForP1) handleJoinRequest(req RoomRequest, ok bool) bool {
	if !ok {
		return true
	}

	s.r.player1chans = req.chans
	s.r.deliver(s.r.player1chans, RoomMessage{msgType: RoomJoined})
	s.r.setState(s.r.waitingForP2)
	return false
}

func (s RoomStateWaitingForP1) handlePlayerMessage(msg PlayerMessage, isP1 bool) {
	slog.Warn("unexpected player message while waiting for first player", "room", s.r.code)
}

type RoomStateWaitingForP2 struct {
	r *Room
}

func (s RoomStateWaitingForP2) handleJoinRequest(req RoomRequest, ok bool) bool {
	if !ok {
		return true
	}

	s.r.player2chans = req.chans
	s.r.deliver(s.r.player2chans, RoomMessage{msgType: RoomJoined})

	// Randomise who resolves first so neither side has a standing advantage.
	p1First := rand.IntN(2) == 0
	s.r.gamestate = NewGameState(NewRandomConfig(p1First))
	cfg1, cfg2 := s.r.gamestate.ClientConfigs()

	s.r.deliver(s.r.player1chans, RoomMessage{msgType: RoomGameStarted, config: cfg1})
	s.r.deliver(s.r.player2chans, RoomMessage{msgType: RoomGameStarted, config: cfg2})

	s.r.setState(s.r.running)
	s.r.resetTurnTimer()
	return false
}

func (s RoomStateWaitingForP2) handlePlayerMessage(msg PlayerMessage, isP1 bool) {
	if !isP1 {
		slog.Warn("message from second player before it joined", "room", s.r.code)
		return
	}
	switch msg.msgType {
	case PlayerQuitRoom:
		s.r.closePlayer(&s.r.player1chans)
		s.r.signalClose()
	case PlayerSendTurn:
		slog.Warn("turn sent before the game started", "room", s.r.code)
	}
}

type RoomStateRunning struct {
	r *Room
}

func (s RoomStateRunning) handleJoinRequest(req RoomRequest, ok bool) bool {
	if !ok {
		return true
	}
	// room is full
	close(req.chans.read)
	return false
}

func (s RoomStateRunning) handlePlayerMessage(msg PlayerMessage, isP1 bool) {
	switch msg.msgType {
	case PlayerQuitRoom:
		res1, res2 := Win, Win
		if isP1 {
			res1 = Lose
		} else {
			res2 = Lose
		}
		s.r.finish(res1, res2)
	case PlayerSendTurn:
		if s.r.QueueActions(msg.tankActions, isP1) {
			s.r.resolveTurn()
		}
	}
}

// handleTurnTimeout resolves the turn with whatever has been submitted; a
// player who has not submitted anything simply does nothing this turn.
func (s RoomStateRunning) handleTurnTimeout() {
	s.r.turnTimer = nil
	if s.r.state != RoomState(s.r.running) {
		return
	}
	slog.Info("turn timed out", "room", s.r.code)
	s.r.resolveTurn()
}

type RoomStateClosing struct {
	r *Room
}

func (s RoomStateClosing) handleJoinRequest(req RoomRequest, ok bool) bool {
	if !ok {
		return true
	}
	close(req.chans.read)
	return false
}

func (s RoomStateClosing) handlePlayerMessage(msg PlayerMessage, isP1 bool) {
}
