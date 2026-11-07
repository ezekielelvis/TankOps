package bot

import (
	"math/rand/v2"
	"sort"
	"time"

	"tank-ops/internal/game"
)

// Hard: tries the best few options per tank against many guesses of what the
// enemy is doing, using the real game rules to resolve each guess.

const (
	hardTopPerTank = 5
	hardSamples    = 32
	hardMaxPlans   = 40
	hardTimeBudget = 400 * time.Millisecond
)

func hardPlan(s *botSnap, v *botView, rng *rand.Rand) []tankAction {
	ids := sortedIDs(s.mine)
	if len(ids) == 0 {
		return nil
	}

	// shortlist of options per tank
	claimed := make(map[vec]bool)
	options := make([][]tankAction, len(ids))
	for i, id := range ids {
		cands := s.candidates(id, claimed, rng)
		if len(cands) > hardTopPerTank {
			cands = cands[:hardTopPerTank]
		}
		for _, c := range cands {
			options[i] = append(options[i], c.action)
		}
	}

	plans := joinPlans(options, rng, hardMaxPlans)
	if len(plans) == 1 {
		return cleanPlan(plans[0])
	}

	// draw the guesses once so every plan faces the same opposition
	guesses := make([]*enemyGuess, hardSamples)
	for i := range guesses {
		guesses[i] = sampleEnemy(s, v, rng)
	}

	deadline := time.Now().Add(hardTimeBudget)
	bestScore, bestPlan := -1e18, plans[0]
	for _, plan := range plans {
		total := 0.0
		for _, g := range guesses {
			total += rollout(s, plan, g)
		}
		if total > bestScore {
			bestScore, bestPlan = total, plan
		}
		if time.Now().After(deadline) {
			break
		}
	}
	return cleanPlan(bestPlan)
}

func cleanPlan(plan []tankAction) []tankAction {
	out := make([]tankAction, 0, len(plan))
	for _, a := range plan {
		if a.Type != 0 {
			out = append(out, a)
		}
	}
	return out
}

// joinPlans builds joint plans from per-tank options, dropping combinations
// where two tanks end on the same hex. When there are too many it keeps a
// random subset, always including the all-best plan.
func joinPlans(options [][]tankAction, rng *rand.Rand, limit int) [][]tankAction {
	plans := [][]tankAction{{}}
	for _, opts := range options {
		var next [][]tankAction
		for _, plan := range plans {
			for _, o := range opts {
				if conflicts(plan, o) {
					continue
				}
				np := make([]tankAction, len(plan)+1)
				copy(np, plan)
				np[len(plan)] = o
				next = append(next, np)
			}
		}
		if len(next) == 0 {
			next = plans
		}
		plans = next
	}
	if len(plans) > limit {
		first := plans[0]
		rng.Shuffle(len(plans), func(i, j int) { plans[i], plans[j] = plans[j], plans[i] })
		plans = append([][]tankAction{first}, plans[:limit-1]...)
	}
	return plans
}

func conflicts(plan []tankAction, a tankAction) bool {
	if a.Type != game.TankMove {
		return false
	}
	end := a.Path[len(a.Path)-1]
	for _, o := range plan {
		if o.Type == game.TankMove && o.Path[len(o.Path)-1] == end {
			return true
		}
	}
	return false
}

type enemyGuess struct {
	positions map[int]vec
	actions   []tankAction
	p1First   bool // whether we (as player one in the rollout) resolve first
}

// sampleEnemy imagines where the enemy tanks are and what they will do.
func sampleEnemy(s *botSnap, v *botView, rng *rand.Rand) *enemyGuess {
	g := &enemyGuess{positions: make(map[int]vec), p1First: rng.IntN(2) == 0}
	if s.weFirst != 0 {
		g.p1First = s.weFirst > 0
	}

	taken := make(map[vec]bool)
	for _, p := range s.mine {
		taken[p] = true
	}
	ids := make([]int, 0, len(v.enemy))
	for id, e := range v.enemy {
		if !e.destroyed {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)

	for _, id := range ids {
		e := v.enemy[id]
		pos := e.p
		if !e.visible && e.age > 0 {
			// the tank could have driven up to driveRange hexes per turn
			sub := *s
			sub.driveRange = min(s.driveRange*e.age, 12)
			reach := sub.reachable(e.p, taken)
			var spots []vec
			for h := range reach {
				if !s.inDanger(h) {
					spots = append(spots, h)
				}
			}
			if len(spots) > 0 {
				sort.Slice(spots, func(i, j int) bool {
					if spots[i].X != spots[j].X {
						return spots[i].X < spots[j].X
					}
					return spots[i].Y < spots[j].Y
				})
				pos = spots[rng.IntN(len(spots))]
			}
		}
		taken[pos] = true
		g.positions[id] = pos
	}

	// What would the enemy do? Mirror our own policies from their side.
	es := &botSnap{
		hexes:      s.hexes,
		sites:      s.sites,
		mine:       g.positions,
		theirs:     s.mine,
		center:     s.center,
		danger:     s.danger,
		weFirst:    -s.weFirst,
		driveRange: s.driveRange,
		fireRange:  s.fireRange,
		visRange:   s.visRange,
	}
	if rng.IntN(2) == 0 {
		g.actions = easyPlan(es, rng)
	} else {
		g.actions = mediumPlan(es, rng)
	}
	return g
}

// rollout plays one imagined turn with the real rules and scores the result
// for us (higher is better).
func rollout(s *botSnap, plan []tankAction, g *enemyGuess) float64 {
	pos := game.Position{
		Center:       s.center,
		DriveRange:   s.driveRange,
		FireRange:    s.fireRange,
		VisRange:     s.visRange,
		ShrinkRadius: s.danger,
		MineFirst:    g.p1First,
	}
	for h := range s.hexes {
		pos.Hexes = append(pos.Hexes, h)
	}
	for p := range s.sites {
		pos.Sites = append(pos.Sites, p)
	}
	for _, id := range sortedIDs(s.mine) {
		pos.Mine = append(pos.Mine, tankConfig{Id: id, P: s.mine[id]})
	}
	for _, id := range sortedIDs(g.positions) {
		pos.Theirs = append(pos.Theirs, tankConfig{Id: id, P: g.positions[id]})
	}

	gs := game.NewSimulation(pos)
	gs.ResolveActions(plan, g.actions)

	score := tally(gs)

	// Second ply: both sides shoot at anything lined up. The resolution order
	// flips, so this punishes ending a turn in the enemy's sights when they
	// will get to fire first.
	if _, _, over := gs.Result(); !over {
		before := score
		a1, a2 := shootIfAligned(gs, true), shootIfAligned(gs, false)
		gs.ResolveActions(a1, a2)
		score = before + 0.9*(tally(gs)-before)
	}

	end := &botSnap{
		hexes: s.hexes, sites: s.sites, center: s.center, danger: 0,
		mine:       gs.Alive(true),
		theirs:     gs.Alive(false),
		weFirst:    s.weFirst, // positionScore flips it to get next turn's order
		driveRange: s.driveRange, fireRange: s.fireRange, visRange: s.visRange,
	}
	for id, p := range end.mine {
		score += end.positionScore(id, p, nil)
	}
	return score
}

// tally scores the material balance: tanks destroyed minus tanks lost.
func tally(gs *game.GameState) float64 {
	return 1000 * float64(gs.Lost(false)-gs.Lost(true))
}

// shootIfAligned has every living tank on one side fire if it has a shot.
func shootIfAligned(gs *game.GameState, first bool) []tankAction {
	hexes, sites := gs.Terrain()
	snap := &botSnap{
		hexes:     hexes,
		sites:     sites,
		mine:      gs.Alive(first),
		theirs:    gs.Alive(!first),
		fireRange: gs.FireRange(),
	}
	var actions []tankAction
	for _, id := range sortedIDs(snap.mine) {
		if dirs := snap.firingSolutions(id, snap.mine[id]); len(dirs) > 0 {
			actions = append(actions, fireAction(id, dirs[0]))
		}
	}
	return actions
}
