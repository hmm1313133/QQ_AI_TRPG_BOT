// Package agent - GameState 持久化存储与剧本初始化。
//
// GameStateStore 使用原子写入（tmp+rename）将 GameState 持久化到
// ./data/scripts/gamestate/{sessionID}.json，复用 Archive 的模式。
//
// InitFromScript 从 script.Script 结构初始化全部运行态字段，
// RefreshForNode 在时间轴推进时刷新运行态。
package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
)

// GameStateStore 管理 GameState 的持久化。
type GameStateStore struct {
	mu  sync.RWMutex
	dir string // ./data/scripts/gamestate/
}

// NewGameStateStore 创建 GameState 存储管理器。
func NewGameStateStore(dir string) (*GameStateStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建 gamestate 目录失败: %w", err)
	}
	return &GameStateStore{dir: dir}, nil
}

// statePath 返回指定会话的 GameState 文件路径。
func (s *GameStateStore) statePath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".json")
}

// Load 加载指定会话的 GameState。
func (s *GameStateStore) Load(sessionID string) (*GameState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.statePath(sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 GameState 失败: %w", err)
	}

	var state GameState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("解析 GameState JSON 失败: %w", err)
	}
	return &state, nil
}

// Save 持久化 GameState（原子写入：tmp+rename）。
func (s *GameStateStore) Save(state *GameState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state.SessionID == "" {
		return fmt.Errorf("GameState SessionID 不能为空")
	}

	state.LastUpdate = time.Now().Format("2006-01-02 15:04:05")
	path := s.statePath(state.SessionID)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 GameState 失败: %w", err)
	}

	// 原子写入
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入 GameState 文件失败: %w", err)
	}
	_ = os.Remove(path)
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("重命名 GameState 文件失败: %w", err)
	}

	return nil
}

// Delete 删除指定会话的 GameState。
func (s *GameStateStore) Delete(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.statePath(sessionID)
	_ = os.Remove(path)
	return nil
}

// LoadOrDefault 加载 GameState，不存在则返回 nil。
func (s *GameStateStore) LoadOrDefault(sessionID string) *GameState {
	state, err := s.Load(sessionID)
	if err != nil {
		return nil
	}
	return state
}

// InitFromScript 从分析后的 Script 结构初始化 GameState。
// 映射关系：
//   - CurrentScene  <- Timeline[0] 节点 + 关联 ScriptScene
//   - NPCStates     <- Script.Characters
//   - HiddenInfo    <- ScriptScene.HiddenDetails + TimelineNode.Clues
//   - PendingEvents <- TimelineNode.Triggers + TimelineNode.Encounters
//   - Objectives    <- TimelineNode.Objectives
//   - StoryContext  <- Script.Background
func (s *GameStateStore) InitFromScript(sessionID string, scr *script.Script) (*GameState, error) {
	state := NewGameState(sessionID, scr.ID, scr.Name)

	// StoryContext 从 Background 构建
	state.StoryContext = buildStoryContext(&scr.Background)

	// 从第一个时间轴节点初始化
	firstNode := scr.GetFirstNode()
	if firstNode != nil {
		applyNodeToState(state, scr, firstNode)
	}

	// 初始化 NPCStates
	for _, ch := range scr.Characters {
		state.NPCStates[ch.Name] = NPCState{
			Name:          ch.Name,
			Role:          ch.Role,
			Disposition:   "neutral",
			Motivation:    ch.Motivation,
			Secrets:       ch.Secrets,
			DialogueStyle: ch.DialogueStyle,
			KeyDialogue:   ch.KeyDialogue,
			Notes:         ch.Notes,
		}
	}

	// 计算初始指标
	state.Metrics = GameMetrics{
		TensionLevel:      0,
		ChaosLevel:        0,
		PlayerAgency:      50,
		ObjectiveProgress: 0,
	}

	if err := s.Save(state); err != nil {
		return nil, fmt.Errorf("保存初始 GameState 失败: %w", err)
	}

	log.Printf("[GameStateStore] 初始化 GameState: session=%s, script=%s, scene=%s, npcs=%d",
		sessionID, scr.Name, state.CurrentScene.NodeName, len(state.NPCStates))

	return state, nil
}

// RefreshForNode 推进时间轴节点时刷新运行态。
// 保留 NPCStates（NPC 状态跨节点持续），但刷新场景/目标/隐藏信息/待触发事件。
func (s *GameStateStore) RefreshForNode(sessionID string, scr *script.Script, nodeID string) error {
	state, err := s.Load(sessionID)
	if err != nil {
		return fmt.Errorf("加载 GameState 失败: %w", err)
	}

	node, err := scr.GetNodeByID(nodeID)
	if err != nil {
		return fmt.Errorf("获取节点失败: %w", err)
	}

	// 刷新场景信息
	applyNodeToState(state, scr, node)

	// 保留 NPCStates（跨节点持续），但可以添加新 NPC
	for _, ch := range scr.Characters {
		if _, exists := state.NPCStates[ch.Name]; !exists {
			state.NPCStates[ch.Name] = NPCState{
				Name:          ch.Name,
				Role:          ch.Role,
				Disposition:   "neutral",
				Motivation:    ch.Motivation,
				Secrets:       ch.Secrets,
				DialogueStyle: ch.DialogueStyle,
				KeyDialogue:   ch.KeyDialogue,
				Notes:         ch.Notes,
			}
		}
	}

	if err := s.Save(state); err != nil {
		return fmt.Errorf("保存刷新后的 GameState 失败: %w", err)
	}

	log.Printf("[GameStateStore] 刷新 GameState: session=%s, node=%s, objectives=%d, events=%d",
		sessionID, nodeID, len(state.Objectives), len(state.PendingEvents))

	return nil
}

// applyNodeToState 将时间轴节点的信息应用到 GameState。
// 设置当前场景、目标、隐藏信息、待触发事件。
func applyNodeToState(state *GameState, scr *script.Script, node *script.TimelineNode) {
	// 当前场景
	state.CurrentScene = SceneState{
		NodeID:       node.ID,
		NodeName:     node.Name,
		NodeType:     node.Type,
		Description:  node.Description,
		Narrative:    node.Narrative,
		KPNotes:      node.KPNotes,
	}

	// 查找关联的 ScriptScene（通过 ConnectedNodes 或名称匹配）
	for i := range scr.Scenes {
		sc := &scr.Scenes[i]
		for _, cn := range sc.ConnectedNodes {
			if cn == node.ID {
				state.CurrentScene.Atmosphere = sc.Atmosphere
				state.CurrentScene.DangerLevel = sc.DangerLevel
				state.CurrentScene.InvestigationPts = sc.InvestigationPoints
				state.CurrentScene.Exits = sc.Exits
				if state.CurrentScene.Narrative == "" {
					state.CurrentScene.Narrative = sc.Narrative
				}
				break
			}
		}
	}

	// 重置目标
	state.Objectives = []ObjectiveState{}
	for _, obj := range node.Objectives {
		state.Objectives = append(state.Objectives, ObjectiveState{
			Description: obj,
			Completed:   false,
		})
	}

	// 重置隐藏信息（新节点的线索）
	state.HiddenInfo = []HiddenItem{}
	for idx, clue := range node.Clues {
		state.HiddenInfo = append(state.HiddenInfo, HiddenItem{
			ID:          fmt.Sprintf("%s_clue_%d", node.ID, idx),
			Description: clue,
			Source:      "clue",
			Discovered:  false,
		})
	}

	// 重置待触发事件
	state.PendingEvents = []PendingEvent{}
	for idx, trigger := range node.Triggers {
		state.PendingEvents = append(state.PendingEvents, PendingEvent{
			ID:          fmt.Sprintf("%s_trigger_%d", node.ID, idx),
			Description: trigger,
			Trigger:     trigger,
			Type:        "trigger",
			Triggered:   false,
		})
	}
	for idx, encounter := range node.Encounters {
		state.PendingEvents = append(state.PendingEvents, PendingEvent{
			ID:          fmt.Sprintf("%s_encounter_%d", node.ID, idx),
			Description: encounter,
			Type:        "encounter",
			Triggered:   false,
		})
	}
}

// buildStoryContext 从 StoryBackground 构建叙事上下文文本。
func buildStoryContext(bg *script.StoryBackground) string {
	var sb []byte
	if bg.Setting != "" {
		sb = append(sb, fmt.Sprintf("时代/世界观: %s\n", bg.Setting)...)
	}
	if bg.Era != "" {
		sb = append(sb, fmt.Sprintf("时代: %s\n", bg.Era)...)
	}
	if bg.Location != "" {
		sb = append(sb, fmt.Sprintf("地点: %s\n", bg.Location)...)
	}
	if bg.Atmosphere != "" {
		sb = append(sb, fmt.Sprintf("氛围: %s\n", bg.Atmosphere)...)
	}
	if bg.Tone != "" {
		sb = append(sb, fmt.Sprintf("基调: %s\n", bg.Tone)...)
	}
	if bg.MainTheme != "" {
		sb = append(sb, fmt.Sprintf("主题: %s\n", bg.MainTheme)...)
	}
	if bg.Synopsis != "" {
		sb = append(sb, fmt.Sprintf("剧情梗概: %s\n", bg.Synopsis)...)
	}
	if bg.Backstory != "" {
		sb = append(sb, fmt.Sprintf("背景故事: %s\n", bg.Backstory)...)
	}
	return string(sb)
}
