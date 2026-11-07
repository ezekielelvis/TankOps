package bot

import (
	"math/rand/v2"
	"sort"

	"tank-ops/internal/game"
)

// Medium: scores every option for every tank.

type candidate struct {
	action tankAction
	score  float64
}

// positionScore says how good it is to end a turn with a tank at h, given
// where the other tanks of our side are.
func (s *botSnap) positionScore(id int, h vec, rng *rand.Rand) float64 {
	score := 0.0
	if s.danger > 0 {
		d := h.Distance(s.center)
		switch {
		case d >= s.danger:
			score -= 500 // will be destroyed
		case d == s.danger-1:
			score -= 3 // next in line
		}
	}

	friendly := make(map[vec]bool)
	for oid, op := range s.mine {
		if oid != id {
			friendly[op] = true
			if op.Distance(h) <= 1 {
				score -= 5 // keep tanks spread out
			}
		}
	}

	// Next turn the resolution order flips. Whoever resolves first can shoot
	// before being shot, so lines of fire are worth more to that side.
	attack, risk := 30.0, 14.0
	switch -s.weFirst {
	case 1: // we resolve first next turn
		attack, risk = 40, 3
	case -1: // they do
		attack, risk = 12, 30
	}

	nearest := 1 << 30
	for _, e := range s.theirs {
		d := h.Distance(e)
		if d < nearest {
			nearest = d
		}
		for _, dir := range hexDirs {
			if s.shotHits(h, dir, e, friendly) {
				score += attack // we can shoot them next turn
			}
			if s.shotHits(e, dir, h, nil) {
				score -= risk // they can shoot us next turn
			}
		}
	}
	if nearest != 1<<30 && nearest > s.fireRange {
		score -= 2 * float64(nearest-s.fireRange)
	}
	if rng != nil {
		score += rng.Float64() * 2
	}
	return score
}

func (s *botSnap) candidates(id int, claimed map[vec]bool, rng *rand.Rand) []candidate {
	p := s.mine[id]
	var cands []candidate

	for _, d := range s.firingSolutions(id, p) {
		cands = append(cands, candidate{fireAction(id, d), 100})
	}

	reach := s.reachable(p, s.occupied(id, claimed))
	delete(reach, p)
	for _, h := range sortedVectors(reach) {
		cands = append(cands, candidate{moveAction(id, reach[h]), s.positionScore(id, h, rng)})
	}

	// holding position is an option too
	cands = append(cands, candidate{tankAction{}, s.positionScore(id, p, rng) - 1})

	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	return cands
}

func mediumPlan(s *botSnap, rng *rand.Rand) []tankAction {
	var actions []tankAction
	claimed := make(map[vec]bool)
	for _, id := range sortedIDs(s.mine) {
		cands := s.candidates(id, claimed, rng)
		best := cands[0]
		if best.action.Type == 0 {
			continue // hold
		}
		if best.action.Type == game.TankMove {
			claimed[best.action.Path[len(best.action.Path)-1]] = true
		}
		actions = append(actions, best.action)
	}
	return actions
}
