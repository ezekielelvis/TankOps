package bot

import (
	"sort"

	"tank-ops/internal/game"
)

// Geometry helpers

var hexDirs = []vec{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, -1}, {-1, 1}}

func sortedIDs(m map[int]vec) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func (s *botSnap) occupied(skipID int, extra map[vec]bool) map[vec]bool {
	occ := make(map[vec]bool)
	for id, p := range s.mine {
		if id != skipID {
			occ[p] = true
		}
	}
	for _, p := range s.theirs {
		occ[p] = true
	}
	for p := range extra {
		occ[p] = true
	}
	return occ
}

// reachable returns, for every hex a tank can drive to this turn, a shortest
// path to it (starting with the tank's own hex).
func (s *botSnap) reachable(from vec, blocked map[vec]bool) map[vec][]vec {
	paths := map[vec][]vec{from: {from}}
	frontier := []vec{from}
	for step := 0; step < s.driveRange; step++ {
		var next []vec
		for _, cur := range frontier {
			for _, d := range hexDirs {
				n := cur.Add(d)
				if _, seen := paths[n]; seen {
					continue
				}
				if !s.hexes[n] || s.sites[n] || blocked[n] {
					continue
				}
				p := make([]vec, len(paths[cur])+1)
				copy(p, paths[cur])
				p[len(p)-1] = n
				paths[n] = p
				next = append(next, n)
			}
		}
		frontier = next
	}
	return paths
}

// shotHits reports whether a shell fired from `from` along dir reaches
// `target` before hitting anything else.
func (s *botSnap) shotHits(from, dir, target vec, friendly map[vec]bool) bool {
	cur := from
	for i := 0; i < s.fireRange; i++ {
		cur = cur.Add(dir)
		if s.sites[cur] {
			return false
		}
		if cur == target {
			return true
		}
		if friendly[cur] {
			return false
		}
	}
	return false
}

// firingSolutions lists directions in which a tank at p would hit a known
// enemy right now.
func (s *botSnap) firingSolutions(id int, p vec) []vec {
	friendly := make(map[vec]bool)
	for oid, op := range s.mine {
		if oid != id {
			friendly[op] = true
		}
	}
	var dirs []vec
	for _, d := range hexDirs {
		for _, e := range s.theirs {
			if s.shotHits(p, d, e, friendly) {
				dirs = append(dirs, d)
				break
			}
		}
	}
	return dirs
}

func (s *botSnap) nearestEnemy(p vec) (vec, int, bool) {
	best, bestD, ok := vec{}, 1<<30, false
	for _, id := range sortedIDs(s.theirs) {
		e := s.theirs[id]
		if d := p.Distance(e); d < bestD {
			best, bestD, ok = e, d, true
		}
	}
	return best, bestD, ok
}

func (s *botSnap) inDanger(p vec) bool {
	return s.danger > 0 && p.Distance(s.center) >= s.danger
}

func moveAction(id int, path []vec) tankAction {
	return tankAction{Type: game.TankMove, Id: id, Path: path}
}

func fireAction(id int, dir vec) tankAction {
	return tankAction{Type: game.TankFire, Id: id, Dir: dir}
}
