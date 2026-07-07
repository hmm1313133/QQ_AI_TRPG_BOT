// 时间轴推进引擎：定时器 + 事件驱动混合机制。
//
// 事件驱动：KPAgent 通过 advance_timeline 工具在关键节点推进；
//           玩家达成触发条件时自动推进。
// 定时器：每个会话维护一个 time.Ticker（可配置间隔，默认 15 分钟），
//         长时间无进展时触发提示性消息，由 KPAgent 生成剧情推进暗示。
//
// 定时器仅在 TRPG 模式 + 剧本已加载时启动，模式切换或剧本卸载时停止。
package trpg

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
)

// TimelineConfig 时间轴引擎配置。
type TimelineConfig struct {
	IdleInterval int // 无进展定时器间隔（分钟），默认 15
	MaxIdleCount int // 最大无进展次数后强制推进提示，默认 3
}

// TimelineEngine 时间轴推进引擎。
type TimelineEngine struct {
	mu              sync.RWMutex
	config          TimelineConfig
	tickers         map[string]*sessionTicker // sessionID -> ticker
	progressTracker *ProgressTracker
	sessionMgr      *core.SessionManager
}

// sessionTicker 单个会话的定时器。
type sessionTicker struct {
	ticker    *time.Ticker
	cancel    context.CancelFunc
	idleCount int // 无进展计数
	lastNode  string // 上次检查时的节点
}

// NewTimelineEngine 创建时间轴推进引擎。
func NewTimelineEngine(cfg TimelineConfig, tracker *ProgressTracker, sessionMgr *core.SessionManager) *TimelineEngine {
	if cfg.IdleInterval <= 0 {
		cfg.IdleInterval = 15
	}
	if cfg.MaxIdleCount <= 0 {
		cfg.MaxIdleCount = 3
	}

	return &TimelineEngine{
		config:          cfg,
		tickers:         make(map[string]*sessionTicker),
		progressTracker: tracker,
		sessionMgr:      sessionMgr,
	}
}

// OnIdleCallback 定时器触发时的回调函数类型。
// 返回需要发送给会话的提示消息，空字符串表示不发送。
type OnIdleCallback func(sessionID string, progress *script.Progress, idleCount int) string

// Start 为指定会话启动定时器。
func (e *TimelineEngine) Start(sessionID string, callback OnIdleCallback) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 如果已有定时器在运行，先停止
	if t, ok := e.tickers[sessionID]; ok {
		t.cancel()
		t.ticker.Stop()
	}

	ctx, cancel := context.WithCancel(context.Background())
	ticker := time.NewTicker(time.Duration(e.config.IdleInterval) * time.Minute)

	st := &sessionTicker{
		ticker: ticker,
		cancel: cancel,
	}

	e.tickers[sessionID] = st

	// 记录初始节点
	if e.progressTracker != nil {
		if progress := e.progressTracker.GetProgress(sessionID); progress != nil {
			st.lastNode = progress.CurrentNodeID
		}
	}

	go e.run(ctx, sessionID, st, callback)

	log.Printf("[TimelineEngine] 启动定时器: 会话=%s, 间隔=%d分钟", sessionID, e.config.IdleInterval)
}

// Stop 停止指定会话的定时器。
func (e *TimelineEngine) Stop(sessionID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if t, ok := e.tickers[sessionID]; ok {
		t.cancel()
		t.ticker.Stop()
		delete(e.tickers, sessionID)
		log.Printf("[TimelineEngine] 停止定时器: 会话=%s", sessionID)
	}
}

// StopAll 停止所有定时器。
func (e *TimelineEngine) StopAll() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for sessionID, t := range e.tickers {
		t.cancel()
		t.ticker.Stop()
		delete(e.tickers, sessionID)
	}
	log.Printf("[TimelineEngine] 已停止所有定时器")
}

// IsRunning 检查指定会话的定时器是否在运行。
func (e *TimelineEngine) IsRunning(sessionID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.tickers[sessionID]
	return ok
}

// ResetIdleCount 重置会话的无进展计数。
// 玩家有行动或剧情推进时调用。
func (e *TimelineEngine) ResetIdleCount(sessionID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if t, ok := e.tickers[sessionID]; ok {
		t.idleCount = 0
		if e.progressTracker != nil {
			if progress := e.progressTracker.GetProgress(sessionID); progress != nil {
				t.lastNode = progress.CurrentNodeID
			}
		}
	}
}

// run 定时器主循环。
func (e *TimelineEngine) run(ctx context.Context, sessionID string, st *sessionTicker, callback OnIdleCallback) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-st.ticker.C:
			e.checkAndNotify(sessionID, st, callback)
		}
	}
}

// checkAndNotify 检查进度并触发提示。
func (e *TimelineEngine) checkAndNotify(sessionID string, st *sessionTicker, callback OnIdleCallback) {
	if e.progressTracker == nil {
		return
	}

	progress := e.progressTracker.GetProgress(sessionID)
	if progress == nil || !progress.IsActive {
		return
	}

	// 检查是否有进展（节点是否变化）
	if progress.CurrentNodeID == st.lastNode {
		st.idleCount++
	} else {
		st.idleCount = 0
		st.lastNode = progress.CurrentNodeID
	}

	// 达到最大无进展次数，触发提示
	if st.idleCount >= e.config.MaxIdleCount {
		log.Printf("[TimelineEngine] 会话 %s 无进展 %d 次，触发提示", sessionID, st.idleCount)
		if callback != nil {
			msg := callback(sessionID, progress, st.idleCount)
			if msg != "" {
				// 通过 session 发送提示消息
				e.sendIdlePrompt(sessionID, msg)
			}
		}
		st.idleCount = 0 // 重置计数，等待下一轮
	}
}

// sendIdlePrompt 通过会话状态传递提示消息。
// 实际发送由 Bot 层在检测到 session 中的 "timeline_prompt" 时执行。
func (e *TimelineEngine) sendIdlePrompt(sessionID, msg string) {
	if e.sessionMgr != nil {
		session := e.sessionMgr.GetSession(sessionID)
		session.Set("timeline_prompt", msg)
		session.Set("timeline_prompt_time", time.Now().Format("15:04:05"))
	}
}

// CheckTriggers 检查玩家行为是否触发了时间轴推进条件。
// 返回应该推进到的节点 ID（空字符串表示不推进）。
func (e *TimelineEngine) CheckTriggers(sessionID, playerAction string) string {
	if e.progressTracker == nil || e.progressTracker.archive == nil {
		return ""
	}

	progress := e.progressTracker.GetProgress(sessionID)
	if progress == nil {
		return ""
	}

	scr, err := e.progressTracker.archive.Get(progress.ScriptID)
	if err != nil {
		return ""
	}

	// 获取当前节点
	currentNode, err := scr.GetNodeByID(progress.CurrentNodeID)
	if err != nil {
		return ""
	}

	// 检查当前节点的触发条件
	// 这里仅做简单的关键词匹配，实际的语义理解由 AI Agent 完成
	actionLower := toLower(playerAction)
	for _, trigger := range currentNode.Triggers {
		triggerLower := toLower(trigger)
		if containsAny(actionLower, triggerLower) {
			// 触发条件匹配，返回下一节点
			nextNode, err := scr.GetNextNode(currentNode.ID)
			if err != nil || nextNode == nil {
				return ""
			}
			log.Printf("[TimelineEngine] 事件触发推进: 会话=%s, %s -> %s",
				sessionID, currentNode.ID, nextNode.ID)
			return nextNode.ID
		}
	}

	return ""
}

// GetTimelineStatus 返回会话的时间轴状态描述。
func (e *TimelineEngine) GetTimelineStatus(sessionID string) string {
	if e.progressTracker == nil || e.progressTracker.archive == nil {
		return "时间轴引擎未初始化"
	}

	progress := e.progressTracker.GetProgress(sessionID)
	if progress == nil {
		return "未加载剧本"
	}

	scr, err := e.progressTracker.archive.Get(progress.ScriptID)
	if err != nil {
		return fmt.Sprintf("剧本 %s 不可用", progress.ScriptName)
	}

	result := fmt.Sprintf("剧本: %s (%s)\n", scr.Title, scr.System)
	result += fmt.Sprintf("总节点数: %d\n", scr.TotalNodes())
	result += fmt.Sprintf("已完成: %d\n", progress.CompletedCount())

	// 显示时间轴
	for i, node := range scr.Timeline {
		status := "⬜"
		if progress.IsNodeCompleted(node.ID) {
			status = "✅"
		}
		if node.ID == progress.CurrentNodeID {
			status = "👉"
		}
		result += fmt.Sprintf("  %s [%d] %s (%s)\n", status, i+1, node.Name, node.Type)
	}

	// 当前节点详情
	if currentNode, err := scr.GetNodeByID(progress.CurrentNodeID); err == nil {
		result += fmt.Sprintf("\n当前节点: %s\n", currentNode.Name)
		result += fmt.Sprintf("描述: %s\n", currentNode.Description)
		if len(currentNode.Triggers) > 0 {
			result += "触发条件:\n"
			for _, t := range currentNode.Triggers {
				result += fmt.Sprintf("  - %s\n", t)
			}
		}
	}

	return result
}

// --- 辅助函数 ---

func toLower(s string) string {
	result := make([]rune, len(s))
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			result[i] = r + 32
		} else {
			result[i] = r
		}
	}
	return string(result)
}

func containsAny(s, substr string) bool {
	// 简单的包含检查：如果 substr 的任何关键词出现在 s 中
	// 实际语义匹配由 AI Agent 完成
	return len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
