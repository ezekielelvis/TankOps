package server

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	mrand "math/rand/v2"
	"time"

	"tank-ops/internal/game"
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
	// defaultGrace is how long a dropped player has to reconnect.
	defaultGrace = 30 * time.Second
)

type RoomMessage struct {
	msgType     RoomMessageType
	config      game.ClientConfig
	roomCode    string
	token       string
	turnResults []game.TurnResult
	gameResult  game.GameResult
}

type QueuedActions struct {
	actions []game.TankAction
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

	gamestate *game.GameState

	queuedP1 QueuedActions
	queuedP2 QueuedActions

	turnTimeout time.Duration
	turnTimer   *time.Timer

	// reconnection support
	grace        time.Duration
	tokens       [2]string
	configs      [2]game.ClientConfig
	history      [2][][]game.TurnResult // every turn's results, per seat, for replay
	disconnected [2]bool
	graceGen     [2]int
	graceExpired chan graceExpiry
}

type graceExpiry struct {
	seat, gen int
}

func newToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func seatIdx(isP1 bool) int {
	if isP1 {
		return 0
	}
	return 1
}

func (r *Room) chansFor(seat int) *RoomChans {
	if seat == 0 {
		return &r.player1chans
	}
	return &r.player2chans
}

func NewRoom(code string, close chan string) *Room {
	return NewRoomWithTimeout(code, close, defaultTurnTimeout)
}

func NewRoomWithTimeout(code string, close chan string, turnTimeout time.Duration) *Room {
	room := &Room{
		code: code,
		// buffered so the hub never blocks on a busy room
		requests:     make(chan RoomRequest, 8),
		close:        close,
		turnTimeout:  turnTimeout,
		grace:        defaultGrace,
		graceExpired: make(chan graceExpiry, 4),
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
		case ge := <-r.graceExpired:
			r.running.handleGraceExpired(ge)
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

func (r *Room) startMessage(seat int) RoomMessage {
	return RoomMessage{msgType: RoomGameStarted, config: r.configs[seat], roomCode: r.code, token: r.tokens[seat]}
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

func (r *Room) QueueActions(actions []game.TankAction, p1 bool) bool {
	if p1 {
		r.queuedP1.queued = true
		r.queuedP1.actions = actions
	} else {
		r.queuedP2.queued = true
		r.queuedP2.actions = actions
	}
	return r.queuedP1.queued && r.queuedP2.queued
}

// ready reports whether the turn can resolve. The game pauses while a player
// is disconnected, so they can come back to the same turn.
func (r *Room) ready() bool {
	return r.queuedP1.queued && r.queuedP2.queued && !r.disconnected[0] && !r.disconnected[1]
}

func (r *Room) ClearActions() {
	r.queuedP1.queued = false
	r.queuedP1.actions = []game.TankAction{}
	r.queuedP2.queued = false
	r.queuedP2.actions = []game.TankAction{}
}

func (r *Room) Actions() ([]game.TankAction, []game.TankAction) {
	return r.queuedP1.actions, r.queuedP2.actions
}

// resolveTurn plays the queued actions and, if the game ended, tells both
// players and closes the room.
func (r *Room) resolveTurn() {
	p1, p2 := r.Actions()
	r.ClearActions()

	results1, results2 := r.gamestate.ResolveActions(p1, p2)
	r.history[0] = append(r.history[0], results1)
	r.history[1] = append(r.history[1], results2)
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
func (r *Room) finish(res1, res2 game.GameResult) {
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
	p1First := mrand.IntN(2) == 0
	s.r.gamestate = game.NewGameState(game.NewRandomConfig(p1First))
	cfg1, cfg2 := s.r.gamestate.ClientConfigs()
	s.r.configs = [2]game.ClientConfig{cfg1, cfg2}
	s.r.tokens = [2]string{newToken(), newToken()}
	metrics.gamesStarted.Add(1)

	s.r.deliver(s.r.player1chans, s.r.startMessage(0))
	s.r.deliver(s.r.player2chans, s.r.startMessage(1))

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
	case PlayerQuitRoom, PlayerDisconnected:
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
	if req.resumeToken == "" {
		// room is full
		close(req.chans.read)
		return false
	}
	seat := -1
	for i, tok := range s.r.tokens {
		if tok != "" && tok == req.resumeToken {
			seat = i
		}
	}
	if seat < 0 {
		close(req.chans.read)
		return false
	}
	r := s.r
	c := r.chansFor(seat)
	if c.read != nil {
		r.closePlayer(c) // replace a connection the server has not noticed dying yet
	}
	*c = req.chans
	r.disconnected[seat] = false
	r.graceGen[seat]++ // cancels any pending forfeit
	slog.Info("player resumed", "room", r.code, "seat", seat)
	metrics.resumes.Add(1)
	if !r.disconnected[0] && !r.disconnected[1] {
		r.resetTurnTimer() // the game was paused
	}

	r.deliver(*c, RoomMessage{msgType: RoomJoined})
	r.deliver(*c, r.startMessage(seat))
	for _, results := range r.history[seat] {
		r.deliver(*c, RoomMessage{msgType: RoomTurnResult, turnResults: results})
	}
	return false
}

func (s RoomStateRunning) handlePlayerMessage(msg PlayerMessage, isP1 bool) {
	switch msg.msgType {
	case PlayerQuitRoom:
		res1, res2 := game.Win, game.Win
		if isP1 {
			res1 = game.Lose
		} else {
			res2 = game.Lose
		}
		s.r.finish(res1, res2)
	case PlayerDisconnected:
		s.handleDisconnect(seatIdx(isP1))
	case PlayerSendTurn:
		s.r.QueueActions(msg.tankActions, isP1)
		if s.r.ready() {
			s.r.resolveTurn()
		}
	}
}

// handleDisconnect pauses the game and keeps the seat open for a grace
// period so the player can resume.
func (s RoomStateRunning) handleDisconnect(seat int) {
	r := s.r
	r.closePlayer(r.chansFor(seat))
	r.disconnected[seat] = true
	r.graceGen[seat]++
	gen := r.graceGen[seat]
	r.stopTurnTimer()
	slog.Info("player disconnected, holding seat", "room", r.code, "seat", seat, "grace", r.grace)
	ch := r.graceExpired
	time.AfterFunc(r.grace, func() {
		select {
		case ch <- graceExpiry{seat, gen}:
		default:
		}
	})
	if r.ready() {
		r.resolveTurn()
	}
}

func (s RoomStateRunning) handleGraceExpired(ge graceExpiry) {
	r := s.r
	if r.state != RoomState(r.running) || !r.disconnected[ge.seat] || r.graceGen[ge.seat] != ge.gen {
		return // resumed in time, or the game is over
	}
	slog.Info("grace period over, player forfeits", "room", r.code, "seat", ge.seat)
	if r.disconnected[1-ge.seat] {
		r.abort() // nobody is left to tell
		return
	}
	res1, res2 := game.Win, game.Win
	if ge.seat == 0 {
		res1 = game.Lose
	} else {
		res2 = game.Lose
	}
	r.finish(res1, res2)
}

// handleTurnTimeout resolves the turn with whatever has been submitted; a
// player who has not submitted anything simply does nothing this turn.
func (s RoomStateRunning) handleTurnTimeout() {
	s.r.turnTimer = nil
	if s.r.state != RoomState(s.r.running) || s.r.disconnected[0] || s.r.disconnected[1] {
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
