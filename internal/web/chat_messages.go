// Package web - 玩家侧聊天消息 API（消息的编辑、删除与 AI 回复多版本切换）。
//
//   - PUT /api/chat/messages/{id}          编辑消息文本（user / reply；reply 改的是当前激活版本）
//   - PUT /api/chat/messages/{id}/version  切换激活版本（仅 reply）
//   - DELETE /api/chat/messages/{id}       删除消息（级联删版本行）
//
// 鉴权与 /api/chat/state 一致：聊天令牌（?auth=）+ 会话 token 定位会话（web:<token>）。
// 编辑/删除/版本切换后尽力同步 WorldState.RecentTurns 中的对应文本（UpdateRecentNarrator /
// UpdateRecentPlayer / RemoveRecentText），已写入长期记忆的内容不回收（已知限制）。
package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// registerChatMessageRoutes 注册玩家侧消息编辑/删除/版本路由（buildMux 中调用）。
func (s *Server) registerChatMessageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/chat/messages/{id}", s.handleChatMessageEdit)
	mux.HandleFunc("PUT /api/chat/messages/{id}/version", s.handleChatMessageVersion)
	mux.HandleFunc("DELETE /api/chat/messages/{id}", s.handleChatMessageDelete)
}

// chatMessageTarget 解析路径 id 并取出目标消息（校验存在且属于本会话）。
// 类型是否允许操作由调用方判断。
func (s *Server) chatMessageTarget(w http.ResponseWriter, r *http.Request, sessionID string) *ChatMessage {
	if s.history == nil {
		writeChatError(w, "聊天记录存储不可用", http.StatusServiceUnavailable)
		return nil
	}
	var id int64
	if _, err := fmt.Sscanf(r.PathValue("id"), "%d", &id); err != nil || id <= 0 {
		writeChatError(w, "消息 ID 无效", http.StatusBadRequest)
		return nil
	}
	msg, err := s.history.GetMessage(sessionID, id)
	if err != nil {
		writeChatError(w, "消息不存在或不属于本会话", http.StatusNotFound)
		return nil
	}
	return msg
}

// syncRecentNarrator 编辑/版本切换后同步世界近期对话窗口（仅当会话世界存在时；
// 失败只记日志，不影响接口成功）。
func (s *Server) syncRecentNarrator(sessionID, oldText, newText string) {
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		return
	}
	if s.adminDeps.WorldEngine.LoadOrNil(sessionID) == nil {
		return
	}
	if err := s.adminDeps.WorldEngine.UpdateRecentNarrator(sessionID, oldText, newText); err != nil {
		log.Printf("[Web] 同步 RecentTurns 失败: %v", err)
	}
}

// syncRecentPlayer 编辑玩家发言后同步世界近期对话窗口的 Player 字段（同 syncRecentNarrator 口径）。
func (s *Server) syncRecentPlayer(sessionID, oldText, newText string) {
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		return
	}
	if s.adminDeps.WorldEngine.LoadOrNil(sessionID) == nil {
		return
	}
	if err := s.adminDeps.WorldEngine.UpdateRecentPlayer(sessionID, oldText, newText); err != nil {
		log.Printf("[Web] 同步 RecentTurns 失败: %v", err)
	}
}

// syncRecentRemoved 删除消息后从世界近期对话窗口清掉对应文本（同 syncRecentNarrator 口径）。
func (s *Server) syncRecentRemoved(sessionID, text string) {
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		return
	}
	if s.adminDeps.WorldEngine.LoadOrNil(sessionID) == nil {
		return
	}
	if err := s.adminDeps.WorldEngine.RemoveRecentText(sessionID, text); err != nil {
		log.Printf("[Web] 同步 RecentTurns 失败: %v", err)
	}
}

// chatMessageEditReq 编辑请求。
type chatMessageEditReq struct {
	Text string `json:"text"`
}

// handleChatMessageEdit 编辑消息文本（user / reply；reply 有版本记录时改的是当前激活版本）。
func (s *Server) handleChatMessageEdit(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	msg := s.chatMessageTarget(w, r, sessionID)
	if msg == nil {
		return
	}
	if msg.Type != "reply" && msg.Type != "user" {
		writeChatError(w, "只能编辑玩家发言或 AI 回复", http.StatusBadRequest)
		return
	}
	var req chatMessageEditReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeChatError(w, "text 不能为空", http.StatusBadRequest)
		return
	}
	oldText := msg.Text
	if err := s.history.UpdateActiveText(msg.ID, req.Text); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if msg.Type == "user" {
		s.syncRecentPlayer(sessionID, oldText, req.Text)
	} else {
		s.syncRecentNarrator(sessionID, oldText, req.Text)
	}
	writeJSON(w, map[string]any{
		"ok": true, "id": msg.ID,
		"version_no": msg.VersionNo, "version_count": msg.VersionCount,
	})
}

// chatMessageVersionReq 版本切换请求。
type chatMessageVersionReq struct {
	VersionNo int `json:"version_no"`
}

// handleChatMessageVersion 切换激活版本，返回激活文本（仅 reply 消息有多版本）。
func (s *Server) handleChatMessageVersion(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	msg := s.chatMessageTarget(w, r, sessionID)
	if msg == nil {
		return
	}
	if msg.Type != "reply" {
		writeChatError(w, "只有 AI 回复有多版本", http.StatusBadRequest)
		return
	}
	var req chatMessageVersionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.VersionNo <= 0 {
		writeChatError(w, "version_no 无效", http.StatusBadRequest)
		return
	}
	oldText := msg.Text
	text, count, err := s.history.SetActiveVersion(msg.ID, req.VersionNo)
	if err != nil {
		writeChatError(w, "切换失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.syncRecentNarrator(sessionID, oldText, text)
	writeJSON(w, map[string]any{
		"ok": true, "text": text,
		"version_no": req.VersionNo, "version_count": count,
	})
}

// handleChatMessageDelete 删除一条消息（级联删版本行），并尽力从世界近期对话窗口清掉对应文本。
func (s *Server) handleChatMessageDelete(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	msg := s.chatMessageTarget(w, r, sessionID)
	if msg == nil {
		return
	}
	if msg.Type != "reply" && msg.Type != "user" && msg.Type != "push" {
		writeChatError(w, "该类型消息不能删除", http.StatusBadRequest)
		return
	}
	if err := s.history.DeleteMessage(sessionID, msg.ID); err != nil {
		writeChatError(w, "删除失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// push 不进世界对话窗口，无需同步
	if msg.Type != "push" {
		s.syncRecentRemoved(sessionID, msg.Text)
	}
	writeJSON(w, map[string]any{"ok": true, "id": msg.ID})
}
