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
