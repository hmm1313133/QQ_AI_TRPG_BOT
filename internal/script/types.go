// Package script 提供剧本识别、解析与存档管理能力。
//
// 核心数据结构：
//   - Script: 识别后的完整剧本
//   - StoryBackground: 故事背景与世界观
//   - TimelineNode: 剧情时间轴节点
//   - ScriptCharacter: 剧本角色（半完整属性）
//   - ScriptScene: 剧本场景
//   - Progress: 跑团进度
//   - Decision: 玩家决策记录
package script

import "fmt"

// Script 代表一个识别后的完整剧本。
// 由 ScriptAnalyzer Agent 从 PDF/Word 文件提取生成，
// 以 JSON 文件形式持久化到 ./data/scripts/ 目录。
type Script struct {
	ID         string            `json:"id"`           // 唯一标识（文件名哈希或自定义）
	Name       string            `json:"name"`         // 简短名称（用于指令引用）
	Title      string            `json:"title"`        // 完整标题
	System     string            `json:"system"`       // 适用规则集: coc7 / dnd5e
	Background StoryBackground   `json:"background"`   // 故事背景与世界观
	Timeline   []TimelineNode    `json:"timeline"`     // 剧情时间轴
	Characters []ScriptCharacter `json:"characters"`   // 登场角色
	Scenes     []ScriptScene     `json:"scenes"`       // 关键场景
	FilePath   string            `json:"-"`            // 存储路径（不序列化）
	CreatedAt  string            `json:"created_at"`   // 创建时间
	SourceFile string            `json:"source_file"`  // 原始文件名
}

// StoryBackground 故事背景与世界观设定。
type StoryBackground struct {
	Setting      string   `json:"setting"`              // 时代/地点/世界观概述
	Era          string   `json:"era"`                  // 具体时代（如1920年代、中世纪等）
	Location     string   `json:"location"`             // 主要地点
	Atmosphere   string   `json:"atmosphere"`           // 氛围描述
	MainTheme    string   `json:"main_theme"`           // 主题（如恐怖、冒险、悬疑）
	Synopsis     string   `json:"synopsis"`             // 故事梗概
	KeyOrganizations []string `json:"key_organizations"` // 关键组织/势力
	KeyThemes    []string `json:"key_themes,omitempty"`    // 核心冲突/关键主题列表
	Tone         string   `json:"tone,omitempty"`          // 叙事基调（如：压抑绝望、轻松冒险）
	Backstory    string   `json:"backstory,omitempty"`     // 详细历史背景（可直接用于跑团开场叙述）
	VerbatimExcerpts []VerbatimExcerpt `json:"verbatim_excerpts,omitempty"` // 需逐字保留的原文摘录（信件、日记、文献等）
}

// TimelineNode 剧情时间轴节点。
// 节点按 Order 顺序排列，关键节点（IsKeyNode）由事件驱动推进，
// 普通节点可由定时器触发提示。
type TimelineNode struct {
	ID           string   `json:"id"`             // 节点唯一标识（如 node_1, node_2）
	Name         string   `json:"name"`           // 节点名称
	Description  string   `json:"description"`    // 节点详细描述（场景、事件、可发生的事情，尽量保留原文细节）
	Type         string   `json:"type"`           // "act"（幕）/ "scene"（场景）/ "event"（事件）
	Order        int      `json:"order"`          // 顺序
	Triggers     []string `json:"triggers"`       // 触发条件（自然语言描述）
	Consequences []string `json:"consequences"`   // 可能后果
	IsKeyNode    bool     `json:"is_key_node"`    // 关键节点（事件驱动推进）
	NPCs         []string `json:"npcs,omitempty"` // 涉及的NPC名称
	// 以下为导演剧本增强字段
	Narrative    string   `json:"narrative,omitempty"`    // 叙述/旁白文本（可直接朗读给玩家的沉浸式描述）
	Clues        []string `json:"clues,omitempty"`        // 可发现的线索/证据/手记（含发现方式）
	Encounters   []string `json:"encounters,omitempty"`   // 可能的遭遇/事件（含触发条件和应对方式）
	Objectives   []string `json:"objectives,omitempty"`   // 玩家在此节点的目标/任务
	Branches     []string `json:"branches,omitempty"`     // 分支路径描述（不同选择导致的不同走向）
	KPNotes      string   `json:"kp_notes,omitempty"`     // KP 导演备注（节奏控制、重点提示、注意事项）
	VerbatimExcerpts []VerbatimExcerpt `json:"verbatim_excerpts,omitempty"` // 需逐字保留的原文摘录（信件、日记、文献等）
}

// ScriptCharacter 剧本角色（半完整属性）。
// 核心属性根据规则集生成，部分属性留空（值为0）可后续补充。
// NPC 角色卡将同步创建到 character.Manager，Player 字段设为 "npc:{scriptID}"。
type ScriptCharacter struct {
	ID            string         `json:"id"`            // 角色唯一标识
	Name          string         `json:"name"`          // 角色名
	Role          string         `json:"role"`          // protagonist（主角）/ antagonist（反派）/ npc（NPC）
	Personality   string         `json:"personality"`   // 性格描述（供 AI 扮演参考，要具体）
	Background    string         `json:"background"`    // 背景故事
	Attrs         map[string]int `json:"attrs"`         // 核心属性（半完整，部分留空为0）
	Skills        map[string]int `json:"skills"`        // 关键技能（3-5个）
	Notes         string         `json:"notes"`         // 备注（如关系、动机等）
	// 以下为导演剧本增强字段
	Motivation    string         `json:"motivation,omitempty"`    // 角色动机/目的（驱动其行为的核心原因）
	Secrets       string         `json:"secrets,omitempty"`       // 秘密/隐藏信息（玩家可发现但角色不会主动透露）
	DialogueStyle string         `json:"dialogue_style,omitempty"` // 对话风格/语言习惯（如：说话结巴、爱用反问）
	KeyDialogue   []string       `json:"key_dialogue,omitempty"`   // 关键台词/必须说出的信息（供 KP 扮演时参考）
	Relationships string         `json:"relationships,omitempty"` // 与其他角色的关系详述
	Appearance    string         `json:"appearance,omitempty"`    // 外貌描述（可供 KP 描述角色出场）
}

// ScriptScene 剧本关键场景。
type ScriptScene struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	OnEnter            string   `json:"on_enter"`      // 进入场景时的描述（可直接朗读给玩家）
	Exits              []string `json:"exits"`         // 可前往的场景或节点ID
	Atmosphere         string   `json:"atmosphere"`    // 场景氛围
	// 以下为导演剧本增强字段
	InvestigationPoints []string `json:"investigation_points,omitempty"` // 可调查的点（如：书架上的日记、墙上的血迹）
	Narrative           string   `json:"narrative,omitempty"`            // 场景旁白/环境叙述文本
	DangerLevel         string   `json:"danger_level,omitempty"`         // 危险等级（如：安全/紧张/危险/致命）
	ConnectedNodes      []string `json:"connected_nodes,omitempty"`      // 关联的时间轴节点ID
	HiddenDetails       []string `json:"hidden_details,omitempty"`       // 隐藏细节（需要特定技能或道具才能发现）
}

// Progress 跑团进度。
// 每个会话（SessionID）对应一个进度记录，追踪当前剧情节点和玩家决策历史。
type Progress struct {
	SessionID       string     `json:"session_id"`        // 会话ID
	ScriptID        string     `json:"script_id"`         // 剧本ID
	ScriptName      string     `json:"script_name"`       // 剧本名称
	CurrentNodeID   string     `json:"current_node_id"`   // 当前剧情节点ID
	CurrentNodeName string     `json:"current_node_name"` // 当前剧情节点名称
	CompletedNodes  []string   `json:"completed_nodes"`   // 已完成节点ID列表
	PlayerDecisions []Decision `json:"player_decisions"`  // 玩家决策历史
	StorySummary    string     `json:"story_summary"`     // AI总结的当前剧情进度
	ChapterSummary  string     `json:"chapter_summary"`   // 当前章节摘要
	LastUpdate      string     `json:"last_update"`       // 最后更新时间
	IsActive        bool       `json:"is_active"`         // 是否活跃
	FilePath        string     `json:"-"`                 // 存储路径（不序列化）
}

// Decision 玩家决策记录。
type Decision struct {
	Timestamp  string `json:"timestamp"`   // 决策时间
	NodeID     string `json:"node_id"`     // 所在剧情节点
	Action     string `json:"action"`      // 玩家行动描述
	Outcome    string `json:"outcome"`     // 结果描述
	DiceRoll   string `json:"dice_roll,omitempty"`  // 相关骰点结果（如有）
}

// VerbatimExcerpt 是需逐字保留的原文摘录（信件、日记、文献、台词等）。
// 由 Planner 识别关键内容后，程序化提取原文对应行号范围，确保内容完整不丢失。
type VerbatimExcerpt struct {
	Description string `json:"description"`          // 内容描述（如：宾夏的信件全文）
	Module      string `json:"module"`               // 所属模块 background/timeline/characters/scenes
	Content     string `json:"content"`              // 逐字保留的原文内容
	SourceLine  int    `json:"source_line"`          // 起始行号
}

// --- 辅助方法 ---

// GetNodeByID 根据ID查找时间轴节点。
func (s *Script) GetNodeByID(nodeID string) (*TimelineNode, error) {
	for i := range s.Timeline {
		if s.Timeline[i].ID == nodeID {
			return &s.Timeline[i], nil
		}
	}
	return nil, fmt.Errorf("节点 %s 不存在", nodeID)
}

// GetNextNode 返回当前节点的下一个时间轴节点，若已是末尾则返回 nil。
func (s *Script) GetNextNode(currentNodeID string) (*TimelineNode, error) {
	current, err := s.GetNodeByID(currentNodeID)
	if err != nil {
		return nil, err
	}
	for i := range s.Timeline {
		if s.Timeline[i].Order > current.Order {
			return &s.Timeline[i], nil
		}
	}
	return nil, nil // 已是末尾节点
}

// GetFirstNode 返回时间轴的第一个节点。
func (s *Script) GetFirstNode() *TimelineNode {
	if len(s.Timeline) == 0 {
		return nil
	}
	return &s.Timeline[0]
}

// GetCharacterByName 根据名称查找角色。
func (s *Script) GetCharacterByName(name string) (*ScriptCharacter, error) {
	for i := range s.Characters {
		if s.Characters[i].Name == name {
			return &s.Characters[i], nil
		}
	}
	return nil, fmt.Errorf("角色 %s 不存在", name)
}

// IsLastNode 判断是否为最后一个时间轴节点。
func (s *Script) IsLastNode(nodeID string) bool {
	if len(s.Timeline) == 0 {
		return true
	}
	lastNode := &s.Timeline[len(s.Timeline)-1]
	return lastNode.ID == nodeID
}

// TotalNodes 返回时间轴节点总数。
func (s *Script) TotalNodes() int {
	return len(s.Timeline)
}

// CompletedCount 返回已完成节点数量。
func (p *Progress) CompletedCount() int {
	return len(p.CompletedNodes)
}

// AddDecision 添加一条玩家决策记录。
func (p *Progress) AddDecision(decision Decision) {
	p.PlayerDecisions = append(p.PlayerDecisions, decision)
}

// IsNodeCompleted 判断指定节点是否已完成。
func (p *Progress) IsNodeCompleted(nodeID string) bool {
	for _, id := range p.CompletedNodes {
		if id == nodeID {
			return true
		}
	}
	return false
}
