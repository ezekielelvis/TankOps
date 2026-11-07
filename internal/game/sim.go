package game

// This file is the narrow API other packages (the bot) use to simulate and
// inspect positions without reaching into GameState's internals.

// Position describes a board to simulate one or two turns on. The side the
// caller controls is "player one": its tanks are Mine.
type Position struct {
	Hexes  []Vector
	Sites  []Vector
	Mine   []TankConfig
	Theirs []TankConfig

	Center                          Vector
	DriveRange, FireRange, VisRange int
	// ShrinkRadius > 0 means the arena ring at that distance from the centre
	// (and beyond) collapses at the end of the first simulated turn.
	ShrinkRadius int
	// MineFirst says whether player one's orders resolve first on turn one.
	MineFirst bool
}

// NewSimulation builds a game state from a Position.
func NewSimulation(p Position) *GameState {
	hexes := make([]SceneConfig, 0, len(p.Hexes))
	for _, h := range p.Hexes {
		hexes = append(hexes, SceneConfig{P: h})
	}
	sites := make([]SceneConfig, 0, len(p.Sites))
	for _, s := range p.Sites {
		sites = append(sites, SceneConfig{P: s, Variant: 6})
	}
	cfg := GameConfig{
		tanksP1:         p.Mine,
		tanksP2:         p.Theirs,
		hexes:           hexes,
		sites:           sites,
		driveRange:      p.DriveRange,
		visibilityRange: p.VisRange,
		fireRange:       p.FireRange,
		p1First:         p.MineFirst,
		center:          p.Center,
		shrinkAfter:     1000, // effectively never
		shrinkInterval:  1000, // only the announced shrink, on the first turn
		radius:          p.ShrinkRadius,
	}
	if p.ShrinkRadius > 0 {
		cfg.shrinkAfter = 1
	}
	return NewGameState(cfg)
}

func (gs *GameState) tanks(player1 bool) map[int]*Tank {
	if player1 {
		return gs.tanksP1
	}
	return gs.tanksP2
}

// Alive returns the positions of a side's surviving tanks by id.
func (gs *GameState) Alive(player1 bool) map[int]Vector {
	out := make(map[int]Vector)
	for id, t := range gs.tanks(player1) {
		if !t.destroyed {
			out[id] = t.p
		}
	}
	return out
}

// Lost returns how many tanks a side has lost.
func (gs *GameState) Lost(player1 bool) int {
	n := 0
	for _, t := range gs.tanks(player1) {
		if t.destroyed {
			n++
		}
	}
	return n
}

// Tank looks up one tank.
func (gs *GameState) Tank(player1 bool, id int) (p Vector, destroyed, ok bool) {
	t, ok := gs.tanks(player1)[id]
	if !ok {
		return Vector{}, false, false
	}
	return t.p, t.destroyed, true
}

// Terrain returns the hexes still on the board and which of them are
// impassable obstacles.
func (gs *GameState) Terrain() (hexes, sites map[Vector]bool) {
	hexes, sites = make(map[Vector]bool), make(map[Vector]bool)
	for p, h := range gs.hexes {
		hexes[p] = true
		if !h.traversable {
			sites[p] = true
		}
	}
	return hexes, sites
}

// Traversable reports whether a tank may drive onto p (ignoring other tanks).
func (gs *GameState) Traversable(p Vector) bool {
	h, ok := gs.hexes[p]
	return ok && h.traversable
}

func (gs *GameState) DriveRange() int { return gs.cfg.driveRange }
func (gs *GameState) FireRange() int  { return gs.cfg.fireRange }
