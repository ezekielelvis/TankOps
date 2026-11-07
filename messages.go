package main

import "fmt"

type ServerMessageType int

const (
	ServerStartGame        ServerMessageType = 1
	ServerTurnResults      ServerMessageType = 2
	ServerRoomJoined       ServerMessageType = 3
	ServerRoomDisconnected ServerMessageType = 4
	ServerGameFinished     ServerMessageType = 5
)

type ServerMessage interface {
	isServerMessage()
}

type StartGameMessage struct {
	Type   ServerMessageType `json:"type"`
	Config ClientConfig      `json:"config"`
	// RoomCode and Token let a client that lost its connection resume.
	RoomCode string `json:"roomCode"`
	Token    string `json:"token"`
}

func (m StartGameMessage) isServerMessage() {}

type TurnResultsMessage struct {
	Type        ServerMessageType `json:"type"`
	TurnResults []TurnResult      `json:"turnResults"`
}

func (m TurnResultsMessage) isServerMessage() {}

type RoomJoinedMessage struct {
	Type ServerMessageType `json:"type"`
}

func (m RoomJoinedMessage) isServerMessage() {}

type RoomDisconnectedMessage struct {
	Type ServerMessageType `json:"type"`
}

func (m RoomDisconnectedMessage) isServerMessage() {}

type GameFinishedMessage struct {
	Type   ServerMessageType `json:"type"`
	Result GameResult        `json:"result"`
}

func (m GameFinishedMessage) isServerMessage() {}

func newStartGameMessage(config ClientConfig, roomCode, token string) StartGameMessage {
	return StartGameMessage{ServerStartGame, config, roomCode, token}
}

func newTurnResultsMessage(results []TurnResult) TurnResultsMessage {
	return TurnResultsMessage{ServerTurnResults, results}
}

func newRoomJoinedMessage() RoomJoinedMessage {
	return RoomJoinedMessage{ServerRoomJoined}
}

func newRoomDisconnectedMessage() RoomDisconnectedMessage {
	return RoomDisconnectedMessage{ServerRoomDisconnected}
}

func newGameFinishedMessage(result GameResult) GameFinishedMessage {
	return GameFinishedMessage{ServerGameFinished, result}
}

type ClientMessageType int

const (
	ClientJoinRoom ClientMessageType = 1
	ClientSendTurn ClientMessageType = 2
	ClientQuitRoom ClientMessageType = 3
	// ClientResume rejoins a running game after a dropped connection.
	ClientResume ClientMessageType = 4
)

type ClientMessage struct {
	Type     ClientMessageType `json:"type"`
	Actions  []TankAction      `json:"Actions"`
	RoomCode string            `json:"roomCode"`
	// VsBot starts a private game against the computer instead of joining
	// the room named by RoomCode. Difficulty is a BotLevel (1 easy .. 3 hard).
	VsBot      bool `json:"vsBot"`
	Difficulty int  `json:"difficulty"`
	// Token proves which seat a ClientResume wants back.
	Token string `json:"token"`
}

const (
	maxRoomCodeLen = 8 // bot games use longer generated codes, which only the server creates
	maxActions     = 8
	maxPathLen     = 16
)

// validate rejects malformed client messages before they reach the game.
func (m ClientMessage) validate() error {
	switch m.Type {
	case ClientJoinRoom:
		if m.VsBot {
			if BotLevel(m.Difficulty) < BotEasy || BotLevel(m.Difficulty) > BotHard {
				return fmt.Errorf("invalid difficulty %d", m.Difficulty)
			}
			return nil
		}
		if len(m.RoomCode) == 0 || len(m.RoomCode) > maxRoomCodeLen {
			return fmt.Errorf("room code must be 1-%d characters", maxRoomCodeLen)
		}
		for _, c := range m.RoomCode {
			ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
			if !ok {
				return fmt.Errorf("room code must be alphanumeric")
			}
		}
	case ClientSendTurn:
		if len(m.Actions) > maxActions {
			return fmt.Errorf("too many actions: %d", len(m.Actions))
		}
		for _, a := range m.Actions {
			if len(a.Path) > maxPathLen {
				return fmt.Errorf("path too long: %d", len(a.Path))
			}
			if a.Type != TankMove && a.Type != TankFire {
				return fmt.Errorf("unknown action type %d", a.Type)
			}
		}
	case ClientQuitRoom:
	case ClientResume:
		if len(m.RoomCode) == 0 || len(m.RoomCode) > 16 || len(m.Token) == 0 || len(m.Token) > 64 {
			return fmt.Errorf("invalid resume request")
		}
	default:
		return fmt.Errorf("unknown message type %d", m.Type)
	}
	return nil
}
