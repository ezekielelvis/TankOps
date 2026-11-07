package main

import (
	"testing"
	"time"
)

func newChans() RoomChans {
	return RoomChans{read: make(chan RoomMessage), send: make(chan PlayerMessage)}
}

func recv(t *testing.T, c RoomChans) RoomMessage {
	t.Helper()
	select {
	case m, ok := <-c.read:
		if !ok {
			t.Fatal("room closed the channel unexpectedly")
		}
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the room")
	}
	return RoomMessage{}
}

func TestHumanVersusBotPlaysToTheEnd(t *testing.T) {
	for _, level := range []BotLevel{BotEasy, BotMedium, BotHard} {
		t.Run(level.String(), func(t *testing.T) {
			hub := NewHub()
			go hub.Run()

			human := newChans()
			hub.roomRequests <- RoomRequest{chans: human, bot: level}

			if m := recv(t, human); m.msgType != RoomJoined {
				t.Fatalf("expected RoomJoined, got %v", m.msgType)
			}
			if m := recv(t, human); m.msgType != RoomGameStarted {
				t.Fatalf("expected RoomGameStarted, got %v", m.msgType)
			}

			// an idle human loses to the shrinking arena at the latest
			for turn := 0; turn < 60; turn++ {
				select {
				case human.send <- PlayerMessage{msgType: PlayerSendTurn}:
				case <-time.After(5 * time.Second):
					t.Fatal("room stopped accepting turns")
				}
				m := recv(t, human)
				if m.msgType != RoomTurnResult {
					t.Fatalf("expected turn results, got %v", m.msgType)
				}
				m, ok := recvMaybe(human)
				if ok && m.msgType == RoomGameFinished {
					return
				}
			}
			t.Fatal("game never finished")
		})
	}
}

// recvMaybe reads the next message if one arrives promptly.
func recvMaybe(c RoomChans) (RoomMessage, bool) {
	select {
	case m, ok := <-c.read:
		return m, ok
	case <-time.After(200 * time.Millisecond):
		return RoomMessage{}, false
	}
}

func TestTurnTimeoutResolvesIdleTurn(t *testing.T) {
	closeCh := make(chan string, 1)
	room := NewRoomWithTimeout("T", closeCh, 100*time.Millisecond)
	go room.Run()

	p1, p2 := newChans(), newChans()
	room.requests <- RoomRequest{code: "T", chans: p1}
	if m := recv(t, p1); m.msgType != RoomJoined {
		t.Fatalf("p1 expected RoomJoined, got %v", m.msgType)
	}
	room.requests <- RoomRequest{code: "T", chans: p2}
	if m := recv(t, p2); m.msgType != RoomJoined {
		t.Fatalf("p2 expected RoomJoined, got %v", m.msgType)
	}
	recv(t, p1) // game started
	recv(t, p2)

	// nobody sends anything; the timer must resolve the turn for us
	if m := recv(t, p1); m.msgType != RoomTurnResult {
		t.Fatalf("expected a timed-out turn result, got %v", m.msgType)
	}
	if m := recv(t, p2); m.msgType != RoomTurnResult {
		t.Fatalf("expected a timed-out turn result, got %v", m.msgType)
	}
}

func TestQuitDuringGameAwardsWinToOpponent(t *testing.T) {
	closeCh := make(chan string, 1)
	room := NewRoomWithTimeout("Q", closeCh, 0)
	go room.Run()

	p1, p2 := newChans(), newChans()
	room.requests <- RoomRequest{code: "Q", chans: p1}
	recv(t, p1)
	room.requests <- RoomRequest{code: "Q", chans: p2}
	recv(t, p2)
	recv(t, p1)
	recv(t, p2)

	p1.send <- PlayerMessage{msgType: PlayerQuitRoom}
	if m := recv(t, p1); m.msgType != RoomGameFinished || m.gameResult != Lose {
		t.Fatalf("quitter should lose, got %+v", m)
	}
	if m := recv(t, p2); m.msgType != RoomGameFinished || m.gameResult != Win {
		t.Fatalf("opponent should win, got %+v", m)
	}
	select {
	case code := <-closeCh:
		if code != "Q" {
			t.Fatalf("closed %q", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("room did not ask to be closed")
	}
}

// startTwoPlayerGame seats two fake players and returns their start messages.
func startTwoPlayerGame(t *testing.T, room *Room) (p1, p2 RoomChans, start1, start2 RoomMessage) {
	t.Helper()
	p1, p2 = newChans(), newChans()
	room.requests <- RoomRequest{code: room.code, chans: p1}
	recv(t, p1)
	room.requests <- RoomRequest{code: room.code, chans: p2}
	recv(t, p2)
	return p1, p2, recv(t, p1), recv(t, p2)
}

func TestPlayerCanResumeAfterDisconnect(t *testing.T) {
	closeCh := make(chan string, 1)
	room := NewRoomWithTimeout("R", closeCh, 0)
	go room.Run()
	p1, p2, start1, _ := startTwoPlayerGame(t, room)
	if start1.token == "" || start1.roomCode != "R" {
		t.Fatalf("start message must carry a token and the room code: %+v", start1)
	}

	p1.send <- PlayerMessage{msgType: PlayerDisconnected}
	// the room hangs up on the old connection
	select {
	case _, ok := <-p1.read:
		if ok {
			t.Fatal("expected the old connection to be closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("old connection was not closed")
	}

	// the game is paused while the player is away
	p2.send <- PlayerMessage{msgType: PlayerSendTurn}
	if m, got := recvMaybe(p2); got {
		t.Fatalf("turn must not resolve while a player is disconnected, got %+v", m)
	}

	back := newChans()
	room.requests <- RoomRequest{code: "R", chans: back, resumeToken: start1.token}
	if m := recv(t, back); m.msgType != RoomJoined {
		t.Fatalf("want RoomJoined, got %v", m.msgType)
	}
	if m := recv(t, back); m.msgType != RoomGameStarted || m.token != start1.token {
		t.Fatalf("want the same game start again, got %+v", m)
	}

	// the resumed player sends their orders and the paused turn resolves
	back.send <- PlayerMessage{msgType: PlayerSendTurn}
	if m := recv(t, back); m.msgType != RoomTurnResult {
		t.Fatalf("resumed player should get results, got %v", m.msgType)
	}
	if m := recv(t, p2); m.msgType != RoomTurnResult {
		t.Fatalf("the other player should get results too, got %v", m.msgType)
	}

	// a rejoin later replays the finished turn
	again := newChans()
	room.requests <- RoomRequest{code: "R", chans: again, resumeToken: start1.token}
	recv(t, again) // RoomJoined
	recv(t, again) // game start
	if m := recv(t, again); m.msgType != RoomTurnResult {
		t.Fatalf("want the turn history replayed, got %v", m.msgType)
	}
}

func TestForfeitAfterGracePeriod(t *testing.T) {
	closeCh := make(chan string, 1)
	room := NewRoomWithTimeout("G", closeCh, 0)
	room.grace = 50 * time.Millisecond
	go room.Run()
	p1, p2, _, _ := startTwoPlayerGame(t, room)

	p1.send <- PlayerMessage{msgType: PlayerDisconnected}
	if m := recv(t, p2); m.msgType != RoomGameFinished || m.gameResult != Win {
		t.Fatalf("remaining player should win after the grace period, got %+v", m)
	}
	select {
	case <-closeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("room should close")
	}
}

func TestResumeWithWrongTokenIsRefused(t *testing.T) {
	closeCh := make(chan string, 1)
	room := NewRoomWithTimeout("W", closeCh, 0)
	go room.Run()
	startTwoPlayerGame(t, room)

	imposter := newChans()
	room.requests <- RoomRequest{code: "W", chans: imposter, resumeToken: "nope"}
	select {
	case _, ok := <-imposter.read:
		if ok {
			t.Fatal("imposter should not receive anything")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("imposter was not refused")
	}
}

func TestResumeReplacesStaleConnection(t *testing.T) {
	closeCh := make(chan string, 1)
	room := NewRoomWithTimeout("S", closeCh, 0)
	go room.Run()
	p1, _, start1, _ := startTwoPlayerGame(t, room)

	// the server never noticed the old connection die (a page refresh)
	fresh := newChans()
	room.requests <- RoomRequest{code: "S", chans: fresh, resumeToken: start1.token}
	if m := recv(t, fresh); m.msgType != RoomJoined {
		t.Fatalf("want RoomJoined, got %v", m.msgType)
	}
	select {
	case _, ok := <-p1.read:
		if ok {
			t.Fatal("stale connection should have been closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stale connection was not closed")
	}
}
