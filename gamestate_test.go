package main

import "testing"

var (
	east = Vector{1, 0}
	west = Vector{-1, 0}
)

// testConfig builds a hex arena of the given radius around (0,0).
func testConfig(radius int, p1, p2 []TankConfig, sites ...Vector) GameConfig {
	center := Vector{0, 0}
	var hexes []SceneConfig
	for x := -radius; x <= radius; x++ {
		for y := -radius; y <= radius; y++ {
			p := Vector{x, y}
			if p.distance(center) <= radius {
				hexes = append(hexes, SceneConfig{P: p})
			}
		}
	}
	var siteCfgs []SceneConfig
	for _, s := range sites {
		siteCfgs = append(siteCfgs, SceneConfig{P: s, Variant: 6})
	}
	return GameConfig{
		tanksP1:         p1,
		tanksP2:         p2,
		hexes:           hexes,
		sites:           siteCfgs,
		driveRange:      5,
		visibilityRange: 2,
		fireRange:       3,
		p1First:         true,
		center:          center,
		shrinkAfter:     1000, // effectively never unless a test overrides it
		shrinkInterval:  3,
		radius:          radius + 1,
	}
}

func path(points ...Vector) []Vector { return points }

func move(id int, p ...Vector) TankAction {
	return TankAction{Type: TankMove, Id: id, Path: p}
}

func fire(id int, dir Vector) TankAction {
	return TankAction{Type: TankFire, Id: id, Dir: dir}
}

func hasResult[T TurnResult](results []TurnResult, match func(T) bool) bool {
	for _, r := range results {
		if tr, ok := r.(T); ok && match(tr) {
			return true
		}
	}
	return false
}

func TestValidPath(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{0, 0}}},
		[]TankConfig{{2, Vector{4, -4}}},
		Vector{2, 0},
	)
	gs := NewGameState(cfg)
	gs.curPlayer, gs.curEnemy = gs.tanksP1, gs.tanksP2
	tank := gs.tanksP1[1]

	tests := []struct {
		name string
		path []Vector
		want int
	}{
		{"single step", path(Vector{0, 0}, Vector{1, 0}), 2},
		{"three steps", path(Vector{0, 0}, Vector{0, 1}, Vector{0, 2}, Vector{0, 3}), 4},
		{"too short", path(Vector{0, 0}), 0},
		{"wrong start", path(Vector{1, 0}, Vector{2, 0}), 0},
		{"not adjacent", path(Vector{0, 0}, Vector{2, 0}), 0},
		{"stops before a site", path(Vector{0, 0}, Vector{1, 0}, Vector{2, 0}, Vector{3, 0}), 2},
		{"truncated at the map edge", path(Vector{0, 0}, Vector{-1, 0}, Vector{-2, 0}, Vector{-3, 0}, Vector{-4, 0}, Vector{-5, 0}), 5},
		{"revisiting a hex stops", path(Vector{0, 0}, Vector{1, 0}, Vector{0, 0}), 2},
		{"over drive range", path(
			Vector{0, 0}, Vector{0, 1}, Vector{0, 2}, Vector{0, 3}, Vector{0, 4}, Vector{-1, 4}, Vector{-2, 4}), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gs.validPath(tt.path, tank)
			if len(got) != tt.want {
				t.Fatalf("validPath len = %d, want %d (%v)", len(got), tt.want, got)
			}
		})
	}
}

func TestMoveBlockedByTank(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{0, 0}}},
		[]TankConfig{{2, Vector{2, 0}}},
	)
	gs := NewGameState(cfg)
	gs.ResolveActions([]TankAction{move(1, Vector{0, 0}, Vector{1, 0}, Vector{2, 0})}, nil)
	if got := gs.tanksP1[1].p; got != (Vector{1, 0}) {
		t.Fatalf("tank should stop in front of the enemy, got %v", got)
	}
}

func TestFireHitsTank(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{-2, 0}}},
		[]TankConfig{{2, Vector{0, 0}}},
	)
	gs := NewGameState(cfg)
	r1, r2 := gs.ResolveActions([]TankAction{fire(1, east)}, nil)
	if !gs.tanksP2[2].destroyed {
		t.Fatal("enemy tank should be destroyed")
	}
	isKill := func(r TurnResultExplosion) bool { return r.Destroyed && r.Id == 2 }
	if !hasResult(r1, isKill) || !hasResult(r2, isKill) {
		t.Fatal("both players should see the destroying explosion")
	}
}

func TestFireOutOfRangeMisses(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{-4, 0}}},
		[]TankConfig{{2, Vector{1, 0}}},
	)
	gs := NewGameState(cfg)
	gs.ResolveActions([]TankAction{fire(1, east)}, nil)
	if gs.tanksP2[2].destroyed {
		t.Fatal("target is 5 hexes away, fireRange is 3")
	}
}

func TestSiteBlocksShot(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{-3, 0}}},
		[]TankConfig{{2, Vector{0, 0}}},
		Vector{-2, 0},
	)
	gs := NewGameState(cfg)
	gs.ResolveActions([]TankAction{fire(1, east)}, nil)
	if gs.tanksP2[2].destroyed {
		t.Fatal("site should absorb the shot")
	}
}

func TestInvalidFireDirectionIgnored(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{-1, 0}}},
		[]TankConfig{{2, Vector{0, 0}}},
	)
	gs := NewGameState(cfg)
	gs.ResolveActions([]TankAction{fire(1, Vector{2, 0})}, nil)
	if gs.tanksP2[2].destroyed {
		t.Fatal("non-unit direction must be ignored")
	}
}

func TestTankActsOncePerTurn(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{0, 0}}},
		[]TankConfig{{2, Vector{-4, 0}}},
	)
	gs := NewGameState(cfg)
	gs.ResolveActions([]TankAction{
		move(1, Vector{0, 0}, Vector{1, 0}),
		move(1, Vector{1, 0}, Vector{2, 0}),
	}, nil)
	if got := gs.tanksP1[1].p; got != (Vector{1, 0}) {
		t.Fatalf("second action for the same tank must be ignored, tank at %v", got)
	}
}

func TestUnknownTankIgnored(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{0, 0}}},
		[]TankConfig{{2, Vector{-4, 0}}},
	)
	gs := NewGameState(cfg)
	// P1 tries to control P2's tank.
	gs.ResolveActions([]TankAction{move(2, Vector{-4, 0}, Vector{-3, 0})}, nil)
	if got := gs.tanksP2[2].p; got != (Vector{-4, 0}) {
		t.Fatalf("player controlled an enemy tank, now at %v", got)
	}
}

func TestVisibilityOnApproach(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{-4, 0}}},
		[]TankConfig{{2, Vector{2, 0}}},
	)
	gs := NewGameState(cfg)
	if gs.tanksP1[1].visible || gs.tanksP2[2].visible {
		t.Fatal("tanks start far apart and must be hidden")
	}
	r1, r2 := gs.ResolveActions([]TankAction{
		move(1, Vector{-4, 0}, Vector{-3, 0}, Vector{-2, 0}, Vector{-1, 0}, Vector{0, 0}),
	}, nil)
	seen := func(r TurnResultVisible) bool { return r.Id == 2 && r.Visible }
	if !hasResult(r1, seen) {
		t.Error("P1 should see the enemy tank appear")
	}
	seen1 := func(r TurnResultVisible) bool { return r.Id == 1 && r.Visible }
	if !hasResult(r2, seen1) {
		t.Error("P2 should see the approaching tank appear")
	}
}

func TestEnemyMovesHiddenWhenNotVisible(t *testing.T) {
	cfg := testConfig(4,
		[]TankConfig{{1, Vector{-4, 0}}},
		[]TankConfig{{2, Vector{4, 0}}},
	)
	gs := NewGameState(cfg)
	_, r2 := gs.ResolveActions([]TankAction{move(1, Vector{-4, 0}, Vector{-3, 0})}, nil)
	if hasResult(r2, func(r TurnResultMove2) bool { return r.Id == 1 }) {
		t.Fatal("hidden tank movement must not be sent to the enemy")
	}
}

func TestSimultaneousFireOrder(t *testing.T) {
	newGame := func() *GameState {
		return NewGameState(testConfig(4,
			[]TankConfig{{1, Vector{-1, 0}}},
			[]TankConfig{{2, Vector{1, 0}}},
		))
	}
	p1 := []TankAction{fire(1, east)}
	p2 := []TankAction{fire(2, west)}

	// With p1First the first player resolves first and kills the other
	// before it can shoot.
	gs := newGame()
	gs.ResolveActions(p1, p2)
	if gs.tanksP1[1].destroyed || !gs.tanksP2[2].destroyed {
		t.Fatal("turn 1: P1 should win the duel")
	}

	// The next turn the order flips.
	gs = newGame()
	gs.ResolveActions(nil, nil)
	gs.ResolveActions(p1, p2)
	if !gs.tanksP1[1].destroyed || gs.tanksP2[2].destroyed {
		t.Fatal("turn 2: P2 should win the duel")
	}
}

func TestShrinkDestroysTanksOnTheEdge(t *testing.T) {
	cfg := testConfig(2,
		[]TankConfig{{1, Vector{2, 0}}}, // on the outer ring
		[]TankConfig{{2, Vector{0, 0}}}, // in the middle
	)
	cfg.shrinkAfter = 1
	cfg.shrinkInterval = 1
	cfg.radius = 2
	gs := NewGameState(cfg)
	gs.ResolveActions(nil, nil)
	if !gs.tanksP1[1].destroyed {
		t.Fatal("tank on the shrinking ring should be destroyed")
	}
	if gs.tanksP2[2].destroyed {
		t.Fatal("tank in the centre should survive")
	}
	if _, ok := gs.hexes[Vector{2, 0}]; ok {
		t.Fatal("outer hex should be removed")
	}
	if _, ok := gs.hexes[Vector{0, 0}]; !ok {
		t.Fatal("centre hex should remain")
	}
}

func TestResult(t *testing.T) {
	cfg := testConfig(3,
		[]TankConfig{{1, Vector{-1, 0}}},
		[]TankConfig{{2, Vector{1, 0}}},
	)
	tests := []struct {
		name       string
		kill1      bool
		kill2      bool
		r1, r2     GameResult
		wantFinish bool
	}{
		{"ongoing", false, false, Draw, Draw, false},
		{"p1 wins", false, true, Win, Lose, true},
		{"p2 wins", true, false, Lose, Win, true},
		{"mutual destruction", true, true, Draw, Draw, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := NewGameState(cfg)
			gs.tanksP1[1].destroyed = tt.kill1
			gs.tanksP2[2].destroyed = tt.kill2
			r1, r2, done := gs.Result()
			if r1 != tt.r1 || r2 != tt.r2 || done != tt.wantFinish {
				t.Fatalf("got (%v,%v,%v) want (%v,%v,%v)", r1, r2, done, tt.r1, tt.r2, tt.wantFinish)
			}
		})
	}
}

func TestResolveIsDeterministic(t *testing.T) {
	run := func() []TurnResult {
		cfg := NewBasicConfig(false, true)
		gs := NewGameState(cfg)
		var last []TurnResult
		for i := 0; i < 6; i++ {
			last, _ = gs.ResolveActions(nil, nil)
		}
		return last
	}
	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("different result counts: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("result %d differs: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestBasicConfigGameTerminates(t *testing.T) {
	gs := NewGameState(NewBasicConfig(false, true))
	for i := 0; i < 100; i++ {
		gs.ResolveActions(nil, nil)
		if _, _, done := gs.Result(); done {
			return
		}
	}
	t.Fatal("shrinking arena should end an idle game")
}

func TestMapsAreWellFormed(t *testing.T) {
	for _, m := range Maps {
		t.Run(m.Name, func(t *testing.T) {
			cfg := m.Build(true)
			hexes := map[Vector]bool{}
			for _, h := range cfg.hexes {
				hexes[h.P] = true
			}
			sites := map[Vector]bool{}
			for _, s := range cfg.sites {
				if !hexes[s.P] {
					t.Errorf("site %v is off the map", s.P)
				}
				sites[s.P] = true
			}
			// the original outpost map is deliberately lopsided; all others
			// must be point-symmetric so neither side is favoured
			symmetric := m.Name != "outpost"
			if symmetric {
				for p := range sites {
					if !sites[negate(p)] {
						t.Errorf("site %v has no mirror image", p)
					}
				}
			}
			if len(cfg.tanksP1) != len(cfg.tanksP2) || len(cfg.tanksP1) == 0 {
				t.Fatal("players need the same, non-zero number of tanks")
			}
			ids := map[int]bool{}
			for i := range cfg.tanksP1 {
				a, b := cfg.tanksP1[i], cfg.tanksP2[i]
				for _, tk := range []TankConfig{a, b} {
					if ids[tk.Id] {
						t.Errorf("duplicate tank id %d", tk.Id)
					}
					ids[tk.Id] = true
					if !hexes[tk.P] || sites[tk.P] {
						t.Errorf("tank %d starts on %v which is not open ground", tk.Id, tk.P)
					}
				}
				if symmetric && b.P != negate(a.P) {
					t.Errorf("tank %d is not mirrored by tank %d", a.Id, b.Id)
				}
			}
			gs := NewGameState(cfg)
			for i := 0; i < 100; i++ {
				gs.ResolveActions(nil, nil)
				if _, _, done := gs.Result(); done {
					return
				}
			}
			t.Fatal("idle game never ends")
		})
	}
}
