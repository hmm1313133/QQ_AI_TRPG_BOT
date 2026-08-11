// Package web - 玩家侧剧情摘要查看/编辑 API（Web 聊天页）。
//
// 剧情摘要（WorldState.CampaignSummary）是滚动更新的上下文总结：
// 对话滑出 RecentTurns 窗口后由 SummaryService 批量并入摘要。
// 玩家可查看并手动修正当前会话世界的摘要。
package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

// registerChatSummaryRoutes 注册玩家侧剧情摘要路由（buildMux 中调用）。
func (s *Server) registerChatSummaryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/chat/summary", s.handleChatSummaryGet)
	mux.HandleFunc("PUT /api/chat/summary", s.handleChatSummaryPut)
}

// handleChatSummaryGet 返回当前会话世界的剧情摘要与待总结轮数。
func (s *Server) handleChatSummaryGet(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.chatWorldContext(w, r)
	if !ok {
		return
	}
	writeJSON(w, map[string]any{
		"summary":       ws.CampaignSummary,
		"pending_turns": len(ws.PendingSummaryTurns),
	})
}

// handleChatSummaryPut 全量替换剧情摘要（世界锁内写入 + note 审计）。
func (s *Server) handleChatSummaryPut(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		writeChatError(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}

	engine := s.adminDeps.WorldEngine
	engine.Lock(sessionID)
	defer engine.Unlock(sessionID)

	ws := engine.LoadOrNil(sessionID)
	if ws == nil {
		writeChatError(w, "当前会话还没有进行中的世界", http.StatusNotFound)
		return
	}
	ws.CampaignSummary = strings.TrimSpace(req.Summary)
	if err := saveWorldWithAuditNote(engine, ws, "player",
		"section:summary", "玩家编辑剧情摘要"); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
