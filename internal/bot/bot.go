// Package bot is the computer opponent. A Bot sees exactly what a human
// player sees (the clientConfig and filtered TurnResults) and returns the
// orders for its tanks each turn.
package bot

import (
	"math/rand/v2"

	"tank-ops/internal/game"
)

// Level is the computer opponent's difficulty.
type Level int

const (
	Easy   Level = 1
	Medium Level = 2
	Hard   Level = 3
)

func (l Level) String() string {
	switch l {
	case Easy:
		return "easy"
	case Medium:
		return "medium"
	case Hard:
		return "hard"
	}
	return "unknown"
}

// Bot plays one side of a game. It only ever sees what a human player sees:
// the clientConfig at the start and the filtered TurnResults afterwards, so
// fog of war applies to it exactly as it does to people.
type Bot struct {
	level Level
	rng   *rand.Rand
	view  *botView
}

func New(level Level, seed uint64) *Bot {
	return &Bot{level: level, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// Start tells the bot about a new game.
func (b *Bot) Start(cfg game.ClientConfig) {
	b.view = newBotView(cfg)
}

// Observe feeds the bot the results of a resolved turn.
func (b *Bot) Observe(results []game.TurnResult) {
	b.view.apply(results)
}

// Plan returns the bot's actions for the coming turn.
func (b *Bot) Plan() []game.TankAction {
	snap := b.view.snapshot()
	switch b.level {
	case Easy:
		return easyPlan(snap, b.rng)
	case Medium:
		return mediumPlan(snap, b.rng)
	default:
		return hardPlan(snap, b.view, b.rng)
	}
}
