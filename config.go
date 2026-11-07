package main

import "math/rand/v2"

type TankConfig struct {
	Id int    `json:"id"`
	P  Vector `json:"p"`
}

type SceneConfig struct {
	P       Vector `json:"p"`
	Variant int    `json:"variant"`
}

type ClientConfig struct {
	PlayerTanks     []TankConfig  `json:"playerTanks"`
	EnemyTanks      []TankConfig  `json:"enemyTanks"`
	Hexes           []SceneConfig `json:"hexes"`
	Sites           []SceneConfig `json:"sites"`
	DriveRange      int           `json:"driveRange"`
	VisibilityRange int           `json:"visibilityRange"`
	FireRange       int           `json:"fireRange"`
	Center          Vector        `json:"center"`
}

type GameConfig struct {
	tanksP1         []TankConfig
	tanksP2         []TankConfig
	hexes           []SceneConfig
	sites           []SceneConfig
	driveRange      int
	visibilityRange int
	fireRange       int
	p1First         bool
	center          Vector
	shrinkAfter     int
	shrinkInterval  int
	radius          int
}

func (gc GameConfig) ClientConfigs() (ClientConfig, ClientConfig) {
	p1 := ClientConfig{
		gc.tanksP1,
		gc.tanksP2,
		gc.hexes,
		gc.sites,
		gc.driveRange,
		gc.visibilityRange,
		gc.fireRange,
		gc.center,
	}
	p2 := ClientConfig{
		gc.tanksP2,
		gc.tanksP1,
		gc.hexes,
		gc.sites,
		gc.driveRange,
		gc.visibilityRange,
		gc.fireRange,
		gc.center,
	}
	return p1, p2
}

func NewBasicConfig(swapPlayers, p1First bool) GameConfig {
	center := Vector{2, 1}
	tanksP1 := []TankConfig{
		{1, Vector{0, 0}},
		{2, Vector{2, -2}},
	}
	tanksP2 := []TankConfig{
		{3, Vector{-2, -1}},
		{4, Vector{0, 1}},
	}
	hexes := []SceneConfig{
		{Vector{1, -2}, 2},
		{Vector{2, -2}, 1},
		{Vector{-2, -1}, 2},
		{Vector{-1, -1}, 2},
		{Vector{0, -1}, 2},
		{Vector{-2, 0}, 0},
		{Vector{0, 0}, 0},
		{Vector{1, 0}, 1},
		{Vector{-2, 1}, 0},
		{Vector{-1, 1}, 0},
		{Vector{0, 1}, 0},
		{Vector{1, 1}, 1},
		{Vector{-2, 2}, 0},
		{Vector{-1, 2}, 0},
	}
	radius := 0
	for _, h := range hexes {
		radius = max(radius, h.P.distance(center))
	}

	return GameConfig{
		tanksP1: tanksP1,
		tanksP2: tanksP2,
		hexes:   hexes,
		sites: []SceneConfig{
			{Vector{1, 0}, 8},
			{Vector{-1, 2}, 6},
		},
		driveRange:      5,
		visibilityRange: 2,
		fireRange:       3,
		p1First:         p1First,
		center:          center,
		shrinkAfter:     2,
		shrinkInterval:  3,
		radius:          radius,
	}
}

// MapBuilder constructs a game for the given first-resolver.
type MapBuilder func(p1First bool) GameConfig

// Maps are the arenas a room picks from. Add new ones here; every map must be
// point-symmetric so neither side is favoured.
var Maps = []struct {
	Name  string
	Build MapBuilder
}{
	{"outpost", func(p1First bool) GameConfig { return NewBasicConfig(false, p1First) }},
	{"duel", NewDuelConfig},
	{"crossroads", NewCrossroadsConfig},
}

// NewRandomConfig picks a random map.
func NewRandomConfig(p1First bool) GameConfig {
	return Maps[rand.IntN(len(Maps))].Build(p1First)
}

// hexArena returns every hex within radius of the centre, with a stable
// pseudo-random ground variant, minus any listed holes.
func hexArena(center Vector, radius int, holes ...Vector) []SceneConfig {
	skip := make(map[Vector]bool)
	for _, h := range holes {
		skip[h] = true
	}
	var hexes []SceneConfig
	for x := center.X - radius; x <= center.X+radius; x++ {
		for y := center.Y - radius; y <= center.Y+radius; y++ {
			p := Vector{x, y}
			if p.distance(center) > radius || skip[p] {
				continue
			}
			rel := p.sub(center)
			hexes = append(hexes, SceneConfig{P: p, Variant: abs(rel.X+2*rel.Y) % 3})
		}
	}
	return hexes
}

func negate(v Vector) Vector { return Vector{-v.X, -v.Y} }

// symmetricConfig builds a map from player one's half; player two's tanks and
// obstacles are the point reflection through the origin.
func symmetricConfig(radius int, p1First bool, starts []Vector, sites []Vector, shrinkAfter int) GameConfig {
	var tanksP1, tanksP2 []TankConfig
	for i, st := range starts {
		tanksP1 = append(tanksP1, TankConfig{Id: i + 1, P: st})
		tanksP2 = append(tanksP2, TankConfig{Id: len(starts) + i + 1, P: negate(st)})
	}
	var siteCfgs []SceneConfig
	seen := make(map[Vector]bool)
	for i, st := range sites {
		for _, p := range []Vector{st, negate(st)} {
			if seen[p] {
				continue
			}
			seen[p] = true
			siteCfgs = append(siteCfgs, SceneConfig{P: p, Variant: []int{6, 8, 7}[i%3]})
		}
	}
	return GameConfig{
		tanksP1:         tanksP1,
		tanksP2:         tanksP2,
		hexes:           hexArena(Vector{}, radius),
		sites:           siteCfgs,
		driveRange:      5,
		visibilityRange: 2,
		fireRange:       3,
		p1First:         p1First,
		center:          Vector{},
		shrinkAfter:     shrinkAfter,
		shrinkInterval:  3,
		radius:          radius,
	}
}

// NewDuelConfig is a small open arena where the tanks start out of sight.
func NewDuelConfig(p1First bool) GameConfig {
	return symmetricConfig(3, p1First,
		[]Vector{{-3, 1}, {-2, 3}},
		[]Vector{{0, 1}, {1, -2}},
		4,
	)
}

// NewCrossroadsConfig is a larger arena with three tanks per side and cover
// scattered across the middle.
func NewCrossroadsConfig(p1First bool) GameConfig {
	return symmetricConfig(4, p1First,
		[]Vector{{-4, 2}, {-3, 3}, {-4, 3}},
		[]Vector{{0, 1}, {2, -1}, {1, 2}, {-1, 3}},
		6,
	)
}
