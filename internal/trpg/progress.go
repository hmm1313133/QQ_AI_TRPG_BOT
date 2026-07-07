// 跑团进度追踪器：管理每个会话的剧情进度、玩家决策历史和 AI 总结。
// 进度数据通过 script.Archive 持久化到本地 JSON，同时通过 OpenViking 同步到 AI 记忆。
package trpg

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/store"
)

// ProgressTracker 管理跑团进度。
type ProgressTracker struct {
	archive     *script.Archive
	openViking  *store.OpenVikingClient
}

// NewProgressTracker 创建进度追踪器。
func NewProgressTracker(archive *script.Archive, openViking *store.OpenVikingClient) *ProgressTracker {
	return &ProgressTracker{
		archive:    archive,
		openViking: openViking,
	}
}

// GetProgress 获取会话的跑团进度，不存在则返回默认值。
func (pt *ProgressTracker) GetProgress(sessionID string) *script.Progress {
	// 先从内存/磁盘加载
	if pt.archive != nil {
		// 尝试加载已有进度
		progress, err := pt.archive.LoadProgress(sessionID)
		if err == nil {
			return progress
		}
	}
	return nil
}

// GetOrCreateProgress 获取或创建进度。
func (pt *ProgressTracker) GetOrCreateProgress(sessionID, scriptID string) (*script.Progress, error) {
	if pt.archive == nil {
		return nil, fmt.Errorf("存档管理器未初始化")
	}

	// 尝试加载已有进度
	progress, err := pt.archive.LoadProgress(sessionID)
	if err == nil {
		return progress, nil
	}

	// 创建新进度
	scr, err := pt.archive.Get(scriptID)
	if err != nil {
		return nil, fmt.Errorf("获取剧本失败: %w", err)
	}

	firstNode := ""
	if node := scr.GetFirstNode(); node != nil {
		firstNode = node.ID
	}

	progress = &script.Progress{
		SessionID:       sessionID,
		ScriptID:        scriptID,
		ScriptName:      scr.Name,
		CurrentNodeID:   firstNode,
		CompletedNodes:  []string{},
		PlayerDecisions: []script.Decision{},
		IsActive:        true,
	}

	if err := pt.archive.SaveProgress(progress); err != nil {
		return nil, fmt.Errorf("保存进度失败: %w", err)
	}

	// 同步到 OpenViking
	if pt.openViking != nil && pt.openViking.IsEnabled() {
		ctx := context.Background()
		_ = pt.openViking.WriteJSON(ctx, fmt.Sprintf("sessions/%s/progress", sessionID), progress)
	}

	log.Printf("[ProgressTracker] 创建进度: 会话=%s, 剧本=%s, 初始节点=%s",
		sessionID, scr.Name, firstNode)

	return progress, nil
}

// AdvanceNode 推进到指定剧情节点。
func (pt *ProgressTracker) AdvanceNode(sessionID, nodeID string) error {
	progress, err := pt.GetOrCreateProgress(sessionID, pt.getScriptID(sessionID))
	if err != nil {
		return err
	}

	// 将当前节点标记为已完成
	if progress.CurrentNodeID != "" && !progress.IsNodeCompleted(progress.CurrentNodeID) {
		progress.CompletedNodes = append(progress.CompletedNodes, progress.CurrentNodeID)
	}

	// 更新当前节点
	progress.CurrentNodeID = nodeID

	// 更新节点名称
	if pt.archive != nil {
		if scr, err := pt.archive.Get(progress.ScriptID); err == nil {
			if node, err := scr.GetNodeByID(nodeID); err == nil {
				progress.CurrentNodeName = node.Name
			}
		}
	}

	progress.LastUpdate = time.Now().Format("2006-01-02 15:04:05")

	if err := pt.archive.SaveProgress(progress); err != nil {
		return fmt.Errorf("保存进度失败: %w", err)
	}

	// 同步到 OpenViking
	if pt.openViking != nil && pt.openViking.IsEnabled() {
		ctx := context.Background()
		_ = pt.openViking.WriteJSON(ctx, fmt.Sprintf("sessions/%s/progress", sessionID), progress)
	}

	log.Printf("[ProgressTracker] 推进节点: 会话=%s, 节点=%s", sessionID, nodeID)
	return nil
}

// RecordDecision 记录玩家决策。
func (pt *ProgressTracker) RecordDecision(sessionID string, decision script.Decision) error {
	progress, err := pt.GetOrCreateProgress(sessionID, pt.getScriptID(sessionID))
	if err != nil {
		return err
	}

	decision.Timestamp = time.Now().Format("15:04:05")
	if decision.NodeID == "" {
		decision.NodeID = progress.CurrentNodeID
	}

	progress.AddDecision(decision)
	progress.LastUpdate = time.Now().Format("2006-01-02 15:04:05")

	if err := pt.archive.SaveProgress(progress); err != nil {
		return fmt.Errorf("保存进度失败: %w", err)
	}

	return nil
}

// UpdateSummary 更新 AI 总结的剧情进度。
func (pt *ProgressTracker) UpdateSummary(sessionID, storySummary, chapterSummary string) error {
	progress, err := pt.GetOrCreateProgress(sessionID, pt.getScriptID(sessionID))
	if err != nil {
		return err
	}

	progress.StorySummary = storySummary
	progress.ChapterSummary = chapterSummary
	progress.LastUpdate = time.Now().Format("2006-01-02 15:04:05")

	if err := pt.archive.SaveProgress(progress); err != nil {
		return fmt.Errorf("保存进度失败: %w", err)
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
	progress := pt.GetProgress(sessionID)
	if progress == nil {
		return ""
	}

	var sb fmt.Stringer
	_ = sb

	result := fmt.Sprintf("【剧本进度】\n")
	result += fmt.Sprintf("剧本: %s\n", progress.ScriptName)
	result += fmt.Sprintf("当前节点: %s (%s)\n", progress.CurrentNodeName, progress.CurrentNodeID)
	result += fmt.Sprintf("已完成节点: %d\n", len(progress.CompletedNodes))

	if progress.StorySummary != "" {
		result += fmt.Sprintf("剧情摘要: %s\n", progress.StorySummary)
	}
	if progress.ChapterSummary != "" {
		result += fmt.Sprintf("当前章节: %s\n", progress.ChapterSummary)
	}

	// 最近 3 条决策
	if len(progress.PlayerDecisions) > 0 {
		result += "最近决策:\n"
		start := len(progress.PlayerDecisions) - 3
		if start < 0 {
			start = 0
		}
		for _, d := range progress.PlayerDecisions[start:] {
			result += fmt.Sprintf("  [%s] %s → %s\n", d.Timestamp, d.Action, d.Outcome)
		}
	}

	return result
}

// ResetProgress 重置会话进度（重新开始剧本）。
func (pt *ProgressTracker) ResetProgress(sessionID, scriptID string) error {
	if pt.archive != nil {
		_ = pt.archive.DeleteProgress(sessionID)
	}
	_, err := pt.GetOrCreateProgress(sessionID, scriptID)
	return err
}

// getScriptID 获取会话当前加载的剧本 ID。
// 通过 trpg.Service 的 Session.State 获取。
func (pt *ProgressTracker) getScriptID(sessionID string) string {
	// 从已有进度读取
	if progress, err := pt.archive.LoadProgress(sessionID); err == nil {
		return progress.ScriptID
	}
	return ""
}

// SetArchive 设置存档管理器（延迟注入用）。
func (pt *ProgressTracker) SetArchive(archive *script.Archive) {
	pt.archive = archive
}
