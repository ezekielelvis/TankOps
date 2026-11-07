package server

import (
	"log/slog"

	"tank-ops/internal/bot"
)

// runBot drives a Bot through the same channels a human player's goroutine
// uses, so the room cannot tell the difference.
func runBot(level bot.Level, chans RoomChans, seed int64) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("bot panicked", "panic", rec)
		}
	}()

	b := bot.New(level, uint64(seed))
	started := false
	var pending *PlayerMessage
	for {
		// Only try to send when there is something to send. A nil channel
		// blocks forever in select, which is what we want otherwise.
		var out chan PlayerMessage
		var msg PlayerMessage
		if pending != nil {
			out = chans.send
			msg = *pending
		}

		select {
		case rm, ok := <-chans.read:
			if !ok {
				return
			}
			switch rm.msgType {
			case RoomGameStarted:
				b.Start(rm.config)
				started = true
				pending = &PlayerMessage{msgType: PlayerSendTurn, tankActions: b.Plan()}
			case RoomTurnResult:
				if !started {
					break
				}
				b.Observe(rm.turnResults)
				pending = &PlayerMessage{msgType: PlayerSendTurn, tankActions: b.Plan()}
			case RoomGameFinished:
				pending = nil
			}
		case out <- msg:
			pending = nil
		}
	}
}
