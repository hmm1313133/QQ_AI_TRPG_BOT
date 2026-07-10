// Package agent - AI KP 多层架构：结构化运行态定义。
//
// GameState 是每个会话的微观运行态，独立于 ProgressTracker（宏观进度）。
// Director 读取 GameState 做决策，Narrator 读取 GameState 做叙事，
// 每轮结束后更新并持久化 GameState，保证跨轮次一致性。
//
// 初始化时从 script.Script 结构映射：
//   - CurrentScene  <- Timeline 节点 + 关联 ScriptScene
//   - NPCStates     <- Script.Characters
//   - HiddenInfo    <- ScriptScene.HiddenDetails + TimelineNode.Clues
//   - PendingEvents <- TimelineNode.Triggers + TimelineNode.Encounters
//   - Objectives    <- TimelineNode.Objectives
//   - StoryContext  <- Script.Background
package agent

import "fmt"

// GameState 是一个会话的结构化运行态，持久化到 JSON。
type GameState struct {
	SessionID     string              `json:"session_id"`
	ScriptID      string              `json:"script_id"`
	ScriptName    string              `json:"script_name"`
	CurrentScene  SceneState          `json:"current_scene"`
	NPCStates     map[string]NPCState `json:"npc_states"`
	HiddenInfo    []HiddenItem        `json:"hidden_info"`
	PendingEvents []PendingEvent      `json:"pending_events"`
	Objectives    []ObjectiveState    `json:"objectives"`
	Metrics       GameMetrics         `json:"metrics"`
	RoundCount    int                 `json:"round_count"`
	LastDirective *DecisionDirective  `json:"last_directive,omitempty"`
	StoryContext  string              `json:"story_context"`
	LastUpdate    string              `json:"last_update"`
}

// SceneState 描述当前场景的运行态。
type SceneState struct {
	NodeID            string   `json:"node_id"`
	NodeName          string   `json:"node_name"`
	NodeType          string   `json:"node_type"`
	Description       string   `json:"description"`
	Narrative         string   `json:"narrative,omitempty"`
	Atmosphere        string   `json:"atmosphere,omitempty"`
	DangerLevel       string   `json:"danger_level,omitempty"`
	InvestigationPts  []string `json:"investigation_points,omitempty"`
	Exits             []string `json:"exits,omitempty"`
	KPNotes           string   `json:"kp_notes,omitempty"`
}

// NPCState 描述一个 NPC 的实时运行态。
type NPCState struct {
	Name          string   `json:"name"`
	Role          string   `json:"role"`
	Disposition   string   `json:"disposition"`    // friendly / neutral / suspicious / hostile / dead
	Location      string   `json:"location,omitempty"`
	CurrentAction string   `json:"current_action,omitempty"`
	Motivation    string   `json:"motivation,omitempty"`
	Secrets       string   `json:"secrets,omitempty"`
	DialogueStyle string   `json:"dialogue_style,omitempty"`
	KeyDialogue   []string `json:"key_dialogue,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

// HiddenItem 是玩家可能发现的隐藏信息。
type HiddenItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Source      string `json:"source"`     // scene / clue / npc
	Discovered  bool   `json:"discovered"`
}

// PendingEvent 是等待触发的剧情事件。
type PendingEvent struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Trigger     string `json:"trigger,omitempty"` // 触发条件描述
	Type        string `json:"type"`              // trigger / encounter
	Triggered   bool   `json:"triggered"`
}

// ObjectiveState 是当前节点的目标状态。
type ObjectiveState struct {
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
}

// GameMetrics 是规则化预评估指标（确定性计算，0-100）。
type GameMetrics struct {
	TensionLevel      int `json:"tension_level"`       // 场景激烈程度
	ChaosLevel        int `json:"chaos_level"`         // 局势失控距离
	PlayerAgency      int `json:"player_agency"`       // 玩家掌控权
	ObjectiveProgress int `json:"objective_progress"`  // 目标推进程度
}

// DecisionDirective 是 Director 输出的结构化决策指令，约束 Narrator。
type DecisionDirective struct {
	Assessment     SceneAssessment  `json:"assessment"`
	NarrationGuide NarrationGuide   `json:"narration_guide"`
	Actions        []DirectorAction `json:"actions"`
	StateUpdates   []StateUpdate    `json:"state_updates"`
	Reasoning      string           `json:"reasoning"`
}

// SceneAssessment 是 Director 对当前场景的评估。
type SceneAssessment struct {
	TensionSummary    string `json:"tension_summary"`
	ChaosSummary      string `json:"chaos_summary"`
	AgencySummary     string `json:"agency_summary"`
	ProgressSummary   string `json:"progress_summary"`
	OverallSituation  string `json:"overall_situation"`
}

// NarrationGuide 指导 Narrator 如何叙事。
type NarrationGuide struct {
	Tone         string `json:"tone"`          // 叙事基调
	Pacing       string `json:"pacing"`        // 节奏: slow / medium / fast
	FocusPoints  string `json:"focus_points"`  // 本轮叙事重点
	NPCBehavior  string `json:"npc_behavior"`  // NPC 行为指导
	Constraints  string `json:"constraints"`   // 约束条件（不可违背的设定等）
}

// DirectorAction 是导演系统的动作指令。
type DirectorAction struct {
	Type        string `json:"type"`         // advance_timeline / trigger_event / introduce_npc / add_clue / adjust_difficulty
	Description string `json:"description"`
	Target      string `json:"target,omitempty"` // 目标 NPC/事件/节点 ID
}

// StateUpdate 是导演系统要求应用的状态变更。
type StateUpdate struct {
	Type     string `json:"type"`     // npc_disposition / hidden_discovered / event_triggered / objective_completed / scene_change
	Target   string `json:"target"`   // 目标 NPC 名称 / HiddenItem ID / PendingEvent ID / Objective 描述
	Value    string `json:"value"`    // 新值
}

// --- 辅助方法 ---

// NewGameState 创建初始 GameState。
func NewGameState(sessionID, scriptID, scriptName string) *GameState {
	return &GameState{
		SessionID:     sessionID,
		ScriptID:      scriptID,
		ScriptName:    scriptName,
		NPCStates:     make(map[string]NPCState),
		HiddenInfo:    []HiddenItem{},
		PendingEvents: []PendingEvent{},
		Objectives:    []ObjectiveState{},
		RoundCount:    0,
	}
}

// String 返回 GameState 的简要描述（用于日志）。
func (gs *GameState) String() string {
	return fmt.Sprintf("GameState{session=%s, script=%s, scene=%s, npcs=%d, hidden=%d, events=%d, objectives=%d, round=%d, metrics=T%d/C%d/A%d/P%d}",
		gs.SessionID, gs.ScriptName, gs.CurrentScene.NodeName,
		len(gs.NPCStates), len(gs.HiddenInfo), len(gs.PendingEvents), len(gs.Objectives),
		gs.RoundCount, gs.Metrics.TensionLevel, gs.Metrics.ChaosLevel,
		gs.Metrics.PlayerAgency, gs.Metrics.ObjectiveProgress)
}

// ApplyUpdate 应用一个 StateUpdate 到 GameState。
func (gs *GameState) ApplyUpdate(update StateUpdate) {
	switch update.Type {
	case "npc_disposition":
		if npc, ok := gs.NPCStates[update.Target]; ok {
			npc.Disposition = update.Value
			gs.NPCStates[update.Target] = npc
		}
	case "hidden_discovered":
		for i := range gs.HiddenInfo {
			if gs.HiddenInfo[i].ID == update.Target {
				gs.HiddenInfo[i].Discovered = true
				break
			}
		}
	case "event_triggered":
		for i := range gs.PendingEvents {
			if gs.PendingEvents[i].ID == update.Target {
				gs.PendingEvents[i].Triggered = true
				break
			}
		}
	case "objective_completed":
		for i := range gs.Objectives {
			if gs.Objectives[i].Description == update.Target {
				gs.Objectives[i].Completed = true
				break
			}
		}
	}
}

// ApplyUpdates 批量应用 StateUpdates。
func (gs *GameState) ApplyUpdates(updates []StateUpdate) {
	for _, u := range updates {
		gs.ApplyUpdate(u)
	}
}

// CompletedObjectiveCount 返回已完成目标数。
func (gs *GameState) CompletedObjectiveCount() int {
	count := 0
	for _, o := range gs.Objectives {
		if o.Completed {
			count++
		}
	}
	return count
}

// ActiveThreatCount 返回活跃威胁数（敌对 NPC 数量 + 未触发的遭遇事件数）。
func (gs *GameState) ActiveThreatCount() int {
	count := 0
	for _, npc := range gs.NPCStates {
		if npc.Disposition == "hostile" {
			count++
		}
	}
	for _, ev := range gs.PendingEvents {
		if !ev.Triggered && ev.Type == "encounter" {
			count++
		}
	}
	return count
}

// UndiscoveredCount 返回未发现的隐藏信息数。
func (gs *GameState) UndiscoveredCount() int {
	count := 0
	for _, h := range gs.HiddenInfo {
		if !h.Discovered {
			count++
		}
	}
	return count
}
