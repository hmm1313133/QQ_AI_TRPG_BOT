// Package agent - 导演系统运行记录（管理后台可视化数据源）。
//
// 每轮叙事指导（AI/规则兜底/纯规则）与每次 Planner 场景计划刷新都会
// 记录一条 DirectorLogEntry，存入全局环形缓存，供管理端 API 查询。
// 与 lore 注入记录（RecentInjections）同一套内存可观测性模式。
package agent

import (
	"time"
)

// directorLogCap 全局保留的最近记录条数（环形缓存）。
const directorLogCap = 100

// DirectorLogEntry 导演系统的一条运行记录。
type DirectorLogEntry struct {
	Time           string        `json:"time"`             // RFC3339 时间
	SessionID      string        `json:"session_id"`       // 会话（世界）ID
	Round          int           `json:"round"`            // 回合数
	Kind           string        `json:"kind"`             // guidance / plan
	Source         string        `json:"source"`           // ai / fallback / rule
	ElapsedMs      int64         `json:"elapsed_ms"`       // LLM 耗时（规则生成时为 0）
	Tone           string        `json:"tone,omitempty"`   // 叙事基调
	Pacing         string        `json:"pacing,omitempty"` // 节奏
	Focus          []string      `json:"focus,omitempty"`
	Constraints    []string      `json:"constraints,omitempty"`
	StateUpdates   []StateUpdate `json:"state_updates,omitempty"`
	AppliedUpdates int           `json:"applied_updates"`        // 状态更新命中数
	ScenePlan      string        `json:"scene_plan,omitempty"`   // 计划文本（kind=plan）
	Error          string        `json:"error,omitempty"`        // 降级/失败原因
}

// recordDirectorLog 追加一条导演系统运行记录（全局环形缓存）。
func (t *TurnEngine) recordDirectorLog(e DirectorLogEntry) {
	if e.Time == "" {
		e.Time = time.Now().Format(time.RFC3339)
	}
	t.dirLogMu.Lock()
	defer t.dirLogMu.Unlock()
	t.dirLogs = append(t.dirLogs, e)
	if len(t.dirLogs) > directorLogCap {
		t.dirLogs = t.dirLogs[len(t.dirLogs)-directorLogCap:]
	}
}

// RecentDirectorLogs 返回最近的导演系统运行记录（新->旧）。
// sessionID 非空时按会话过滤；limit<=0 时返回全部匹配记录。
func (t *TurnEngine) RecentDirectorLogs(sessionID string, limit int) []DirectorLogEntry {
	t.dirLogMu.Lock()
	defer t.dirLogMu.Unlock()
	out := make([]DirectorLogEntry, 0, len(t.dirLogs))
	for i := len(t.dirLogs) - 1; i >= 0; i-- {
		e := t.dirLogs[i]
		if sessionID != "" && e.SessionID != sessionID {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
