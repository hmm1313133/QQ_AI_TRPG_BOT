// Package agent - Director/Planner 决策指令类型定义。
// 状态类型已迁移至 internal/world（WorldState），
// 本文件仅保留决策指令（directive）相关类型。
package agent

// DecisionDirective 是 Planner/Director 输出的结构化决策指令，约束 Narrator。
type DecisionDirective struct {
	Assessment     SceneAssessment  `json:"assessment"`
	NarrationGuide NarrationGuide   `json:"narration_guide"`
	Actions        []DirectorAction `json:"actions"`
	StateUpdates   []StateUpdate    `json:"state_updates"`
	Reasoning      string           `json:"reasoning"`
}

// SceneAssessment 是对当前场景的评估。
type SceneAssessment struct {
	TensionSummary   string `json:"tension_summary"`
	ChaosSummary     string `json:"chaos_summary"`
	AgencySummary    string `json:"agency_summary"`
	ProgressSummary  string `json:"progress_summary"`
	OverallSituation string `json:"overall_situation"`
}

// NarrationGuide 指导 Narrator 如何叙事。
type NarrationGuide struct {
	Tone        string `json:"tone"`         // 叙事基调
	Pacing      string `json:"pacing"`       // 节奏: slow / medium / fast
	FocusPoints string `json:"focus_points"` // 本轮叙事重点
	NPCBehavior string `json:"npc_behavior"` // NPC 行为指导
	Constraints string `json:"constraints"`  // 约束条件（不可违背的设定等）
}

// DirectorAction 是规划动作指令。
type DirectorAction struct {
	Type        string `json:"type"` // advance_timeline / trigger_event / introduce_npc / add_clue / adjust_difficulty
	Description string `json:"description"`
	Target      string `json:"target,omitempty"`
}

// StateUpdate 是要求应用的状态变更。
// Target 支持 NPC 名称 / HiddenItem ID 或线索描述 / PendingEvent ID 或事件描述 / Objective 描述。
type StateUpdate struct {
	Type  string `json:"type"` // npc_disposition / hidden_discovered / event_triggered / objective_completed
	Target string `json:"target"`
	Value string `json:"value"`
}
