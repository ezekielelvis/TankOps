package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	hub := NewHub()
	go hub.Run()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.ServeWs)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func readType(t *testing.T, c *websocket.Conn) ServerMessageType {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env struct {
		Type ServerMessageType `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env.Type
}

func TestWebsocketGameAgainstBot(t *testing.T) {
	_, url := newTestServer(t)
	c := dial(t, url)

	if err := c.WriteJSON(map[string]any{"type": ClientJoinRoom, "vsBot": true, "difficulty": 2}); err != nil {
		t.Fatal(err)
	}
	if got := readType(t, c); got != ServerRoomJoined {
		t.Fatalf("want RoomJoined, got %d", got)
	}
	if got := readType(t, c); got != ServerStartGame {
		t.Fatalf("want StartGame, got %d", got)
	}

	// read in the background; gorilla connections cannot survive a read timeout
	types := make(chan ServerMessageType, 256)
	go func() {
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				close(types)
				return
			}
			var env struct {
				Type ServerMessageType `json:"type"`
			}
			_ = json.Unmarshal(data, &env)
			types <- env.Type
		}
	}()

	for i := 0; i < 60; i++ {
		if err := c.WriteJSON(map[string]any{"type": ClientSendTurn, "Actions": []any{}}); err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case typ, ok := <-types:
				if !ok {
					t.Fatal("connection closed before the game finished")
				}
				if typ == ServerGameFinished {
					return
				}
				if typ != ServerTurnResults {
					t.Fatalf("unexpected message type %d", typ)
				}
			case <-time.After(300 * time.Millisecond):
				goto nextTurn
			}
		}
	nextTurn:
	}
	t.Fatal("game never finished")
}

func TestWebsocketInvalidJoinIsRejectedNotFatal(t *testing.T) {
	_, url := newTestServer(t)
	c := dial(t, url)

	_ = c.WriteJSON(map[string]any{"type": ClientJoinRoom, "roomCode": "bad code!"})
	if got := readType(t, c); got != ServerRoomDisconnected {
		t.Fatalf("want RoomDisconnected, got %d", got)
	}
	// the connection is still usable afterwards
	_ = c.WriteJSON(map[string]any{"type": ClientJoinRoom, "roomCode": "OK1"})
	if got := readType(t, c); got != ServerRoomJoined {
		t.Fatalf("want RoomJoined, got %d", got)
	}
}

func TestWebsocketTwoHumansShareARoom(t *testing.T) {
	_, url := newTestServer(t)
	a, b := dial(t, url), dial(t, url)

	_ = a.WriteJSON(map[string]any{"type": ClientJoinRoom, "roomCode": "ROOM"})
	if readType(t, a) != ServerRoomJoined {
		t.Fatal("a should join")
	}
	_ = b.WriteJSON(map[string]any{"type": ClientJoinRoom, "roomCode": "ROOM"})
	if readType(t, b) != ServerRoomJoined {
		t.Fatal("b should join")
	}
	if readType(t, a) != ServerStartGame || readType(t, b) != ServerStartGame {
		t.Fatal("both should get StartGame")
	}
	_ = a.WriteJSON(map[string]any{"type": ClientQuitRoom})
	if readType(t, b) != ServerGameFinished {
		t.Fatal("b should be told the game finished")
	}
}

func TestOversizedMessageDropsConnection(t *testing.T) {
	_, url := newTestServer(t)
	c := dial(t, url)
	_ = c.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", maxMessageSize*2)))
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("expected the server to close the connection")
	}
}

func TestWebsocketResumeAfterDroppedConnection(t *testing.T) {
	_, url := newTestServer(t)
	c := dial(t, url)

	_ = c.WriteJSON(map[string]any{"type": ClientJoinRoom, "vsBot": true, "difficulty": 1})
	readType(t, c) // RoomJoined
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var start struct {
		Type     ServerMessageType `json:"type"`
		RoomCode string            `json:"roomCode"`
		Token    string            `json:"token"`
	}
	if err := json.Unmarshal(data, &start); err != nil || start.Type != ServerStartGame {
		t.Fatalf("expected StartGame, got %s (%v)", data, err)
	}
	if start.RoomCode == "" || start.Token == "" {
		t.Fatalf("StartGame must carry roomCode and token: %s", data)
	}

	_ = c.WriteJSON(map[string]any{"type": ClientSendTurn, "Actions": []any{}})
	if readType(t, c) != ServerTurnResults {
		t.Fatal("expected turn results")
	}
	c.Close() // connection drops

	back := dial(t, url)
	_ = back.WriteJSON(map[string]any{"type": ClientResume, "roomCode": start.RoomCode, "token": start.Token})
	want := []ServerMessageType{ServerRoomJoined, ServerStartGame, ServerTurnResults}
	for i, w := range want {
		if got := readType(t, back); got != w {
			t.Fatalf("message %d: want type %d, got %d", i, w, got)
		}
	}
}

func TestWebsocketResumeUnknownRoom(t *testing.T) {
	_, url := newTestServer(t)
	c := dial(t, url)
	_ = c.WriteJSON(map[string]any{"type": ClientResume, "roomCode": "bot-99", "token": "abc"})
	if got := readType(t, c); got != ServerRoomDisconnected {
		t.Fatalf("want RoomDisconnected, got %d", got)
	}
}

func websocketDialRaw(url string) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial(url, nil)
}
