package main

import (
	"log/slog"
	"math/rand/v2"
	"sort"
	"time"
)

// BotLevel is the computer opponent's difficulty.
type BotLevel int

const (
	BotEasy   BotLevel = 1
	BotMedium BotLevel = 2
	BotHard   BotLevel = 3
)

func (l BotLevel) String() string {
	switch l {
	case BotEasy:
		return "easy"
	case BotMedium:
		return "medium"
	case BotHard:
		return "hard"
	}
	return "unknown"
}

// Bot plays one side of a game. It only ever sees what a human player sees:
// the ClientConfig at the start and the filtered TurnResults afterwards, so
// fog of war applies to it exactly as it does to people.
type Bot struct {
	level BotLevel
	rng   *rand.Rand
	view  *botView
}

func NewBot(level BotLevel, seed uint64) *Bot {
	return &Bot{level: level, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// Start tells the bot about a new game.
func (b *Bot) Start(cfg ClientConfig) {
	b.view = newBotView(cfg)
}

// Observe feeds the bot the results of a resolved turn.
func (b *Bot) Observe(results []TurnResult) {
	b.view.apply(results)
}

// Plan returns the bot's actions for the coming turn.
func (b *Bot) Plan() []TankAction {
	snap := b.view.snapshot()
	switch b.level {
	case BotEasy:
		return easyPlan(snap, b.rng)
	case BotMedium:
		return mediumPlan(snap, b.rng)
	default:
		return hardPlan(snap, b.view, b.rng)
	}
}

// runBot drives a Bot through the same channels a human player's goroutine
// uses, so the room cannot tell the difference.
func runBot(level BotLevel, chans RoomChans, seed int64) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("bot panicked", "panic", rec)
		}
	}()

	bot := NewBot(level, uint64(seed))
	var pending *PlayerMessage
	for {
		// Only try to send when there is something to send. A nil channel
		// blocks forever in select, which is what we want otherwise.
		var out chan PlayerMessage
		var msg PlayerMessage
		if pending != nil {
			out = chans.send
			msg = *pending
		}

		select {
		case rm, ok := <-chans.read:
			if !ok {
				return
			}
			switch rm.msgType {
			case RoomGameStarted:
				bot.Start(rm.config)
				pending = &PlayerMessage{msgType: PlayerSendTurn, tankActions: bot.Plan()}
			case RoomTurnResult:
				if bot.view == nil {
					break
				}
				bot.Observe(rm.turnResults)
				pending = &PlayerMessage{msgType: PlayerSendTurn, tankActions: bot.Plan()}
			case RoomGameFinished:
				pending = nil
			}
		case out <- msg:
			pending = nil
		}
	}
}

// ---------------------------------------------------------------------------
// What the bot knows
// ---------------------------------------------------------------------------

type botTank struct {
	p         Vector
	destroyed bool
}

type enemyTrack struct {
	p         Vector
	visible   bool
	destroyed bool
	age       int // turns since last seen
}

type botView struct {
	cfg    ClientConfig
	hexes  map[Vector]bool
	sites  map[Vector]bool
	own    map[int]*botTank
	enemy  map[int]*enemyTrack
	danger int // hexes at this distance from the centre or further vanish at the end of this turn; 0 = none announced
	// weFirst says whether our orders resolve before the enemy's this turn.
	// Resolution order alternates every turn but is not announced, so it is
	// 0 (unknown) until the first turn's events reveal it.
	weFirst int // +1 we resolve first, -1 the enemy does, 0 unknown
}

func newBotView(cfg ClientConfig) *botView {
	v := &botView{
		cfg:   cfg,
		hexes: make(map[Vector]bool),
		sites: make(map[Vector]bool),
		own:   make(map[int]*botTank),
		enemy: make(map[int]*enemyTrack),
	}
	for _, h := range cfg.Hexes {
		v.hexes[h.P] = true
	}
	for _, s := range cfg.Sites {
		v.sites[s.P] = true
	}
	for _, t := range cfg.PlayerTanks {
		v.own[t.Id] = &botTank{p: t.P}
	}
	for _, t := range cfg.EnemyTanks {
		visible := false
		for _, o := range cfg.PlayerTanks {
			if o.P.distance(t.P) <= cfg.VisibilityRange {
				visible = true
			}
		}
		v.enemy[t.Id] = &enemyTrack{p: t.P, visible: visible}
	}
	return v
}

// inferOrder works out who resolved first this turn from the order of events
// in the results (which are chronological). It needs to have seen an action
// from both sides; if the enemy acted out of sight it returns 0 (unknown).
func (v *botView) inferOrder(results []TurnResult) int {
	firstOwn, firstEnemy := -1, -1
	note := func(i, id int) {
		if _, ok := v.own[id]; ok {
			if firstOwn < 0 {
				firstOwn = i
			}
		} else if _, ok := v.enemy[id]; ok {
			if firstEnemy < 0 {
				firstEnemy = i
			}
		}
	}
	for i, r := range results {
		switch r := r.(type) {
		case TurnResultMove2:
			if r.Start {
				note(i, r.Id)
			}
		case TurnResultFire:
			note(i, r.Id)
		}
	}
	switch {
	case firstOwn < 0 || firstEnemy < 0:
		return 0
	case firstOwn < firstEnemy:
		return 1
	default:
		return -1
	}
}

func (v *botView) apply(results []TurnResult) {
	// the turn just played had order v.weFirst; the next turn flips it
	if v.weFirst == 0 {
		v.weFirst = v.inferOrder(results)
	}
	if v.weFirst != 0 {
		v.weFirst = -v.weFirst
	}
	for _, e := range v.enemy {
		if !e.visible {
			e.age++
		}
	}
	v.danger = 0
	for _, r := range results {
		switch r := r.(type) {
		case TurnResultMove2:
			if !r.Start {
				v.moved(r.Id, r.P2)
			}
		case TurnResultMove3:
			v.moved(r.Id, r.P2)
		case TurnResultVisible:
			if e, ok := v.enemy[r.Id]; ok && !e.destroyed {
				e.visible = r.Visible
				if r.Visible {
					e.p = r.P
					e.age = 0
				}
			}
		case TurnResultExplosion:
			if r.Destroyed {
				v.destroyedTank(r.Id)
			}
		case TurnResultDestroyed:
			v.destroyedTank(r.Id)
		case TurnResultShrink:
			if r.Started {
				for p := range v.hexes {
					if p.distance(v.cfg.Center) >= r.R {
						delete(v.hexes, p)
					}
				}
			} else {
				v.danger = r.R
			}
		}
	}
}

func (v *botView) moved(id int, p Vector) {
	if t, ok := v.own[id]; ok {
		t.p = p
		return
	}
	if e, ok := v.enemy[id]; ok && !e.destroyed {
		e.p = p
		e.visible = true
		e.age = 0
	}
}

func (v *botView) destroyedTank(id int) {
	if t, ok := v.own[id]; ok {
		t.destroyed = true
	}
	if e, ok := v.enemy[id]; ok {
		e.destroyed = true
		e.visible = false
	}
}

// snapshot is a self-contained picture of the board from one side. The
// policies work on snapshots so the same code can play the bot's side and,
// inside rollouts, the imagined enemy's side.
type botSnap struct {
	hexes  map[Vector]bool
	sites  map[Vector]bool
	mine   map[int]Vector // alive tanks we control
	theirs map[int]Vector // alive tanks we believe the opponent has
	center Vector
	danger int
	// weFirst is the resolution order for the turn being planned: +1 we
	// resolve first, -1 the enemy does, 0 unknown. The turn after flips it.
	weFirst int

	driveRange, fireRange, visRange int
}

func (v *botView) snapshot() *botSnap {
	s := &botSnap{
		hexes:      v.hexes,
		sites:      v.sites,
		mine:       make(map[int]Vector),
		theirs:     make(map[int]Vector),
		center:     v.cfg.Center,
		danger:     v.danger,
		weFirst:    v.weFirst,
		driveRange: v.cfg.DriveRange,
		fireRange:  v.cfg.FireRange,
		visRange:   v.cfg.VisibilityRange,
	}
	for id, t := range v.own {
		if !t.destroyed {
			s.mine[id] = t.p
		}
	}
	for id, e := range v.enemy {
		if !e.destroyed {
			s.theirs[id] = e.p
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// Geometry helpers
// ---------------------------------------------------------------------------

var hexDirs = []Vector{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, -1}, {-1, 1}}

func sortedIDs(m map[int]Vector) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func (s *botSnap) occupied(skipID int, extra map[Vector]bool) map[Vector]bool {
	occ := make(map[Vector]bool)
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
func (s *botSnap) reachable(from Vector, blocked map[Vector]bool) map[Vector][]Vector {
	paths := map[Vector][]Vector{from: {from}}
	frontier := []Vector{from}
	for step := 0; step < s.driveRange; step++ {
		var next []Vector
		for _, cur := range frontier {
			for _, d := range hexDirs {
				n := cur.add(d)
				if _, seen := paths[n]; seen {
					continue
				}
				if !s.hexes[n] || s.sites[n] || blocked[n] {
					continue
				}
				p := make([]Vector, len(paths[cur])+1)
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
func (s *botSnap) shotHits(from, dir, target Vector, friendly map[Vector]bool) bool {
	cur := from
	for i := 0; i < s.fireRange; i++ {
		cur = cur.add(dir)
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
func (s *botSnap) firingSolutions(id int, p Vector) []Vector {
	friendly := make(map[Vector]bool)
	for oid, op := range s.mine {
		if oid != id {
			friendly[op] = true
		}
	}
	var dirs []Vector
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

func (s *botSnap) nearestEnemy(p Vector) (Vector, int, bool) {
	best, bestD, ok := Vector{}, 1<<30, false
	for _, id := range sortedIDs(s.theirs) {
		e := s.theirs[id]
		if d := p.distance(e); d < bestD {
			best, bestD, ok = e, d, true
		}
	}
	return best, bestD, ok
}

func (s *botSnap) inDanger(p Vector) bool {
	return s.danger > 0 && p.distance(s.center) >= s.danger
}

func moveAction(id int, path []Vector) TankAction {
	return TankAction{Type: TankMove, Id: id, Path: path}
}

func fireAction(id int, dir Vector) TankAction {
	return TankAction{Type: TankFire, Id: id, Dir: dir}
}

// ---------------------------------------------------------------------------
// Easy: advances on the enemy and shoots when it happens to line up.
// ---------------------------------------------------------------------------

func easyPlan(s *botSnap, rng *rand.Rand) []TankAction {
	var actions []TankAction
	claimed := make(map[Vector]bool)

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

		var dest Vector
		switch {
		case s.inDanger(p):
			dest = pickMin(dests, rng, func(h Vector) int {
				pen := 0
				if s.inDanger(h) {
					pen = 100
				}
				return pen + h.distance(s.center)
			})
		case rng.Float64() < 0.3:
			dest = dests[rng.IntN(len(dests))]
		default:
			target := s.center
			if e, _, ok := s.nearestEnemy(p); ok {
				target = e
			}
			dest = pickMin(dests, rng, func(h Vector) int {
				pen := 0
				if s.inDanger(h) {
					pen = 100
				}
				return pen + h.distance(target)
			})
		}
		claimed[dest] = true
		actions = append(actions, moveAction(id, reach[dest]))
	}
	return actions
}

func sortedVectors(m map[Vector][]Vector) []Vector {
	vs := make([]Vector, 0, len(m))
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
func pickMin(items []Vector, rng *rand.Rand, cost func(Vector) int) Vector {
	best := 1 << 30
	var winners []Vector
	for _, it := range items {
		c := cost(it)
		switch {
		case c < best:
			best = c
			winners = []Vector{it}
		case c == best:
			winners = append(winners, it)
		}
	}
	return winners[rng.IntN(len(winners))]
}

// ---------------------------------------------------------------------------
// Medium: scores every option for every tank.
// ---------------------------------------------------------------------------

type candidate struct {
	action TankAction
	score  float64
}

// positionScore says how good it is to end a turn with a tank at h, given
// where the other tanks of our side are.
func (s *botSnap) positionScore(id int, h Vector, rng *rand.Rand) float64 {
	score := 0.0
	if s.danger > 0 {
		d := h.distance(s.center)
		switch {
		case d >= s.danger:
			score -= 500 // will be destroyed
		case d == s.danger-1:
			score -= 3 // next in line
		}
	}

	friendly := make(map[Vector]bool)
	for oid, op := range s.mine {
		if oid != id {
			friendly[op] = true
			if op.distance(h) <= 1 {
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
		d := h.distance(e)
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

func (s *botSnap) candidates(id int, claimed map[Vector]bool, rng *rand.Rand) []candidate {
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
	cands = append(cands, candidate{TankAction{}, s.positionScore(id, p, rng) - 1})

	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	return cands
}

func mediumPlan(s *botSnap, rng *rand.Rand) []TankAction {
	var actions []TankAction
	claimed := make(map[Vector]bool)
	for _, id := range sortedIDs(s.mine) {
		cands := s.candidates(id, claimed, rng)
		best := cands[0]
		if best.action.Type == 0 {
			continue // hold
		}
		if best.action.Type == TankMove {
			claimed[best.action.Path[len(best.action.Path)-1]] = true
		}
		actions = append(actions, best.action)
	}
	return actions
}

// ---------------------------------------------------------------------------
// Hard: tries the best few options per tank against many guesses of what the
// enemy is doing, using the real game rules to resolve each guess.
// ---------------------------------------------------------------------------

const (
	hardTopPerTank = 5
	hardSamples    = 32
	hardMaxPlans   = 40
	hardTimeBudget = 400 * time.Millisecond
)

func hardPlan(s *botSnap, v *botView, rng *rand.Rand) []TankAction {
	ids := sortedIDs(s.mine)
	if len(ids) == 0 {
		return nil
	}

	// shortlist of options per tank
	claimed := make(map[Vector]bool)
	options := make([][]TankAction, len(ids))
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

func cleanPlan(plan []TankAction) []TankAction {
	out := make([]TankAction, 0, len(plan))
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
func joinPlans(options [][]TankAction, rng *rand.Rand, limit int) [][]TankAction {
	plans := [][]TankAction{{}}
	for _, opts := range options {
		var next [][]TankAction
		for _, plan := range plans {
			for _, o := range opts {
				if conflicts(plan, o) {
					continue
				}
				np := make([]TankAction, len(plan)+1)
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
		plans = append([][]TankAction{first}, plans[:limit-1]...)
	}
	return plans
}

func conflicts(plan []TankAction, a TankAction) bool {
	if a.Type != TankMove {
		return false
	}
	end := a.Path[len(a.Path)-1]
	for _, o := range plan {
		if o.Type == TankMove && o.Path[len(o.Path)-1] == end {
			return true
		}
	}
	return false
}

type enemyGuess struct {
	positions map[int]Vector
	actions   []TankAction
	p1First   bool // whether we (as player one in the rollout) resolve first
}

// sampleEnemy imagines where the enemy tanks are and what they will do.
func sampleEnemy(s *botSnap, v *botView, rng *rand.Rand) *enemyGuess {
	g := &enemyGuess{positions: make(map[int]Vector), p1First: rng.IntN(2) == 0}
	if s.weFirst != 0 {
		g.p1First = s.weFirst > 0
	}

	taken := make(map[Vector]bool)
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
			var spots []Vector
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
func rollout(s *botSnap, plan []TankAction, g *enemyGuess) float64 {
	var tanksMine, tanksTheirs []TankConfig
	for _, id := range sortedIDs(s.mine) {
		tanksMine = append(tanksMine, TankConfig{Id: id, P: s.mine[id]})
	}
	for _, id := range sortedIDs(g.positions) {
		tanksTheirs = append(tanksTheirs, TankConfig{Id: id, P: g.positions[id]})
	}
	hexes := make([]SceneConfig, 0, len(s.hexes))
	for h := range s.hexes {
		hexes = append(hexes, SceneConfig{P: h})
	}
	var sites []SceneConfig
	for p := range s.sites {
		sites = append(sites, SceneConfig{P: p, Variant: 6})
	}

	cfg := GameConfig{
		tanksP1:         tanksMine,
		tanksP2:         tanksTheirs,
		hexes:           hexes,
		sites:           sites,
		driveRange:      s.driveRange,
		visibilityRange: s.visRange,
		fireRange:       s.fireRange,
		p1First:         g.p1First,
		center:          s.center,
		shrinkAfter:     1000,
		shrinkInterval:  1000, // only the announced shrink, on this turn
		radius:          s.danger,
	}
	if s.danger > 0 {
		cfg.shrinkAfter = 1 // the announced shrink lands at the end of this turn
	}
	gs := NewGameState(cfg)
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
		mine: map[int]Vector{}, theirs: map[int]Vector{},
		weFirst:    s.weFirst, // positionScore flips it to get next turn's order
		driveRange: s.driveRange, fireRange: s.fireRange, visRange: s.visRange,
	}
	for id, t := range gs.tanksP1 {
		if !t.destroyed {
			end.mine[id] = t.p
		}
	}
	for id, t := range gs.tanksP2 {
		if !t.destroyed {
			end.theirs[id] = t.p
		}
	}
	for id, p := range end.mine {
		score += end.positionScore(id, p, nil)
	}
	return score
}

// tally scores the material balance: tanks destroyed minus tanks lost.
func tally(gs *GameState) float64 {
	score := 0.0
	for _, t := range gs.tanksP1 {
		if t.destroyed {
			score -= 1000
		}
	}
	for _, t := range gs.tanksP2 {
		if t.destroyed {
			score += 1000
		}
	}
	return score
}

// shootIfAligned has every living tank on one side fire if it has a shot.
func shootIfAligned(gs *GameState, first bool) []TankAction {
	mine, theirs := gs.tanksP1, gs.tanksP2
	if !first {
		mine, theirs = theirs, mine
	}
	snap := &botSnap{
		hexes: map[Vector]bool{}, sites: map[Vector]bool{},
		mine: map[int]Vector{}, theirs: map[int]Vector{},
		fireRange: gs.cfg.fireRange,
	}
	for p, h := range gs.hexes {
		snap.hexes[p] = true
		if !h.traversable {
			snap.sites[p] = true
		}
	}
	for id, t := range mine {
		if !t.destroyed {
			snap.mine[id] = t.p
		}
	}
	for id, t := range theirs {
		if !t.destroyed {
			snap.theirs[id] = t.p
		}
	}
	var actions []TankAction
	for _, id := range sortedIDs(snap.mine) {
		if dirs := snap.firingSolutions(id, snap.mine[id]); len(dirs) > 0 {
			actions = append(actions, fireAction(id, dirs[0]))
		}
	}
	return actions
}
