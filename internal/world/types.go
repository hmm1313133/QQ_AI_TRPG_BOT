// Package world 实现 AI 世界引擎的核心：确定性世界状态层。
//
// 设计见《AI世界引擎技术设计.md》第四~六章。核心原则：
//   - 硬状态/软叙事分离：本包管理的全部是客观事实（硬状态），
//     LLM 只通过 ApplyEvent 请求变更，由引擎校验后落库。
//   - 单一写入者：一切状态变更必须经过 WorldEngine.ApplyEvent。
//   - 双份真相合并：WorldState 合并了旧 GameState（微观运行态）
//     与 Progress（宏观进度），一个世界实例对应一份持久化文档。
package world

// 游戏模式（见设计文档第九章）。
const (
	ModeTRPG     = "trpg"     // AI 跑团（剧本驱动）
	ModeSimRPG   = "simrpg"   // 文字 RPG 模拟（开放世界）
	ModeRoleplay = "roleplay" // AI 角色扮演（单 NPC 深度对话）
)

// WorldState 是一个世界实例（= 一个 QQ 群的一场游戏）的全部硬状态。
// 合并自旧 GameState（internal/agent/gamestate.go）与 Progress（internal/script/types.go）。
type WorldState struct {
	WorldID    string `json:"world_id"`
	Mode       string `json:"mode"`
	ScriptID   string `json:"script_id,omitempty"`
	ScriptName string `json:"script_name,omitempty"`

	// Background 是不可变的剧本/世界设定（旧 StoryContext 拆分而来）。
	Background string `json:"background"`
	// CampaignSummary 是滚动更新的战役摘要（不再覆盖 Background）。
	CampaignSummary string `json:"campaign_summary,omitempty"`
	// ReplyStyle 是本世界的回复风格指令（自由文本，如"冷峻克苏鲁风，重对话少环境铺陈"），
	// 非空时每轮经 ContextBuilder 注入 Narrator（Author's Note 位置）。
	ReplyStyle string `json:"reply_style,omitempty"`

	// Lore 世界设定库（Lorebook 条目，设计文档《世界设定库与按需加载设计.md》§4.1）。
	// 内嵌进 WorldState：条目与世界强绑定，沿用每世界单 JSON 文件存储。
	Lore []LoreEntry `json:"lore,omitempty"`

	Clock      WorldClock                 `json:"clock"`
	Scene      SceneState                 `json:"scene"`
	ScenePlan  string                     `json:"scene_plan,omitempty"` // Planner 的场景级计划（替代旧 LastDirective）
	// PlanNodeID / PlanRound 记录 ScenePlan 生成时的场景与轮次，用于低频刷新判断。
	PlanNodeID string                     `json:"plan_node_id,omitempty"`
	PlanRound  int                        `json:"plan_round,omitempty"`
	Characters map[string]*CharacterState `json:"characters"`           // name -> NPC（PC 经 CardRef 关联角色卡）
	Locations  map[string]*Location       `json:"locations,omitempty"`
	Factions   map[string]*Faction        `json:"factions,omitempty"`
	Items      map[string]*Item           `json:"items,omitempty"`    // 物品/道具（设计 §3.2）
	Storyline  *Storyline                 `json:"storyline,omitempty"` // 导演主线（设计 §3.4）
	Quests     []QuestState               `json:"quests"`     // 旧 Objectives 演化
	HiddenInfo []HiddenItem               `json:"hidden_info"`
	EventQueue []ScheduledEvent           `json:"event_queue"` // 旧 PendingEvents 演化
	// CompletedNodes 已完成的时间轴节点 ID（旧 Progress.CompletedNodes）。
	CompletedNodes []string               `json:"completed_nodes,omitempty"`
	Relations  []RelationEdge             `json:"relations,omitempty"`
	Locks      []StateLock                `json:"locks,omitempty"`
	Facts      []Fact                     `json:"facts,omitempty"`
	EventLog   []WorldEvent               `json:"event_log,omitempty"`
	// SkillUses 待结算的技能成功使用记录（成长闭环，幕间统一结算）。
	SkillUses  []SkillUseRecord           `json:"skill_uses,omitempty"`
	// RecentTurns 最近对话回合（短期记忆窗口，环形保留最近 RecentTurnsCap 轮）。
	// Narrator 是无状态调用，跨轮叙事连续性由本窗口 + CampaignSummary + 记忆层承载。
	RecentTurns []DialogueTurn            `json:"recent_turns,omitempty"`
	Metrics    Metrics                    `json:"metrics"`
	RoundCount int                        `json:"round_count"`
	LastUpdate string                     `json:"last_update"`
}

// RecentTurnsCap 短期对话窗口保留的回合数。
const RecentTurnsCap = 10

// DialogueTurn 一轮对话（玩家行动 + KP 叙事）。
type DialogueTurn struct {
	Round    int    `json:"round"`
	Player   string `json:"player"`
	Narrator string `json:"narrator"`
}

// AppendTurn 追加一轮对话到短期窗口（超出容量淘汰最旧）。
func (ws *WorldState) AppendTurn(player, narrator string) {
	ws.RecentTurns = append(ws.RecentTurns, DialogueTurn{
		Round: ws.RoundCount, Player: player, Narrator: narrator,
	})
	if len(ws.RecentTurns) > RecentTurnsCap {
		ws.RecentTurns = ws.RecentTurns[len(ws.RecentTurns)-RecentTurnsCap:]
	}
}

// WorldClock 世界时钟（设计文档 4.2）。
// 时间相关计算（情绪衰减、记忆 recency）一律使用世界时间，
// 暂停一周的团不会"过期"。
type WorldClock struct {
	WorldTime  int64 `json:"world_time"`  // 世界内时间（分钟计数）
	RealLapsed int64 `json:"real_lapsed"` // 上次活跃时的现实时间戳（秒），离线演化依据
}

// SceneState 当前场景运行态。
type SceneState struct {
	NodeID           string   `json:"node_id"`
	NodeName         string   `json:"node_name"`
	NodeType         string   `json:"node_type"`
	Description      string   `json:"description"`
	Narrative        string   `json:"narrative,omitempty"`
	Atmosphere       string   `json:"atmosphere,omitempty"`
	DangerLevel      string   `json:"danger_level,omitempty"`
	InvestigationPts []string `json:"investigation_points,omitempty"`
	Exits            []string `json:"exits,omitempty"`
	KPNotes          string   `json:"kp_notes,omitempty"`
}

// CharacterState PC/NPC 统一实体（旧 NPCState 扩展）。
// 数值真相（HP/SAN/技能）仍在 character.Card，这里只存引用与叙事状态。
type CharacterState struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"` // npc / pc
	Role          string   `json:"role,omitempty"`
	CardRef       string   `json:"card_ref,omitempty"` // 关联 character.Card ID
	Alive         bool     `json:"alive"`
	Disposition   string   `json:"disposition"` // friendly / neutral / suspicious / hostile / dead
	Location      string   `json:"location,omitempty"`
	CurrentAction string   `json:"current_action,omitempty"`
	Motivation    string   `json:"motivation,omitempty"`
	Secrets       string   `json:"secrets,omitempty"`
	DialogueStyle string   `json:"dialogue_style,omitempty"`
	KeyDialogue   []string `json:"key_dialogue,omitempty"`
	Traits        []string `json:"traits,omitempty"` // 性格特质标签（记仇/胆小/贪婪…）
	// 玩家创作字段（《世界编辑器与素材联动设计.md》§3.1）：叙事软信息，不影响规则判定。
	Appearance  string   `json:"appearance,omitempty"`  // 外貌描写
	Personality string   `json:"personality,omitempty"` // 性格描述（长文；Traits 是短标签）
	Backstory   string   `json:"backstory,omitempty"`   // 背景故事
	Skills      []string `json:"skills,omitempty"`      // 能力/特长描述，如 "剑术(娴熟)"
	Goals       []Goal   `json:"goals,omitempty"`       // NPC 目标（模拟层驱动）
	Mood          MoodState `json:"mood,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

// MoodState 情绪状态（设计文档 6.2）。
type MoodState struct {
	Valence   int      `json:"valence"` // -100..100（愉悦度）
	Arousal   int      `json:"arousal"` // 0..100（激活度）
	Tags      []string `json:"tags,omitempty"`
	UpdatedAt int64    `json:"updated_at"` // 世界时间
}

// Goal NPC 目标。
type Goal struct {
	Description string `json:"description"`
	Priority    int    `json:"priority"` // 1-10
	Progress    int    `json:"progress"` // 0-100
}

// SkillUseRecord 一次成功的技能使用记录（待幕间成长结算）。
type SkillUseRecord struct {
	UserID string `json:"user_id"`
	Skill  string `json:"skill"`
	Round  int    `json:"round"`
}

// Location 地点（RPG 模式全量，TRPG 模式按需）。
type Location struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Exits       []string `json:"exits,omitempty"`
	Atmosphere  string   `json:"atmosphere,omitempty"` // 氛围（设计 §3.3）
	Danger      string   `json:"danger,omitempty"`     // 危险度
	Points      []string `json:"points,omitempty"`     // 兴趣点/可调查处
}

// Faction 势力/组织。
type Faction struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Reputation  int      `json:"reputation"` // 玩家在该势力中的声誉 -100..100
	Description string   `json:"description,omitempty"` // 设计 §3.3
	Goals       []string `json:"goals,omitempty"`       // 势力目标
	Leader      string   `json:"leader,omitempty"`      // 领袖（可对应角色名）
}

// Item 世界内物品/道具（硬状态，可追踪与转移；设计 §3.2）。
type Item struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"` // weapon / consumable / key / material / other
	Description string   `json:"description,omitempty"`
	Location    string   `json:"location,omitempty"` // 所在地点名
	Owner       string   `json:"owner,omitempty"`    // 持有者（角色名；"玩家" 表示玩家持有）
	Tags        []string `json:"tags,omitempty"`
}

// Storyline 导演系统主线剧情脊柱（simrpg/roleplay 手工编排；
// trpg 模式由 InitFromScript 从剧本时间轴派生镜像，设计 §11.2）。设计 §3.4。
type Storyline struct {
	Title   string     `json:"title"`
	Premise string     `json:"premise,omitempty"` // 主线前提/悬念
	Acts    []StoryAct `json:"acts,omitempty"`
}

// StoryAct 主线一幕。
type StoryAct struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	Status  string `json:"status"` // pending / active / done
}

// QuestState 任务/目标（旧 ObjectiveState 演化）。
type QuestState struct {
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
}

// HiddenItem 玩家可能发现的隐藏信息。
type HiddenItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Source      string `json:"source"` // scene / clue / npc
	Discovered  bool   `json:"discovered"`
}

// ScheduledEvent 待触发事件（旧 PendingEvent 演化，设计文档 6.3）。
type ScheduledEvent struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Trigger     string `json:"trigger,omitempty"`     // 触发条件描述（自然语言）
	TriggerAt   int64  `json:"trigger_at,omitempty"`  // 世界时间触发点（0 = 条件触发）
	Type        string `json:"type"`                  // trigger / encounter / world
	Triggered   bool   `json:"triggered"`
	Resolved    bool   `json:"resolved,omitempty"`    // 已解决（修复旧版混乱度单调累积）
}

// RelationEdge 关系图谱边（设计文档 4.3）。
type RelationEdge struct {
	From  string   `json:"from"`
	To    string   `json:"to"`
	Trust int      `json:"trust"` // -100..100
	Fear  int      `json:"fear"`
	Debt  int      `json:"debt"`
	Tags  []string `json:"tags,omitempty"`
}

// StateLock 状态锁定：关键事实一旦确立不可被 LLM 覆盖。
type StateLock struct {
	Key    string `json:"key"`    // 如 "npc:老陈:dead"
	Reason string `json:"reason"`
	Round  int    `json:"round"`
}

// Fact 双时序事实（设计文档 4.5）：易变事实带有效区间，失效不覆盖。
type Fact struct {
	Subject    string `json:"subject"`
	Predicate  string `json:"predicate"`
	Object     string `json:"object"`
	ValidAt    int64  `json:"valid_at"`
	InvalidAt  int64  `json:"invalid_at,omitempty"` // 0 = 仍有效
	RecordedAt int64  `json:"recorded_at"`
}

// WorldEvent 事件溯源条目，也是 ApplyEvent 的变更请求。
// Type 取值：
//   npc_disposition / hidden_discovered / event_triggered / objective_completed /
//   mood_change / relation_change / quest_advance / lock / unlock /
//   fact_add / fact_invalidate / decision / note
type WorldEvent struct {
	Type     string `json:"type"`
	Actor    string `json:"actor,omitempty"`    // 行为发起者（玩家/NPC/引擎）
	Target   string `json:"target"`             // 目标：NPC名称/线索ID或描述/事件ID或描述/目标描述/Fact subject
	Value    string `json:"value,omitempty"`    // 新值
	CausedBy string `json:"caused_by,omitempty"` // 因果链：上游事件描述
	Round    int    `json:"round"`
	Time     int64  `json:"time"` // 世界时间
}

// Metrics 规则化预评估指标（确定性计算，0-100）。
type Metrics struct {
	TensionLevel      int `json:"tension_level"`
	ChaosLevel        int `json:"chaos_level"`
	PlayerAgency      int `json:"player_agency"`
	ObjectiveProgress int `json:"objective_progress"`
}

// --- 辅助方法 ---

// NewWorldState 创建初始 WorldState。
func NewWorldState(worldID, mode string) *WorldState {
	return &WorldState{
		WorldID:    worldID,
		Mode:       mode,
		Characters: make(map[string]*CharacterState),
		Locations:  make(map[string]*Location),
		Factions:   make(map[string]*Faction),
		Items:      make(map[string]*Item),
		Quests:     []QuestState{},
		HiddenInfo: []HiddenItem{},
		EventQueue: []ScheduledEvent{},
		EventLog:   []WorldEvent{},
		Metrics:    Metrics{PlayerAgency: 50},
	}
}

// IsLocked 检查指定 key 是否被锁定。
func (ws *WorldState) IsLocked(key string) bool {
	for _, l := range ws.Locks {
		if l.Key == key {
			return true
		}
	}
	return false
}

// AddLock 添加状态锁定（幂等）。
func (ws *WorldState) AddLock(key, reason string, round int) {
	if ws.IsLocked(key) {
		return
	}
	ws.Locks = append(ws.Locks, StateLock{Key: key, Reason: reason, Round: round})
}

// CompletedQuestCount 已完成目标数。
func (ws *WorldState) CompletedQuestCount() int {
	count := 0
	for _, q := range ws.Quests {
		if q.Completed {
			count++
		}
	}
	return count
}

// ActiveThreatCount 活跃威胁数（敌对 NPC + 未触发遭遇）。
func (ws *WorldState) ActiveThreatCount() int {
	count := 0
	for _, c := range ws.Characters {
		if c.Disposition == "hostile" && c.Alive {
			count++
		}
	}
	for _, ev := range ws.EventQueue {
		if !ev.Triggered && ev.Type == "encounter" {
			count++
		}
	}
	return count
}

// UndiscoveredCount 未发现的隐藏信息数。
func (ws *WorldState) UndiscoveredCount() int {
	count := 0
	for _, h := range ws.HiddenInfo {
		if !h.Discovered {
			count++
		}
	}
	return count
}

// GetRelation 获取 From->To 的关系边（不存在则创建零值边）。
func (ws *WorldState) GetRelation(from, to string) *RelationEdge {
	for i := range ws.Relations {
		if ws.Relations[i].From == from && ws.Relations[i].To == to {
			return &ws.Relations[i]
		}
	}
	ws.Relations = append(ws.Relations, RelationEdge{From: from, To: to})
	return &ws.Relations[len(ws.Relations)-1]
}
