// 跑团进度追踪器：world.Engine 的兼容门面。
//
// P1 改造后，进度的唯一真相是 WorldState（internal/world），
// 本类型仅将 WorldState 视图合成为旧的 script.Progress 结构，
// 保持 script_tools / timeline / handler 等现有调用方编译兼容。
// 新代码应直接使用 world.Engine。
package trpg

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/store"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// ProgressTracker 管理跑团进度（world.Engine 的门面）。
type ProgressTracker struct {
	archive     *script.Archive
	openViking  *store.OpenVikingClient
	worldEngine *world.Engine
}

// NewProgressTracker 创建进度追踪器。
func NewProgressTracker(archive *script.Archive, openViking *store.OpenVikingClient, worldEngine *world.Engine) *ProgressTracker {
	return &ProgressTracker{
		archive:     archive,
		openViking:  openViking,
		worldEngine: worldEngine,
	}
}

// progressView 将 WorldState 合成为旧 Progress 视图。
func progressView(ws *world.WorldState) *script.Progress {
	if ws == nil {
		return nil
	}
	p := &script.Progress{
		SessionID:       ws.WorldID,
		ScriptID:        ws.ScriptID,
		ScriptName:      ws.ScriptName,
		CurrentNodeID:   ws.Scene.NodeID,
		CurrentNodeName: ws.Scene.NodeName,
		CompletedNodes:  ws.CompletedNodes,
		StorySummary:    ws.CampaignSummary,
		LastUpdate:      ws.LastUpdate,
		IsActive:        true,
		PlayerDecisions: []script.Decision{},
	}
	// 从 EventLog 还原决策记录
	for _, ev := range ws.EventLog {
		if ev.Type != "decision" {
			continue
		}
		action, outcome := ev.Value, ""
		if idx := strings.Index(ev.Value, " → "); idx >= 0 {
			action, outcome = ev.Value[:idx], ev.Value[idx+len(" → "):]
		}
		p.PlayerDecisions = append(p.PlayerDecisions, script.Decision{
			NodeID:  ev.Target,
			Action:  action,
			Outcome: outcome,
		})
	}
	return p
}

// GetProgress 获取会话的跑团进度视图，不存在则返回 nil。
func (pt *ProgressTracker) GetProgress(sessionID string) *script.Progress {
	if pt.worldEngine == nil {
		return nil
	}
	return progressView(pt.worldEngine.LoadOrNil(sessionID))
}

// GetOrCreateProgress 获取或创建进度。
// 已存在同剧本的世界则复用（长团续跑）；剧本不同则重新初始化。
func (pt *ProgressTracker) GetOrCreateProgress(sessionID, scriptID string) (*script.Progress, error) {
	if pt.worldEngine == nil {
		return nil, fmt.Errorf("世界引擎未初始化")
	}

	if ws := pt.worldEngine.LoadOrNil(sessionID); ws != nil && ws.ScriptID == scriptID {
		return progressView(ws), nil
	}

	if pt.archive == nil {
		return nil, fmt.Errorf("存档管理器未初始化")
	}
	scr, err := pt.archive.Get(scriptID)
	if err != nil {
		return nil, fmt.Errorf("获取剧本失败: %w", err)
	}

	ws, err := pt.worldEngine.InitFromScript(sessionID, scr)
	if err != nil {
		return nil, fmt.Errorf("初始化世界状态失败: %w", err)
	}

	// 同步到 OpenViking
	if pt.openViking != nil && pt.openViking.IsEnabled() {
		ctx := context.Background()
		_ = pt.openViking.WriteJSON(ctx, fmt.Sprintf("sessions/%s/progress", sessionID), progressView(ws))
	}

	log.Printf("[ProgressTracker] 创建进度: 会话=%s, 剧本=%s, 初始节点=%s",
		sessionID, scr.Name, ws.Scene.NodeID)

	return progressView(ws), nil
}

// AdvanceNode 推进到指定剧情节点。
func (pt *ProgressTracker) AdvanceNode(sessionID, nodeID string) error {
	if pt.worldEngine == nil {
		return fmt.Errorf("世界引擎未初始化")
	}
	ws := pt.worldEngine.LoadOrNil(sessionID)
	if ws == nil {
		return fmt.Errorf("未找到世界状态，请先加载剧本")
	}
	if pt.archive == nil {
		return fmt.Errorf("存档管理器未初始化")
	}
	scr, err := pt.archive.Get(ws.ScriptID)
	if err != nil {
		return fmt.Errorf("获取剧本失败: %w", err)
	}
	if err := pt.worldEngine.RefreshScene(sessionID, scr, nodeID); err != nil {
		return err
	}

	// 同步到 OpenViking
	if pt.openViking != nil && pt.openViking.IsEnabled() {
		ctx := context.Background()
		_ = pt.openViking.WriteJSON(ctx, fmt.Sprintf("sessions/%s/progress", sessionID), pt.GetProgress(sessionID))
	}

	log.Printf("[ProgressTracker] 推进节点: 会话=%s, 节点=%s", sessionID, nodeID)
	return nil
}

// RecordDecision 记录玩家决策。
func (pt *ProgressTracker) RecordDecision(sessionID string, decision script.Decision) error {
	if pt.worldEngine == nil {
		return fmt.Errorf("世界引擎未初始化")
	}
	return pt.worldEngine.RecordDecision(sessionID, decision.Action, decision.Outcome)
}

// UpdateSummary 更新 AI 总结的剧情进度。
func (pt *ProgressTracker) UpdateSummary(sessionID, storySummary, chapterSummary string) error {
	if pt.worldEngine == nil {
		return fmt.Errorf("世界引擎未初始化")
	}
	if err := pt.worldEngine.UpdateSummary(sessionID, storySummary); err != nil {
		return err
	}

	// 同步到 OpenViking 记忆
	if pt.openViking != nil && pt.openViking.IsEnabled() {
		ctx := context.Background()
		_ = pt.openViking.UpdateMemory(ctx, sessionID, "story_summary", storySummary)
		_ = pt.openViking.UpdateMemory(ctx, sessionID, "chapter_summary", chapterSummary)
	}

	return nil
}

// GetContextForKP 构建供 KP Agent 使用的剧情上下文文本。
func (pt *ProgressTracker) GetContextForKP(sessionID string) string {
	if pt.worldEngine == nil {
		return ""
	}
	return pt.worldEngine.GetProgressContext(pt.worldEngine.LoadOrNil(sessionID))
}

// ResetProgress 重置会话进度（重新开始剧本）。
func (pt *ProgressTracker) ResetProgress(sessionID, scriptID string) error {
	if pt.worldEngine != nil {
		_ = pt.worldEngine.Delete(sessionID)
	}
	_, err := pt.GetOrCreateProgress(sessionID, scriptID)
	return err
}

// SetArchive 设置存档管理器（延迟注入用）。
func (pt *ProgressTracker) SetArchive(archive *script.Archive) {
	pt.archive = archive
}
