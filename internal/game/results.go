package game

type TankActionType int

const (
	TankMove TankActionType = 1
	TankFire TankActionType = 2
)

type TankAction struct {
	Type TankActionType `json:"type"`
	Id   int            `json:"id"`
	Dir  Vector         `json:"dir"`
	Path []Vector       `json:"path"`
}

type Tank struct {
	id        int
	p         Vector
	exercised bool
	visible   bool
	destroyed bool
	seen      bool
}

type Hex struct {
	p           Vector
	traversable bool
}

type GameResult int

const (
	Win  GameResult = 1
	Draw GameResult = 2
	Lose GameResult = 3
)

type TurnResult interface {
	isTurnResult()
}

type TurnResultType int

const (
	Move2     TurnResultType = 1
	Move3     TurnResultType = 2
	Fire      TurnResultType = 3
	Explosion TurnResultType = 4
	Destroyed TurnResultType = 5
	Visible   TurnResultType = 6
	Shrink    TurnResultType = 7
)

type TurnResultMove2 struct {
	Type  TurnResultType `json:"type"`
	Id    int            `json:"id"`
	P1    Vector         `json:"p1"`
	P2    Vector         `json:"p2"`
	Start bool           `json:"start"`
}

func (tr TurnResultMove2) isTurnResult() {}
func NewTurnResultMove2(id int, p1, p2 Vector, start bool) TurnResultMove2 {
	return TurnResultMove2{Type: Move2, Id: id, P1: p1, P2: p2, Start: start}
}

type TurnResultMove3 struct {
	Type TurnResultType `json:"type"`
	Id   int            `json:"id"`
	P1   Vector         `json:"p1"`
	P2   Vector         `json:"p2"`
	P3   Vector         `json:"p3"`
}

func (tr TurnResultMove3) isTurnResult() {}
func NewTurnResultMove3(id int, p1, p2, p3 Vector) TurnResultMove3 {
	return TurnResultMove3{Type: Move3, Id: id, P1: p1, P2: p2, P3: p3}
}

type TurnResultFire struct {
	Type TurnResultType `json:"type"`
	Id   int            `json:"id"`
	Dir  Vector         `json:"dir"`
}

func (tr TurnResultFire) isTurnResult() {}
func NewTurnResultFire(id int, dir Vector) TurnResultFire {
	return TurnResultFire{Type: Fire, Id: id, Dir: dir}
}

type TurnResultExplosion struct {
	Type      TurnResultType `json:"type"`
	P         Vector         `json:"p"`
	Destroyed bool           `json:"destroyed"`
	Id        int            `json:"id"`
}

func (tr TurnResultExplosion) isTurnResult() {}
func NewTurnResultExplosion(p Vector) TurnResultExplosion {
	return TurnResultExplosion{Type: Explosion, P: p, Destroyed: false}
}
func NewTurnResultDestroyingExplosion(p Vector, id int) TurnResultExplosion {
	return TurnResultExplosion{Type: Explosion, P: p, Destroyed: true, Id: id}
}

type TurnResultDestroyed struct {
	Type TurnResultType `json:"type"`
	P    Vector         `json:"p"`
	Id   int            `json:"id"`
}

func (tr TurnResultDestroyed) isTurnResult() {}
func NewTurnResultDestroyed(p Vector, id int) TurnResultDestroyed {
	return TurnResultDestroyed{Type: Destroyed, P: p, Id: id}
}

type TurnResultVisible struct {
	Type    TurnResultType `json:"type"`
	Id      int            `json:"id"`
	P       Vector         `json:"p"`
	Visible bool           `json:"visible"`
}

func (tr TurnResultVisible) isTurnResult() {}
func NewTurnResultVisible(id int, p Vector, visible bool) TurnResultVisible {
	return TurnResultVisible{Type: Visible, Id: id, P: p, Visible: visible}
}

type TurnResultShrink struct {
	Type    TurnResultType `json:"type"`
	R       int            `json:"r"`
	Started bool           `json:"started"`
}

func (tr TurnResultShrink) isTurnResult() {}
func NewTurnResultShrink(r int, started bool) TurnResultShrink {
	return TurnResultShrink{Type: Shrink, R: r, Started: started}
}
