package main

import (
	"fmt"
	"testing"
	"time"
)

// playBots plays a full game between two bots and returns player one's
// result. It fails the test if a bot ever submits an illegal action.
func playBots(t *testing.T, l1, l2 BotLevel, seed uint64, p1First bool) GameResult {
	t.Helper()
	// rotate through the maps so every arena is exercised
	m := Maps[int(seed)%len(Maps)]
	gs := NewGameState(m.Build(p1First))
	cfg1, cfg2 := gs.ClientConfigs()
	b1, b2 := NewBot(l1, seed), NewBot(l2, seed+1000)
	b1.Start(cfg1)
	b2.Start(cfg2)

	for turn := 0; turn < 80; turn++ {
		a1, a2 := b1.Plan(), b2.Plan()
		checkActions(t, gs, gs.tanksP1, a1)
		checkActions(t, gs, gs.tanksP2, a2)
		r1, r2 := gs.ResolveActions(a1, a2)
		b1.Observe(r1)
		b2.Observe(r2)
		if res1, _, done := gs.Result(); done {
			return res1
		}
	}
	t.Fatalf("%v vs %v did not finish within 80 turns", l1, l2)
	return Draw
}

func checkActions(t *testing.T, gs *GameState, tanks map[int]*Tank, actions []TankAction) {
	t.Helper()
	if err := (ClientMessage{Type: ClientSendTurn, Actions: actions}).validate(); err != nil {
		t.Fatalf("bot output fails protocol validation: %v", err)
	}
	seen := map[int]bool{}
	for _, a := range actions {
		tank, ok := tanks[a.Id]
		if !ok || tank.destroyed {
			t.Fatalf("action for unknown or destroyed tank %d", a.Id)
		}
		if seen[a.Id] {
			t.Fatalf("two actions for tank %d", a.Id)
		}
		seen[a.Id] = true
		switch a.Type {
		case TankFire:
			if !a.Dir.isUnit() {
				t.Fatalf("fire direction %v is not a unit step", a.Dir)
			}
		case TankMove:
			if len(a.Path) < 2 || len(a.Path) > gs.cfg.driveRange+1 {
				t.Fatalf("bad path length %d", len(a.Path))
			}
			if a.Path[0] != tank.p {
				t.Fatalf("path starts at %v, tank is at %v", a.Path[0], tank.p)
			}
			for i := 1; i < len(a.Path); i++ {
				if !a.Path[i].sub(a.Path[i-1]).isUnit() {
					t.Fatalf("path %v has a non-adjacent step", a.Path)
				}
				if h, ok := gs.hexes[a.Path[i]]; !ok || !h.traversable {
					t.Fatalf("path %v leaves the traversable map", a.Path)
				}
			}
		default:
			t.Fatalf("unknown action type %d", a.Type)
		}
	}
}

func TestBotsPlayLegalGames(t *testing.T) {
	if testing.Short() {
		t.Skip("self-play is slow")
	}
	for _, l1 := range []BotLevel{BotEasy, BotMedium, BotHard} {
		for _, l2 := range []BotLevel{BotEasy, BotMedium, BotHard} {
			t.Run(fmt.Sprintf("%v-vs-%v", l1, l2), func(t *testing.T) {
				for seed := uint64(1); seed <= 3; seed++ {
					playBots(t, l1, l2, seed, seed%2 == 0)
				}
			})
		}
	}
}

// score returns how well `a` does against `b` over n games (win = 1,
// draw = 0.5), alternating sides and who resolves first.
func score(t *testing.T, a, b BotLevel, n int) float64 {
	total := 0.0
	for i := 0; i < n; i++ {
		seed := uint64(i + 1)
		var res GameResult
		if i%2 == 0 {
			res = playBots(t, a, b, seed, i%4 == 0)
		} else {
			// swap seats; flip the result back to a's point of view
			switch playBots(t, b, a, seed, i%4 == 1) {
			case Win:
				res = Lose
			case Lose:
				res = Win
			default:
				res = Draw
			}
		}
		switch res {
		case Win:
			total++
		case Draw:
			total += 0.5
		}
	}
	return total / float64(n)
}

func TestBotStrengthOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("self-play is slow")
	}
	games := 60
	start := time.Now()
	medVsEasy := score(t, BotMedium, BotEasy, games)
	hardVsMed := score(t, BotHard, BotMedium, games)
	hardVsEasy := score(t, BotHard, BotEasy, games)
	t.Logf("medium vs easy: %.2f, hard vs medium: %.2f, hard vs easy: %.2f (%v)",
		medVsEasy, hardVsMed, hardVsEasy, time.Since(start).Round(time.Millisecond))

	if medVsEasy < 0.6 {
		t.Errorf("medium should clearly beat easy, scored %.2f", medVsEasy)
	}
	if hardVsMed < 0.55 {
		t.Errorf("hard should beat medium, scored %.2f", hardVsMed)
	}
	if hardVsEasy < 0.6 {
		t.Errorf("hard should clearly beat easy, scored %.2f", hardVsEasy)
	}
}

func TestBotViewTracksVisibility(t *testing.T) {
	cfg := ClientConfig{
		PlayerTanks:     []TankConfig{{1, Vector{0, 0}}},
		EnemyTanks:      []TankConfig{{2, Vector{4, 0}}},
		Hexes:           []SceneConfig{{P: Vector{0, 0}}, {P: Vector{4, 0}}, {P: Vector{3, 0}}},
		VisibilityRange: 2,
		DriveRange:      5,
		FireRange:       3,
		Center:          Vector{0, 0},
	}
	v := newBotView(cfg)
	if v.enemy[2].visible {
		t.Fatal("enemy 4 hexes away should start hidden")
	}
	v.apply([]TurnResult{newTurnResultVisible(2, Vector{2, 0}, true)})
	if e := v.enemy[2]; !e.visible || e.p != (Vector{2, 0}) || e.age != 0 {
		t.Fatalf("unexpected track %+v", e)
	}
	v.apply([]TurnResult{newTurnResultVisible(2, Vector{2, 0}, false)})
	if e := v.enemy[2]; e.visible {
		t.Fatalf("enemy should be hidden again: %+v", e)
	}
	v.apply(nil)
	if v.enemy[2].age != 1 {
		t.Fatalf("age should count turns since last seen, got %d", v.enemy[2].age)
	}
	v.apply([]TurnResult{newTurnResultDestroyingExplosion(Vector{2, 0}, 2)})
	if !v.enemy[2].destroyed {
		t.Fatal("enemy should be marked destroyed")
	}
}

func TestBotViewShrink(t *testing.T) {
	cfg := ClientConfig{
		PlayerTanks: []TankConfig{{1, Vector{0, 0}}},
		Hexes:       []SceneConfig{{P: Vector{0, 0}}, {P: Vector{1, 0}}, {P: Vector{2, 0}}},
		Center:      Vector{0, 0},
	}
	v := newBotView(cfg)
	v.apply([]TurnResult{newTurnResultShrink(2, false)})
	if v.danger != 2 {
		t.Fatalf("danger = %d, want 2", v.danger)
	}
	v.apply([]TurnResult{newTurnResultShrink(2, true)})
	if v.hexes[Vector{2, 0}] || !v.hexes[Vector{1, 0}] {
		t.Fatal("outer hex should be gone and inner hex kept")
	}
	if v.danger != 0 {
		t.Fatal("danger resets once the shrink has happened")
	}
}

func arenaConfig(radius int, own, enemy Vector) ClientConfig {
	cfg := ClientConfig{
		PlayerTanks:     []TankConfig{{1, own}},
		EnemyTanks:      []TankConfig{{2, enemy}},
		DriveRange:      5,
		FireRange:       3,
		VisibilityRange: 2,
		Center:          Vector{0, 0},
	}
	for x := -radius; x <= radius; x++ {
		for y := -radius; y <= radius; y++ {
			if p := (Vector{x, y}); p.distance(Vector{}) <= radius {
				cfg.Hexes = append(cfg.Hexes, SceneConfig{P: p})
			}
		}
	}
	return cfg
}

func TestBotLeavesDangerZone(t *testing.T) {
	cfg := arenaConfig(2, Vector{2, 0}, Vector{-2, 0})
	for _, level := range []BotLevel{BotEasy, BotMedium, BotHard} {
		b := NewBot(level, 7)
		b.Start(cfg)
		b.view.danger = 2 // the ring the tank stands on is about to vanish
		actions := b.Plan()
		if len(actions) != 1 || actions[0].Type != TankMove {
			t.Fatalf("%v: expected a move, got %v", level, actions)
		}
		end := actions[0].Path[len(actions[0].Path)-1]
		if end.distance(Vector{}) >= 2 {
			t.Errorf("%v: tank ended at %v, still in the danger zone", level, end)
		}
	}
}

func TestBotFiresWhenAligned(t *testing.T) {
	cfg := arenaConfig(3, Vector{0, 0}, Vector{2, 0})
	for _, level := range []BotLevel{BotMedium, BotHard} {
		b := NewBot(level, 3)
		b.Start(cfg)
		actions := b.Plan()
		if len(actions) != 1 || actions[0].Type != TankFire || actions[0].Dir != east {
			t.Errorf("%v: expected to fire east at the aligned enemy, got %v", level, actions)
		}
	}
}

func TestMessageValidation(t *testing.T) {
	good := []ClientMessage{
		{Type: ClientJoinRoom, RoomCode: "ABC123"},
		{Type: ClientJoinRoom, VsBot: true, Difficulty: 2},
		{Type: ClientQuitRoom},
		{Type: ClientSendTurn, Actions: []TankAction{{Type: TankMove, Id: 1, Path: []Vector{{0, 0}, {1, 0}}}}},
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
		{Type: ClientSendTurn, Actions: make([]TankAction, 20)},
		{Type: ClientSendTurn, Actions: []TankAction{{Type: TankMove, Path: make([]Vector, 100)}}},
		{Type: ClientSendTurn, Actions: []TankAction{{Type: 42}}},
	}
	for _, m := range bad {
		if err := m.validate(); err == nil {
			t.Errorf("%+v should be rejected", m)
		}
	}
}

func TestInferOrder(t *testing.T) {
	cfg := arenaConfig(3, Vector{0, 0}, Vector{2, 0})
	own := newTurnResultFire(1, east)
	enemy := newTurnResultFire(2, west)

	tests := []struct {
		name    string
		results []TurnResult
		want    int
	}{
		{"we acted first", []TurnResult{own, enemy}, 1},
		{"enemy acted first", []TurnResult{enemy, own}, -1},
		{"enemy out of sight", []TurnResult{own}, 0},
		{"nobody acted", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newBotView(cfg)
			if got := v.inferOrder(tt.results); got != tt.want {
				t.Fatalf("inferOrder = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestOrderAlternatesAfterInference(t *testing.T) {
	cfg := arenaConfig(3, Vector{0, 0}, Vector{2, 0})
	v := newBotView(cfg)
	v.apply([]TurnResult{newTurnResultFire(1, east), newTurnResultFire(2, west)})
	if v.weFirst != -1 {
		t.Fatalf("we went first on turn 1, so the enemy goes first on turn 2; got %d", v.weFirst)
	}
	v.apply(nil)
	if v.weFirst != 1 {
		t.Fatalf("order should flip every turn, got %d", v.weFirst)
	}
}
