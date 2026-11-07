package bot

import "tank-ops/internal/game"

// What the bot knows

type botTank struct {
	p         vec
	destroyed bool
}

type enemyTrack struct {
	p         vec
	visible   bool
	destroyed bool
	age       int // turns since last seen
}

type botView struct {
	cfg    clientConfig
	hexes  map[vec]bool
	sites  map[vec]bool
	own    map[int]*botTank
	enemy  map[int]*enemyTrack
	danger int // hexes at this distance from the centre or further vanish at the end of this turn; 0 = none announced
	// weFirst says whether our orders resolve before the enemy's this turn.
	// Resolution order alternates every turn but is not announced, so it is
	// 0 (unknown) until the first turn's events reveal it.
	weFirst int // +1 we resolve first, -1 the enemy does, 0 unknown
}

func newBotView(cfg clientConfig) *botView {
	v := &botView{
		cfg:   cfg,
		hexes: make(map[vec]bool),
		sites: make(map[vec]bool),
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
			if o.P.Distance(t.P) <= cfg.VisibilityRange {
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
func (v *botView) inferOrder(results []turnResult) int {
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
		case game.TurnResultMove2:
			if r.Start {
				note(i, r.Id)
			}
		case game.TurnResultFire:
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

func (v *botView) apply(results []turnResult) {
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
		case game.TurnResultMove2:
			if !r.Start {
				v.moved(r.Id, r.P2)
			}
		case game.TurnResultMove3:
			v.moved(r.Id, r.P2)
		case game.TurnResultVisible:
			if e, ok := v.enemy[r.Id]; ok && !e.destroyed {
				e.visible = r.Visible
				if r.Visible {
					e.p = r.P
					e.age = 0
				}
			}
		case game.TurnResultExplosion:
			if r.Destroyed {
				v.destroyedTank(r.Id)
			}
		case game.TurnResultDestroyed:
			v.destroyedTank(r.Id)
		case game.TurnResultShrink:
			if r.Started {
				for p := range v.hexes {
					if p.Distance(v.cfg.Center) >= r.R {
						delete(v.hexes, p)
					}
				}
			} else {
				v.danger = r.R
			}
		}
	}
}

func (v *botView) moved(id int, p vec) {
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
	hexes  map[vec]bool
	sites  map[vec]bool
	mine   map[int]vec // alive tanks we control
	theirs map[int]vec // alive tanks we believe the opponent has
	center vec
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
		mine:       make(map[int]vec),
		theirs:     make(map[int]vec),
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
