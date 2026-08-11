// Package web - 玩家侧剧情摘要 API（Web 聊天页）。
//
// 剧情摘要（WorldState.Summaries）是阶段性多条摘要：对话滑出 RecentTurns 窗口后
// 由 SummaryService 攒批总结，一个阶段追加一条；多条摘要按时间顺序注入上下文，
// 替代远期对话窗口。玩家可查看、修改、删除已有条目，也可手动补录新条目。
//
//   - GET    /api/chat/summary                摘要条目列表 + 待总结轮数
//   - POST   /api/chat/summary/entries        手动补录一条（{text}）
//   - PUT    /api/chat/summary/entries/{id}   修改条目文本（{text}）
//   - DELETE /api/chat/summary/entries/{id}   删除条目
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// registerChatSummaryRoutes 注册玩家侧剧情摘要路由（buildMux 中调用）。
func (s *Server) registerChatSummaryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/chat/summary", s.handleChatSummaryGet)
	mux.HandleFunc("POST /api/chat/summary/entries", s.handleChatSummaryAdd)
	mux.HandleFunc("PUT /api/chat/summary/entries/{id}", s.handleChatSummaryEdit)
	mux.HandleFunc("DELETE /api/chat/summary/entries/{id}", s.handleChatSummaryDelete)
}

// handleChatSummaryGet 返回当前会话世界的摘要条目列表与待总结轮数。
func (s *Server) handleChatSummaryGet(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.chatWorldContext(w, r)
	if !ok {
		return
	}
	entries := ws.SummaryEntries()
	if entries == nil {
		entries = []world.SummaryEntry{}
	}
	writeJSON(w, map[string]any{
		"entries":       entries,
		"pending_turns": len(ws.PendingSummaryTurns),
	})
}

// chatSummaryTextReq 摘要条目文本请求体。
type chatSummaryTextReq struct {
	Text string `json:"text"`
}

// chatSummaryWorld 校验会话与世界并加世界锁（调用方负责解锁），返回会话 ID 与世界状态。
func (s *Server) chatSummaryWorld(w http.ResponseWriter, r *http.Request) (string, *world.WorldState, bool) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return "", nil, false
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		writeChatError(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return "", nil, false
	}
	engine := s.adminDeps.WorldEngine
	engine.Lock(sessionID)
	ws := engine.LoadOrNil(sessionID)
	if ws == nil {
		engine.Unlock(sessionID)
		writeChatError(w, "当前会话还没有进行中的世界", http.StatusNotFound)
		return "", nil, false
	}
	return sessionID, ws, true
}

// handleChatSummaryAdd 手动补录一条摘要（追加到列表末尾；轮次范围为 0 表示手动条目）。
func (s *Server) handleChatSummaryAdd(w http.ResponseWriter, r *http.Request) {
	sessionID, ws, ok := s.chatSummaryWorld(w, r)
	if !ok {
		return
	}
	defer s.adminDeps.WorldEngine.Unlock(sessionID)

	var req chatSummaryTextReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeChatError(w, "text 不能为空", http.StatusBadRequest)
		return
	}
	id := ws.AppendSummary(text, 0, 0, "")
	if err := saveWorldWithAuditNote(s.adminDeps.WorldEngine, ws, "player",
		"section:summary", fmt.Sprintf("玩家补录剧情摘要 #%d", id)); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

// chatSummaryEntryID 解析路径中的条目 id。
func chatSummaryEntryID(w http.ResponseWriter, r *http.Request) (int, bool) {
	var id int
	if _, err := fmt.Sscanf(r.PathValue("id"), "%d", &id); err != nil || id <= 0 {
		writeChatError(w, "条目 ID 无效", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// handleChatSummaryEdit 修改一条摘要的文本。
func (s *Server) handleChatSummaryEdit(w http.ResponseWriter, r *http.Request) {
	sessionID, ws, ok := s.chatSummaryWorld(w, r)
	if !ok {
		return
	}
	defer s.adminDeps.WorldEngine.Unlock(sessionID)

	id, ok := chatSummaryEntryID(w, r)
	if !ok {
		return
	}
	var req chatSummaryTextReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeChatError(w, "text 不能为空", http.StatusBadRequest)
		return
	}
	if !ws.EditSummary(id, text) {
		writeChatError(w, "摘要条目不存在", http.StatusNotFound)
		return
	}
	if err := saveWorldWithAuditNote(s.adminDeps.WorldEngine, ws, "player",
		"section:summary", fmt.Sprintf("玩家编辑剧情摘要 #%d", id)); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

// handleChatSummaryDelete 删除一条摘要。
func (s *Server) handleChatSummaryDelete(w http.ResponseWriter, r *http.Request) {
	sessionID, ws, ok := s.chatSummaryWorld(w, r)
	if !ok {
		return
	}
	defer s.adminDeps.WorldEngine.Unlock(sessionID)

	id, ok := chatSummaryEntryID(w, r)
	if !ok {
		return
	}
	if !ws.DeleteSummary(id) {
		writeChatError(w, "摘要条目不存在", http.StatusNotFound)
		return
	}
	if err := saveWorldWithAuditNote(s.adminDeps.WorldEngine, ws, "player",
		"section:summary", fmt.Sprintf("玩家删除剧情摘要 #%d", id)); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}
