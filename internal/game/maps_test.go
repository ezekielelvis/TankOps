package game

import "testing"

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
