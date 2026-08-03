// 游戏模式配置（设计文档第九章）：模式 = 子系统开关的组合。
// 三种业务模式共享同一引擎内核，差异仅为配置。
package world

// GameMode 游戏模式配置。
type GameMode struct {
	Name          string // trpg / simrpg / roleplay
	Ruleset       string // coc7 / dnd5e / lite / none
	EnableRules   bool   // 规则层（检定/成长）
	EnableScript  bool   // 剧本/时间轴
	EnablePlanner bool   // 低频场景规划
	EnableMemory  bool   // 记忆层
	EnableMood    bool   // 情绪/关系模拟
	EnableClock   bool   // 世界时钟与定时事件
	EnableOffline bool   // 离线演化 fastForward
}

var modePresets = map[string]GameMode{
	ModeTRPG: {
		Name: ModeTRPG, Ruleset: "coc7",
		EnableRules: true, EnableScript: true, EnablePlanner: true,
		EnableMemory: true, EnableMood: true, EnableClock: true,
		EnableOffline: false, // 剧本内时间，不做跨现实时间演化
	},
	ModeSimRPG: {
		Name: ModeSimRPG, Ruleset: "lite",
		EnableRules: true, EnableScript: false, EnablePlanner: true,
		EnableMemory: true, EnableMood: true, EnableClock: true,
		EnableOffline: true, // 开放世界，回归时结算世界变迁
	},
	ModeRoleplay: {
		Name: ModeRoleplay, Ruleset: "none",
		EnableRules: false, EnableScript: false, EnablePlanner: false,
		EnableMemory: true, EnableMood: true, EnableClock: true,
		EnableOffline: false,
	},
}

// GetMode 获取模式配置（未知模式回退为 TRPG）。
func GetMode(name string) GameMode {
	if m, ok := modePresets[name]; ok {
		return m
	}
	return modePresets[ModeTRPG]
}

// CreateWorld 创建指定模式的空世界（RPG 模拟/角色扮演的入口）。
func (e *Engine) CreateWorld(worldID, mode string) (*WorldState, error) {
	ws := NewWorldState(worldID, mode)
	if err := e.repo.Save(ws); err != nil {
		return nil, err
	}
	return ws, nil
}
