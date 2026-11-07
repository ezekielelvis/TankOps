package bot

import (
	"math/rand/v2"
	"sort"
)

// Easy: advances on the enemy and shoots when it happens to line up.

func easyPlan(s *botSnap, rng *rand.Rand) []tankAction {
	var actions []tankAction
	claimed := make(map[vec]bool)

	for _, id := range sortedIDs(s.mine) {
		p := s.mine[id]

		if dirs := s.firingSolutions(id, p); len(dirs) > 0 && rng.Float64() < 0.7 {
			actions = append(actions, fireAction(id, dirs[rng.IntN(len(dirs))]))
			continue
		}

		reach := s.reachable(p, s.occupied(id, claimed))
		delete(reach, p)
		if len(reach) == 0 {
			continue
		}
		dests := sortedVectors(reach)

		var dest vec
		switch {
		case s.inDanger(p):
			dest = pickMin(dests, rng, func(h vec) int {
				pen := 0
				if s.inDanger(h) {
					pen = 100
				}
				return pen + h.Distance(s.center)
			})
		case rng.Float64() < 0.3:
			dest = dests[rng.IntN(len(dests))]
		default:
			target := s.center
			if e, _, ok := s.nearestEnemy(p); ok {
				target = e
			}
			dest = pickMin(dests, rng, func(h vec) int {
				pen := 0
				if s.inDanger(h) {
					pen = 100
				}
				return pen + h.Distance(target)
			})
		}
		claimed[dest] = true
		actions = append(actions, moveAction(id, reach[dest]))
	}
	return actions
}

func sortedVectors(m map[vec][]vec) []vec {
	vs := make([]vec, 0, len(m))
	for v := range m {
		vs = append(vs, v)
	}
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].X != vs[j].X {
			return vs[i].X < vs[j].X
		}
		return vs[i].Y < vs[j].Y
	})
	return vs
}

// pickMin returns a random element among those with the lowest cost.
func pickMin(items []vec, rng *rand.Rand, cost func(vec) int) vec {
	best := 1 << 30
	var winners []vec
	for _, it := range items {
		c := cost(it)
		switch {
		case c < best:
			best = c
			winners = []vec{it}
		case c == best:
			winners = append(winners, it)
		}
	}
	return winners[rng.IntN(len(winners))]
}
