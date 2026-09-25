// Package agent - 导演系统运行记录（管理后台可视化数据源）。
//
// 每轮叙事指导（AI/规则兜底/纯规则）与每次 Planner 场景计划刷新都会
// 记录一条 DirectorLogEntry，存入全局环形缓存，供管理端 API 查询。
// 与 lore 注入记录（RecentInjections）同一套内存可观测性模式。
package agent

import (
	"sort"
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
	AppliedUpdates int           `json:"applied_updates"`      // 状态更新命中数
	ScenePlan      string        `json:"scene_plan,omitempty"` // 计划文本（kind=plan）
	Reasoning      string        `json:"reasoning,omitempty"`  // 决策推理（kind=plan；此前被丢弃，仅观测用）
	Error          string        `json:"error,omitempty"`      // 降级/失败原因
}

// recordDirectorLog 追加一条导演系统运行记录（按会话分桶的环形缓存）。
// 分桶可避免多世界共享一个全局环形缓存时相互挤占历史。
func (t *TurnEngine) recordDirectorLog(e DirectorLogEntry) {
	if e.Time == "" {
		e.Time = time.Now().Format(time.RFC3339)
	}
	t.dirLogMu.Lock()
	defer t.dirLogMu.Unlock()
	if t.dirLogs == nil {
		t.dirLogs = make(map[string][]DirectorLogEntry)
	}
	list := append(t.dirLogs[e.SessionID], e)
	if len(list) > directorLogCap {
		list = list[len(list)-directorLogCap:]
	}
	t.dirLogs[e.SessionID] = list
}

// RecentDirectorLogs 返回最近的导演系统运行记录（新->旧）。
// sessionID 非空时只返回该会话；为空时合并全部会话并按时间倒序取前 limit 条。
// limit<=0 时返回全部匹配记录。
func (t *TurnEngine) RecentDirectorLogs(sessionID string, limit int) []DirectorLogEntry {
	t.dirLogMu.Lock()
	defer t.dirLogMu.Unlock()

	if sessionID != "" {
		list := t.dirLogs[sessionID]
		out := make([]DirectorLogEntry, 0, len(list))
		for i := len(list) - 1; i >= 0; i-- {
			out = append(out, list[i])
			if limit > 0 && len(out) >= limit {
				break
			}
		}
		return out
	}

	all := make([]DirectorLogEntry, 0)
	for _, list := range t.dirLogs {
		all = append(all, list...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Time > all[j].Time })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}
