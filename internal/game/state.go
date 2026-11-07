package game

import "sort"

type GameState struct {
	cfg GameConfig

	tanksP1 map[int]*Tank
	tanksP2 map[int]*Tank
	hexes   map[Vector]*Hex

	turnP1 bool
	turn   int
	radius int

	curPlayer        map[int]*Tank
	curEnemy         map[int]*Tank
	curResultsPlayer []TurnResult
	curResultsEnemy  []TurnResult
}

func NewGameState(cfg GameConfig) *GameState {
	hexes := make(map[Vector]*Hex)
	tanksP1 := make(map[int]*Tank)
	tanksP2 := make(map[int]*Tank)

	for _, hexCfg := range cfg.hexes {
		hexes[hexCfg.P] = &Hex{hexCfg.P, true}
	}
	for _, site := range cfg.sites {
		hex, ok := hexes[site.P]
		if !ok {
			continue
		}
		hex.traversable = false
	}

	for _, pt := range cfg.tanksP1 {
		tanksP1[pt.Id] = &Tank{id: pt.Id, p: pt.P}
	}

	for _, et := range cfg.tanksP2 {
		tanksP2[et.Id] = &Tank{id: et.Id, p: et.P}
	}

	for _, t1 := range tanksP1 {
		for _, t2 := range tanksP2 {
			if t1.p.Distance(t2.p) <= cfg.visibilityRange {
				t1.visible = true
				t2.visible = true
			}
		}
	}

	return &GameState{
		cfg:     cfg,
		tanksP1: tanksP1,
		tanksP2: tanksP2,
		hexes:   hexes,
		turn:    1,
		turnP1:  cfg.p1First,
		radius:  cfg.radius,
	}
}

func (gs *GameState) ClientConfigs() (ClientConfig, ClientConfig) {
	return gs.cfg.ClientConfigs()
}

func (gs *GameState) Result() (GameResult, GameResult, bool) {
	hasTanksP1 := false
	for _, t := range gs.tanksP1 {
		if !t.destroyed {
			hasTanksP1 = true
			break
		}
	}
	hasTanksP2 := false
	for _, t := range gs.tanksP2 {
		if !t.destroyed {
			hasTanksP2 = true
			break
		}
	}
	if hasTanksP1 && hasTanksP2 {
		return Draw, Draw, false
	}
	if !hasTanksP1 && !hasTanksP2 {
		return Draw, Draw, true
	}
	if hasTanksP1 && !hasTanksP2 {
		return Win, Lose, true
	}
	return Lose, Win, true
}

func (gs *GameState) ResolveActions(p1, p2 []TankAction) ([]TurnResult, []TurnResult) {
	gs.curResultsPlayer = []TurnResult{}
	gs.curResultsEnemy = []TurnResult{}
	gs.curPlayer = gs.tanksP1
	gs.curEnemy = gs.tanksP2

	if gs.turnP1 {
		gs.resolveSinglePlayer(p1)
	}
	gs.curResultsPlayer, gs.curResultsEnemy = gs.curResultsEnemy, gs.curResultsPlayer
	gs.curPlayer, gs.curEnemy = gs.curEnemy, gs.curPlayer
	gs.resolveSinglePlayer(p2)
	gs.curResultsPlayer, gs.curResultsEnemy = gs.curResultsEnemy, gs.curResultsPlayer
	gs.curPlayer, gs.curEnemy = gs.curEnemy, gs.curPlayer

	if !gs.turnP1 {
		gs.resolveSinglePlayer(p1)
	}

	gs.resolveShrinking()

	gs.tanksP1 = gs.curPlayer
	gs.tanksP2 = gs.curEnemy
	gs.turnP1 = !gs.turnP1
	gs.resetExercised()
	gs.turn++

	return gs.curResultsPlayer, gs.curResultsEnemy

}

func (gs *GameState) resolveShrinking() {
	after := gs.cfg.shrinkAfter
	interval := gs.cfg.shrinkInterval
	shrinksNow := gs.turn >= after && (gs.turn-after)%interval == 0
	shrinksNext := gs.turn+1 >= after && (gs.turn+1-after)%interval == 0
	if !shrinksNow {
		if shrinksNext {
			shrinkResult := NewTurnResultShrink(gs.radius, false)
			gs.curResultsPlayer = append(gs.curResultsPlayer, shrinkResult)
			gs.curResultsEnemy = append(gs.curResultsEnemy, shrinkResult)
		}
		return
	}

	for _, t := range sortedTanks(gs.curEnemy) {
		if !t.destroyed && t.p.Distance(gs.cfg.center) >= gs.radius {
			t.destroyed = true
			res := NewTurnResultDestroyingExplosion(t.p, t.id)
			gs.curResultsEnemy = append(gs.curResultsEnemy, res)
			if t.visible {
				gs.curResultsPlayer = append(gs.curResultsPlayer, res)
			}
		}
	}

	for _, t := range sortedTanks(gs.curPlayer) {
		if !t.destroyed && t.p.Distance(gs.cfg.center) >= gs.radius {
			t.destroyed = true
			res := NewTurnResultDestroyingExplosion(t.p, t.id)
			gs.curResultsPlayer = append(gs.curResultsPlayer, res)
			if t.visible {
				gs.curResultsEnemy = append(gs.curResultsEnemy, res)
			}
		}
	}

	for _, h := range gs.hexes {
		if h.p.Distance(gs.cfg.center) >= gs.radius {
			delete(gs.hexes, h.p)
		}
	}

	shrinkResult := NewTurnResultShrink(gs.radius, true)
	gs.curResultsPlayer = append(gs.curResultsPlayer, shrinkResult)
	gs.curResultsEnemy = append(gs.curResultsEnemy, shrinkResult)

	gs.updateVisibilities()

	gs.radius--

	if shrinksNext {
		shrinkResult := NewTurnResultShrink(gs.radius, false)
		gs.curResultsPlayer = append(gs.curResultsPlayer, shrinkResult)
		gs.curResultsEnemy = append(gs.curResultsEnemy, shrinkResult)
	}
}

func (gs *GameState) resolveTankMove(path []Vector, tank *Tank) {
	validPath := gs.validPath(path, tank)
	if len(validPath) < 2 {
		return
	}

	startResult := NewTurnResultMove2(
		tank.id, validPath[0], validPath[1], true,
	)
	gs.curResultsPlayer = append(gs.curResultsPlayer, startResult)

	if tank.visible {
		gs.curResultsEnemy = append(gs.curResultsEnemy, startResult)
	}

	for i := 0; i < len(validPath)-2; i++ {
		p1, p2, p3 := validPath[i], validPath[i+1], validPath[i+2]
		tank.p = p2
		gs.updateVisibilities()

		midResult := NewTurnResultMove3(
			tank.id,
			p1,
			p2,
			p3,
		)
		gs.curResultsPlayer = append(gs.curResultsPlayer, midResult)
		if tank.visible {
			gs.curResultsEnemy = append(gs.curResultsEnemy, midResult)
		}
	}

	iLast := len(validPath) - 1
	tank.p = validPath[iLast]
	gs.updateVisibilities()

	endResult := NewTurnResultMove2(
		tank.id, validPath[iLast-1], validPath[iLast], false,
	)
	gs.curResultsPlayer = append(gs.curResultsPlayer, endResult)

	if tank.visible {
		gs.curResultsEnemy = append(gs.curResultsEnemy, endResult)
	}
}

func (gs *GameState) resolveTankFire(dir Vector, tank *Tank) {
	if !dir.IsUnit() {
		return
	}
	res := NewTurnResultFire(tank.id, dir)
	gs.curResultsPlayer = append(gs.curResultsPlayer, res)
	if tank.visible {
		gs.curResultsEnemy = append(gs.curResultsEnemy, res)
	}

	cur := tank.p
	for i := 0; i < gs.cfg.fireRange; i++ {
		cur = cur.Add(dir)
		if gs.collides(cur) {
			break
		}
	}

	visPlayer, visEnemy := gs.visible(cur)

	if colTank := gs.getCollidingTank(cur); colTank != nil {
		colTank.destroyed = true
		explosion := NewTurnResultDestroyingExplosion(cur, colTank.id)
		if visPlayer {
			gs.curResultsPlayer = append(gs.curResultsPlayer, explosion)
		}
		if visEnemy {
			gs.curResultsEnemy = append(gs.curResultsEnemy, explosion)
		}
		gs.updateVisibilities()
		return
	}
	explosion := NewTurnResultExplosion(cur)
	if visPlayer {
		gs.curResultsPlayer = append(gs.curResultsPlayer, explosion)
	}
	if visEnemy {
		gs.curResultsEnemy = append(gs.curResultsEnemy, explosion)
	}
}

func (gs *GameState) resolveSinglePlayer(actions []TankAction) {
	for _, action := range actions {
		tank, ok := gs.curPlayer[action.Id]
		if !ok || tank.exercised || tank.destroyed {
			continue
		}
		tank.exercised = true

		switch action.Type {
		case TankMove:
			gs.resolveTankMove(action.Path, tank)
		case TankFire:
			gs.resolveTankFire(action.Dir, tank)
		}
	}
}

func (gs *GameState) resetExercised() {
	for _, t := range gs.tanksP1 {
		t.exercised = false
	}
	for _, t := range gs.tanksP2 {
		t.exercised = false
	}
}

func (gs *GameState) visible(p Vector) (bool, bool) {
	visPlayer, visEnemy := false, false
	for _, t := range gs.curPlayer {
		if !t.destroyed && t.p.Distance(p) <= gs.cfg.visibilityRange {
			visPlayer = true
		}
	}
	for _, t := range gs.curEnemy {
		if !t.destroyed && t.p.Distance(p) <= gs.cfg.visibilityRange {
			visEnemy = true
		}
	}
	return visPlayer, visEnemy
}

func (gs *GameState) collides(p Vector) bool {
	if gs.collidesWithSite(p) {
		return true
	}
	tank := gs.getCollidingTank(p)
	if tank != nil {
		return true
	}
	return false
}

func (gs *GameState) getCollidingTank(p Vector) *Tank {
	for _, t := range gs.curEnemy {
		if !t.destroyed && t.p == p {
			return t
		}
	}
	for _, t := range gs.curPlayer {
		if !t.destroyed && t.p == p {
			return t
		}
	}
	return nil
}

func (gs *GameState) collidesWithSite(p Vector) bool {
	hex, ok := gs.hexes[p]
	if !ok || hex.traversable {
		return false
	}
	return true
}

func (gs *GameState) playerVisibilities(tanks1, tanks2 map[int]*Tank) []TurnResult {
	visibilities := []TurnResult{}

	for _, et := range sortedTanks(tanks2) {
		if et.seen {
			continue
		}
		isVisible := false
		for _, pt := range tanks1 {
			if pt.destroyed {
				continue
			}
			if et.p.Distance(pt.p) <= gs.cfg.visibilityRange {
				isVisible = true
				break
			}
		}

		if isVisible && !et.visible {
			if et.destroyed {
				et.seen = true
				res := NewTurnResultDestroyed(et.p, et.id)
				visibilities = append(visibilities, res)
			} else {
				visibilities = append(
					visibilities,
					NewTurnResultVisible(et.id, et.p, true),
				)
			}

		}
		if !et.destroyed && !isVisible && et.visible {
			visibilities = append(
				visibilities,
				NewTurnResultVisible(et.id, et.p, false),
			)
		}
		et.visible = isVisible
	}

	return visibilities
}

func (gs *GameState) updateVisibilities() {
	visibilitiesPlayer := gs.playerVisibilities(gs.curPlayer, gs.curEnemy)
	visibilitiesEnemy := gs.playerVisibilities(gs.curEnemy, gs.curPlayer)
	gs.curResultsPlayer = append(gs.curResultsPlayer, visibilitiesPlayer...)
	gs.curResultsEnemy = append(gs.curResultsEnemy, visibilitiesEnemy...)
}

func containsVector(vecs []Vector, v Vector) bool {
	for _, vv := range vecs {
		if v == vv {
			return true
		}
	}
	return false
}

func (gs *GameState) validPath(path []Vector, tank *Tank) []Vector {
	validPath := make([]Vector, 0)
	if len(path) < 2 || len(path) > gs.cfg.driveRange+1 || tank.p != path[0] {
		return validPath
	}

	for i := 1; i < len(path); i++ {
		if !gs.isTraversable(path[i]) {
			break
		}
		if path[i] == tank.p {
			break
		}
		if containsVector(validPath, path[i]) {
			break
		}
		if !path[i].Sub(path[i-1]).IsUnit() {
			break
		}

		if i == 1 {
			validPath = append(validPath, path[0])
		}
		validPath = append(validPath, path[i])
	}
	return validPath
}

func (gs *GameState) isTraversable(p Vector) bool {
	hex, ok := gs.hexes[p]
	if !ok || !hex.traversable {
		return false
	}
	for _, tank := range gs.curPlayer {
		if !tank.destroyed && p == tank.p {
			return false
		}
	}

	for _, tank := range gs.curEnemy {
		if !tank.destroyed && p == tank.p {
			return false
		}
	}

	return true
}

// sortedTanks returns the tanks ordered by id so that results are produced in
// a deterministic order (Go randomizes map iteration).
func sortedTanks(m map[int]*Tank) []*Tank {
	tanks := make([]*Tank, 0, len(m))
	for _, t := range m {
		tanks = append(tanks, t)
	}
	sort.Slice(tanks, func(i, j int) bool { return tanks[i].id < tanks[j].id })
	return tanks
}
