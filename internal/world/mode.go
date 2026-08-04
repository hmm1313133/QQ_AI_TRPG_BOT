// 游戏模式配置（设计文档第九章）：模式 = 子系统开关的组合。
// 三种业务模式共享同一引擎内核，差异仅为配置。
package world

import (
	"fmt"
	"log"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
)

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

// NPCSeed 手动创建世界时的 NPC 种子数据（见《管理后台扩展设计.md》2.5）。
type NPCSeed struct {
	Name        string `json:"name"`        // 必填
	Kind        string `json:"kind"`        // npc / pc，默认 npc
	Disposition string `json:"disposition"` // friendly / neutral / suspicious / hostile，默认 neutral
	Personality string `json:"personality"` // 性格/目标描述，映射为 NPC 的 Goals
}

// SeedSpec 手动创建世界时的播种规格。
type SeedSpec struct {
	Mode       string    `json:"mode"`       // trpg / simrpg / roleplay
	Background string    `json:"background"` // 世界设定文本
	Scene      string    `json:"scene"`      // 初始场景描述（可选）
	NPCs       []NPCSeed `json:"npcs"`       // roleplay 至少 1 个
	Locations  []string  `json:"locations"`  // simrpg 用
	ScriptID   string    `json:"script_id"`  // trpg 专用：由调用方据此加载剧本后传入 scr
}

// SeedWorld 创建并播种一个世界（管理后台手动创建入口）。
// 校验：worldID 非空且不重复、模式合法、roleplay 至少 1 个 NPC。
// trpg 模式直接复用 InitFromScript，此时 scr 必填（调用方按 spec.ScriptID 加载）。
// 符合单写入原则：一切状态经 Engine 填充后由 Engine 落库。
func (e *Engine) SeedWorld(worldID string, spec SeedSpec, scr *script.Script) (*WorldState, error) {
	if worldID == "" {
		return nil, fmt.Errorf("世界 ID 不能为空")
	}
	if _, ok := modePresets[spec.Mode]; !ok {
		return nil, fmt.Errorf("未知游戏模式: %s", spec.Mode)
	}
	if spec.Mode == ModeRoleplay && len(spec.NPCs) == 0 {
		return nil, fmt.Errorf("roleplay 模式至少需要 1 个 NPC")
	}
	if spec.Mode == ModeTRPG && scr == nil {
		return nil, fmt.Errorf("trpg 模式必须提供剧本（spec.ScriptID 对应的 Script）")
	}
	for _, n := range spec.NPCs {
		if n.Name == "" {
			return nil, fmt.Errorf("NPC 名称不能为空")
		}
	}

	e.Lock(worldID)
	defer e.Unlock(worldID)

	// 重复 ID 报错，防止覆盖已有世界
	if existing, err := e.repo.Load(worldID); err == nil && existing != nil {
		return nil, fmt.Errorf("世界 %s 已存在", worldID)
	}

	if spec.Mode == ModeTRPG {
		return e.InitFromScript(worldID, scr)
	}

	state := NewWorldState(worldID, spec.Mode)
	state.Background = spec.Background
	if spec.Scene != "" {
		state.Scene.NodeName = "初始场景"
		state.Scene.Description = spec.Scene
	}
	for _, n := range spec.NPCs {
		kind := n.Kind
		if kind == "" {
			kind = "npc"
		}
		disposition := n.Disposition
		if disposition == "" {
			disposition = "neutral"
		}
		cs := &CharacterState{
			Name:        n.Name,
			Kind:        kind,
			Alive:       true,
			Disposition: disposition,
		}
		if n.Personality != "" {
			cs.Goals = []Goal{{Description: n.Personality, Priority: 5}}
		}
		state.Characters[n.Name] = cs
	}
	for i, loc := range spec.Locations {
		id := fmt.Sprintf("loc_%d", i)
		state.Locations[id] = &Location{ID: id, Name: loc}
	}

	if err := e.repo.Save(state); err != nil {
		return nil, fmt.Errorf("保存初始 WorldState 失败: %w", err)
	}

	log.Printf("[World] 手动播种世界: world=%s, mode=%s, npcs=%d, locations=%d",
		worldID, spec.Mode, len(spec.NPCs), len(spec.Locations))
	return state, nil
}
