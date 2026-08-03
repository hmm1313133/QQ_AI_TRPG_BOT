// 旧版数据迁移：将 P1 前的 GameState（data/scripts/gamestate/*.json）
// 与 Progress（data/scripts/progress/*.json）合并迁移为 WorldState。
// 迁移为一次性操作：目标世界文件已存在则跳过。
package world

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// legacyGameState 旧版 GameState（internal/agent/gamestate.go，已删除）。
type legacyGameState struct {
	SessionID     string                    `json:"session_id"`
	ScriptID      string                    `json:"script_id"`
	ScriptName    string                    `json:"script_name"`
	CurrentScene  SceneState                `json:"current_scene"`
	NPCStates     map[string]legacyNPCState `json:"npc_states"`
	HiddenInfo    []HiddenItem              `json:"hidden_info"`
	PendingEvents []legacyPendingEvent      `json:"pending_events"`
	Objectives    []legacyObjective         `json:"objectives"`
	Metrics       Metrics                   `json:"metrics"`
	RoundCount    int                       `json:"round_count"`
	StoryContext  string                    `json:"story_context"`
}

type legacyNPCState struct {
	Name          string   `json:"name"`
	Role          string   `json:"role"`
	Disposition   string   `json:"disposition"`
	Location      string   `json:"location,omitempty"`
	CurrentAction string   `json:"current_action,omitempty"`
	Motivation    string   `json:"motivation,omitempty"`
	Secrets       string   `json:"secrets,omitempty"`
	DialogueStyle string   `json:"dialogue_style,omitempty"`
	KeyDialogue   []string `json:"key_dialogue,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

type legacyPendingEvent struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Trigger     string `json:"trigger,omitempty"`
	Type        string `json:"type"`
	Triggered   bool   `json:"triggered"`
}

type legacyObjective struct {
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
}

// legacyProgress 旧版 Progress 中需要迁移的字段。
type legacyProgress struct {
	SessionID       string   `json:"session_id"`
	CompletedNodes  []string `json:"completed_nodes"`
	StorySummary    string   `json:"story_summary"`
	CurrentNodeID   string   `json:"current_node_id"`
}

// MigrateLegacy 扫描旧数据目录，将旧格式迁移为 WorldState。
// gamestateDir / progressDir 不存在或为空时静默跳过。
func MigrateLegacy(engine *Engine, gamestateDir, progressDir string) {
	entries, err := os.ReadDir(gamestateDir)
	if err != nil {
		return
	}

	migrated := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")

		// 已有新世界文件则跳过
		if engine.LoadOrNil(sessionID) != nil {
			continue
		}

		data, err := os.ReadFile(filepath.Join(gamestateDir, entry.Name()))
		if err != nil {
			continue
		}
		var old legacyGameState
		if err := json.Unmarshal(data, &old); err != nil {
			log.Printf("[Migrate] 跳过无法解析的旧 GameState: %s: %v", entry.Name(), err)
			continue
		}

		ws := convertLegacyGameState(&old)

		// 合并旧 Progress（CompletedNodes / StorySummary）
		if progressDir != "" {
			if pData, err := os.ReadFile(filepath.Join(progressDir, entry.Name())); err == nil {
				var lp legacyProgress
				if err := json.Unmarshal(pData, &lp); err == nil {
					ws.CompletedNodes = lp.CompletedNodes
					if lp.StorySummary != "" {
						ws.CampaignSummary = lp.StorySummary
					}
				}
			}
		}

		if err := engine.Save(ws); err != nil {
			log.Printf("[Migrate] 保存迁移结果失败: %s: %v", sessionID, err)
			continue
		}
		migrated++
		log.Printf("[Migrate] 已迁移会话 %s 的世界状态（场景=%s, NPC=%d, 轮次=%d）",
			sessionID, ws.Scene.NodeName, len(ws.Characters), ws.RoundCount)
	}

	if migrated > 0 {
		log.Printf("[Migrate] 旧数据迁移完成: %d 个会话", migrated)
	}
}

// convertLegacyGameState 将旧 GameState 转换为 WorldState。
func convertLegacyGameState(old *legacyGameState) *WorldState {
	ws := NewWorldState(old.SessionID, ModeTRPG)
	ws.ScriptID = old.ScriptID
	ws.ScriptName = old.ScriptName
	// 旧 StoryContext 混合了背景与摘要，整体放入 Background（不可变部分）
	ws.Background = old.StoryContext
	ws.Scene = old.CurrentScene
	ws.HiddenInfo = old.HiddenInfo
	ws.Metrics = old.Metrics
	ws.RoundCount = old.RoundCount

	for name, npc := range old.NPCStates {
		alive := npc.Disposition != "dead"
		ws.Characters[name] = &CharacterState{
			Name:          npc.Name,
			Kind:          "npc",
			Role:          npc.Role,
			Alive:         alive,
			Disposition:   npc.Disposition,
			Location:      npc.Location,
			CurrentAction: npc.CurrentAction,
			Motivation:    npc.Motivation,
			Secrets:       npc.Secrets,
			DialogueStyle: npc.DialogueStyle,
			KeyDialogue:   npc.KeyDialogue,
			Notes:         npc.Notes,
		}
		if !alive {
			ws.AddLock("npc:"+npc.Name+":dead", "旧数据迁移", old.RoundCount)
		}
	}

	for _, ev := range old.PendingEvents {
		ws.EventQueue = append(ws.EventQueue, ScheduledEvent{
			ID:          ev.ID,
			Description: ev.Description,
			Trigger:     ev.Trigger,
			Type:        ev.Type,
			Triggered:   ev.Triggered,
		})
	}

	for _, obj := range old.Objectives {
		ws.Quests = append(ws.Quests, QuestState{
			Description: obj.Description,
			Completed:   obj.Completed,
		})
	}

	return ws
}
