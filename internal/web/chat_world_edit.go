// Package web - 玩家侧世界状态查看/编辑 API（Web 聊天页）。
//
// 会话即世界：世界 ID == 会话 ID（web:<token>）。返回形状与写入口径
// 对齐管理端 /api/admin/worlds/{id} 与 /section（复用 admin_lore.go 的
// sectionParts/applySection/writeSectionJSON），区别仅在鉴权方式与
// 审计事件 Actor（玩家侧为 "player"）。
package web

import (
	"encoding/json"
	"net/http"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// registerChatWorldEditRoutes 注册玩家侧世界状态查看/编辑路由（buildMux 中调用）。
func (s *Server) registerChatWorldEditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/chat/world/detail", s.handleChatWorldDetail)
	mux.HandleFunc("GET /api/chat/world/section", s.handleChatSectionGet)
	mux.HandleFunc("PATCH /api/chat/world/section", s.handleChatSectionPatch)
}

// chatWorldContext 校验会话并加载会话世界；
// 未进入世界写 404 {"error":"当前会话还没有进行中的世界"} 并返回 ok=false。
func (s *Server) chatWorldContext(w http.ResponseWriter, r *http.Request) (string, *world.WorldState, bool) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return "", nil, false
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		writeChatError(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return "", nil, false
	}
	ws := s.adminDeps.WorldEngine.LoadOrNil(sessionID)
	if ws == nil {
		writeChatError(w, "当前会话还没有进行中的世界", http.StatusNotFound)
		return "", nil, false
	}
	return sessionID, ws, true
}

// handleChatWorldDetail 返回会话世界完整 WorldState JSON（形状同 admin 世界 detail）。
func (s *Server) handleChatWorldDetail(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.chatWorldContext(w, r)
	if !ok {
		return
	}
	writeJSON(w, ws)
}

// handleChatSectionGet 按需返回会话世界指定分区 JSON（形状同 admin section GET）。
func (s *Server) handleChatSectionGet(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.chatWorldContext(w, r)
	if !ok {
		return
	}
	if part := r.URL.Query().Get("part"); !writeSectionJSON(w, ws, part) {
		writeChatError(w, "未知分区: "+part, http.StatusBadRequest)
	}
}

// handleChatSectionPatch 按 part 全量替换会话世界对应分区（复用 applySection 校验），
// 保存时追加 Actor="player" 的 note 审计事件。
func (s *Server) handleChatSectionPatch(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		writeChatError(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Part string          `json:"part"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !sectionParts[req.Part] {
		writeChatError(w, "未知分区: "+req.Part, http.StatusBadRequest)
		return
	}
	if len(req.Data) == 0 {
		writeChatError(w, "缺少 data", http.StatusBadRequest)
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
	if err := applySection(ws, req.Part, req.Data); err != nil {
		writeChatError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := saveWorldWithAuditNote(engine, ws, "player",
		"section:"+req.Part, "玩家编辑分区 "+req.Part); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "已应用"})
}
