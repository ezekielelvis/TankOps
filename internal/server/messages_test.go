package server

import (
	"testing"

	"tank-ops/internal/game"
)

func TestMessageValidation(t *testing.T) {
	good := []ClientMessage{
		{Type: ClientJoinRoom, RoomCode: "ABC123"},
		{Type: ClientJoinRoom, VsBot: true, Difficulty: 2},
		{Type: ClientQuitRoom},
		{Type: ClientSendTurn, Actions: []game.TankAction{{Type: game.TankMove, Id: 1, Path: []game.Vector{{0, 0}, {1, 0}}}}},
	}
	for _, m := range good {
		if err := m.validate(); err != nil {
			t.Errorf("%+v should be valid: %v", m, err)
		}
	}
	bad := []ClientMessage{
		{Type: ClientJoinRoom},
		{Type: ClientJoinRoom, RoomCode: "waytoolongcode"},
		{Type: ClientJoinRoom, RoomCode: "a b"},
		{Type: ClientJoinRoom, VsBot: true, Difficulty: 9},
		{Type: 99},
		{Type: ClientSendTurn, Actions: make([]game.TankAction, 20)},
		{Type: ClientSendTurn, Actions: []game.TankAction{{Type: game.TankMove, Path: make([]game.Vector, 100)}}},
		{Type: ClientSendTurn, Actions: []game.TankAction{{Type: 42}}},
	}
	for _, m := range bad {
		if err := m.validate(); err == nil {
			t.Errorf("%+v should be rejected", m)
		}
	}
}
