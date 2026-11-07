package game

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
