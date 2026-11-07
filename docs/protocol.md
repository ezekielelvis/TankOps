# Websocket protocol

Connect to `ws://<host>/ws`. Messages are JSON text frames, max 8 KiB.
The server is authoritative; it only sends each player what they can see.

## Client to server

```json
{"type": 1, "roomCode": "AB12"}                 // join a room (1-8 letters/digits)
{"type": 1, "vsBot": true, "difficulty": 2}     // private game vs the computer (1 easy, 2 medium, 3 hard)
{"type": 2, "Actions": [ ... ]}                 // send this turn's orders
{"type": 3}                                     // leave the room (forfeits a running game)
{"type": 4, "roomCode": "AB12", "token": "..."} // resume a running game after a dropped connection
```

An action is one of

```json
{"type": 1, "id": 3, "path": [{"x":0,"y":0},{"x":1,"y":0}]}   // move: path starts on the tank, unit steps, up to 5 steps
{"type": 2, "id": 3, "dir": {"x":1,"y":0}}                     // fire: dir is one of the six unit hex steps
```

Limits: at most 8 actions, paths of at most 16 points. Each tank acts at most
once per turn; extra actions and invalid ones are ignored. A bad join is
answered with type 4; other invalid messages are ignored.

Hex directions: `(1,0) (-1,0) (0,1) (0,-1) (1,-1) (-1,1)`. Distance is
`max(|dx|, |dy|, |dx+dy|)`.

## Server to client

| type | name | payload |
| ---- | ---- | ------- |
| 1 | StartGame | `config`: tanks, hexes, obstacle sites, ranges, arena centre (`playerTanks` are yours); `roomCode` and `token` for resuming |
| 2 | TurnResults | `turnResults`: ordered events you can see (see below) |
| 3 | RoomJoined | you are in the room, waiting for an opponent |
| 4 | RoomDisconnected | the room closed or could not be joined |
| 5 | GameFinished | `result`: 1 win, 2 draw, 3 lose |

Turn result events (`type`): 1 Move2 (start/end of a move), 2 Move3 (passing
through a hex), 3 Fire, 4 Explosion (`destroyed` and `id` if a tank died), 5
Destroyed (a tank you could not see died), 6 Visible (enemy appears or
vanishes), 7 Shrink (`started: false` warns that ring `r` and beyond will
collapse at the end of this turn; `true` says it did).

Events are in the order they happened. Which side's orders resolved first
alternates every turn and is not announced.

## Turn timer

If both players have not sent orders within 90 seconds the server resolves
the turn anyway; a player who sent nothing simply does nothing.

## Reconnecting

`StartGame` carries a `roomCode` and a secret `token` for your seat. If your
connection drops, the server pauses the game and holds your seat for 30
seconds. Open a new connection and send a type 4 message with the same
`roomCode` and `token`. You then receive `RoomJoined`, `StartGame` again, and
one `TurnResults` message for every turn played so far, in order, so the
client can rebuild the board by replaying them. If the seat is gone (the grace
period passed, or the token is wrong) you get `RoomDisconnected`.

The token also lets a refreshed page replace its own old connection, even if
the server has not noticed that connection die yet.

While a player is disconnected nobody's turn resolves and the turn timer is
stopped.
