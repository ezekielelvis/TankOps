package game

func abs(val int) int {
	if val < 0 {
		return -val
	}
	return val
}

type Vector struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func newZeroVector() Vector {
	return Vector{0, 0}
}

func (v Vector) Add(other Vector) Vector {
	return Vector{v.X + other.X, v.Y + other.Y}
}

func (v Vector) Sub(other Vector) Vector {
	return Vector{v.X - other.X, v.Y - other.Y}
}

func (v Vector) Distance(other Vector) int {
	diff := v.Sub(other)
	x := abs(diff.X)
	y := abs(diff.Y)
	xy := abs(diff.X + diff.Y)

	return max(x, y, xy)
}

func (v Vector) IsUnit() bool {
	if v.Distance(newZeroVector()) == 1 {
		return true
	}
	return false
}
