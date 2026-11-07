package bot

import (
	"fmt"
	"testing"
	"time"

	"tank-ops/internal/game"
)

var (
	east = vec{1, 0}
	west = vec{-1, 0}
)

// playBots plays a full game between two bots and returns player one's
// result. It fails the test if a bot ever submits an illegal action.
func playBots(t *testing.T, l1, l2 Level, seed uint64, p1First bool) game.GameResult {
	t.Helper()
	// rotate through the maps so every arena is exercised
	m := game.Maps[int(seed)%len(game.Maps)]
	gs := game.NewGameState(m.Build(p1First))
	cfg1, cfg2 := gs.ClientConfigs()
	b1, b2 := New(l1, seed), New(l2, seed+1000)
	b1.Start(cfg1)
	b2.Start(cfg2)

	for turn := 0; turn < 80; turn++ {
		a1, a2 := b1.Plan(), b2.Plan()
		checkActions(t, gs, true, a1)
		checkActions(t, gs, false, a2)
		r1, r2 := gs.ResolveActions(a1, a2)
		b1.Observe(r1)
		b2.Observe(r2)
		if res1, _, done := gs.Result(); done {
			return res1
		}
	}
	t.Fatalf("%v vs %v did not finish within 80 turns", l1, l2)
	return game.Draw
}

// checkActions fails the test if the orders break the rules the server would
// enforce: one order per living tank of our own, unit steps on open ground,
// within drive range, and a fire direction that is a single hex step.
func checkActions(t *testing.T, gs *game.GameState, player1 bool, actions []tankAction) {
	t.Helper()
	if len(actions) > 8 {
		t.Fatalf("too many actions: %d", len(actions))
	}
	seen := map[int]bool{}
	for _, a := range actions {
		pos, destroyed, ok := gs.Tank(player1, a.Id)
		if !ok || destroyed {
			t.Fatalf("action for unknown or destroyed tank %d", a.Id)
		}
		if seen[a.Id] {
			t.Fatalf("two actions for tank %d", a.Id)
		}
		seen[a.Id] = true
		switch a.Type {
		case game.TankFire:
			if !a.Dir.IsUnit() {
				t.Fatalf("fire direction %v is not a unit step", a.Dir)
			}
		case game.TankMove:
			if len(a.Path) < 2 || len(a.Path) > gs.DriveRange()+1 {
				t.Fatalf("bad path length %d", len(a.Path))
			}
			if a.Path[0] != pos {
				t.Fatalf("path starts at %v, tank is at %v", a.Path[0], pos)
			}
			for i := 1; i < len(a.Path); i++ {
				if !a.Path[i].Sub(a.Path[i-1]).IsUnit() {
					t.Fatalf("path %v has a non-adjacent step", a.Path)
				}
				if !gs.Traversable(a.Path[i]) {
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
	for _, l1 := range []Level{Easy, Medium, Hard} {
		for _, l2 := range []Level{Easy, Medium, Hard} {
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
func score(t *testing.T, a, b Level, n int) float64 {
	total := 0.0
	for i := 0; i < n; i++ {
		seed := uint64(i + 1)
		var res game.GameResult
		if i%2 == 0 {
			res = playBots(t, a, b, seed, i%4 == 0)
		} else {
			// swap seats; flip the result back to a's point of view
			switch playBots(t, b, a, seed, i%4 == 1) {
			case game.Win:
				res = game.Lose
			case game.Lose:
				res = game.Win
			default:
				res = game.Draw
			}
		}
		switch res {
		case game.Win:
			total++
		case game.Draw:
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
	medVsEasy := score(t, Medium, Easy, games)
	hardVsMed := score(t, Hard, Medium, games)
	hardVsEasy := score(t, Hard, Easy, games)
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
	cfg := clientConfig{
		PlayerTanks:     []tankConfig{{1, vec{0, 0}}},
		EnemyTanks:      []tankConfig{{2, vec{4, 0}}},
		Hexes:           []sceneConfig{{P: vec{0, 0}}, {P: vec{4, 0}}, {P: vec{3, 0}}},
		VisibilityRange: 2,
		DriveRange:      5,
		FireRange:       3,
		Center:          vec{0, 0},
	}
	v := newBotView(cfg)
	if v.enemy[2].visible {
		t.Fatal("enemy 4 hexes away should start hidden")
	}
	v.apply([]turnResult{game.NewTurnResultVisible(2, vec{2, 0}, true)})
	if e := v.enemy[2]; !e.visible || e.p != (vec{2, 0}) || e.age != 0 {
		t.Fatalf("unexpected track %+v", e)
	}
	v.apply([]turnResult{game.NewTurnResultVisible(2, vec{2, 0}, false)})
	if e := v.enemy[2]; e.visible {
		t.Fatalf("enemy should be hidden again: %+v", e)
	}
	v.apply(nil)
	if v.enemy[2].age != 1 {
		t.Fatalf("age should count turns since last seen, got %d", v.enemy[2].age)
	}
	v.apply([]turnResult{game.NewTurnResultDestroyingExplosion(vec{2, 0}, 2)})
	if !v.enemy[2].destroyed {
		t.Fatal("enemy should be marked destroyed")
	}
}

func TestBotViewShrink(t *testing.T) {
	cfg := clientConfig{
		PlayerTanks: []tankConfig{{1, vec{0, 0}}},
		Hexes:       []sceneConfig{{P: vec{0, 0}}, {P: vec{1, 0}}, {P: vec{2, 0}}},
		Center:      vec{0, 0},
	}
	v := newBotView(cfg)
	v.apply([]turnResult{game.NewTurnResultShrink(2, false)})
	if v.danger != 2 {
		t.Fatalf("danger = %d, want 2", v.danger)
	}
	v.apply([]turnResult{game.NewTurnResultShrink(2, true)})
	if v.hexes[vec{2, 0}] || !v.hexes[vec{1, 0}] {
		t.Fatal("outer hex should be gone and inner hex kept")
	}
	if v.danger != 0 {
		t.Fatal("danger resets once the shrink has happened")
	}
}

func arenaConfig(radius int, own, enemy vec) clientConfig {
	cfg := clientConfig{
		PlayerTanks:     []tankConfig{{1, own}},
		EnemyTanks:      []tankConfig{{2, enemy}},
		DriveRange:      5,
		FireRange:       3,
		VisibilityRange: 2,
		Center:          vec{0, 0},
	}
	for x := -radius; x <= radius; x++ {
		for y := -radius; y <= radius; y++ {
			if p := (vec{x, y}); p.Distance(vec{}) <= radius {
				cfg.Hexes = append(cfg.Hexes, sceneConfig{P: p})
			}
		}
	}
	return cfg
}

func TestBotLeavesDangerZone(t *testing.T) {
	cfg := arenaConfig(2, vec{2, 0}, vec{-2, 0})
	for _, level := range []Level{Easy, Medium, Hard} {
		b := New(level, 7)
		b.Start(cfg)
		b.view.danger = 2 // the ring the tank stands on is about to vanish
		actions := b.Plan()
		if len(actions) != 1 || actions[0].Type != game.TankMove {
			t.Fatalf("%v: expected a move, got %v", level, actions)
		}
		end := actions[0].Path[len(actions[0].Path)-1]
		if end.Distance(vec{}) >= 2 {
			t.Errorf("%v: tank ended at %v, still in the danger zone", level, end)
		}
	}
}

func TestBotFiresWhenAligned(t *testing.T) {
	cfg := arenaConfig(3, vec{0, 0}, vec{2, 0})
	for _, level := range []Level{Medium, Hard} {
		b := New(level, 3)
		b.Start(cfg)
		actions := b.Plan()
		if len(actions) != 1 || actions[0].Type != game.TankFire || actions[0].Dir != east {
			t.Errorf("%v: expected to fire east at the aligned enemy, got %v", level, actions)
		}
	}
}

func TestInferOrder(t *testing.T) {
	cfg := arenaConfig(3, game.Vector{0, 0}, game.Vector{2, 0})
	own := game.NewTurnResultFire(1, east)
	enemy := game.NewTurnResultFire(2, west)

	tests := []struct {
		name    string
		results []turnResult
		want    int
	}{
		{"we acted first", []turnResult{own, enemy}, 1},
		{"enemy acted first", []turnResult{enemy, own}, -1},
		{"enemy out of sight", []turnResult{own}, 0},
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
	cfg := arenaConfig(3, game.Vector{0, 0}, game.Vector{2, 0})
	v := newBotView(cfg)
	v.apply([]turnResult{game.NewTurnResultFire(1, east), game.NewTurnResultFire(2, west)})
	if v.weFirst != -1 {
		t.Fatalf("we went first on turn 1, so the enemy goes first on turn 2; got %d", v.weFirst)
	}
	v.apply(nil)
	if v.weFirst != 1 {
		t.Fatalf("order should flip every turn, got %d", v.weFirst)
	}
}
