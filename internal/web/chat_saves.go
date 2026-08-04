// Package web - 玩家侧存档 API（Web 聊天页，设计 §9.4）。
//
// 会话即世界：Web 聊天会话 ID（web:<token>）与世界 ID 相同，
// 玩家对当前游玩世界做保存/读取。鉴权沿用聊天令牌（?auth=）+ 会话 token。
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// registerChatSaveRoutes 注册玩家侧存档路由（buildMux 中调用）。
func (s *Server) registerChatSaveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/saves", s.handleChatSaveList)
	mux.HandleFunc("POST /api/saves", s.handleChatSaveCreate)
	mux.HandleFunc("POST /api/saves/{id}/restore", s.handleChatSaveRestore)
}

// chatSaveContext 校验聊天令牌 + 会话 token，返回世界 ID 与 SQLite 仓储。
func (s *Server) chatSaveContext(w http.ResponseWriter, r *http.Request) (string, *world.SQLiteRepository, bool) {
	if s.cfg.ChatToken != "" && r.URL.Query().Get("auth") != s.cfg.ChatToken {
		http.Error(w, "未授权", http.StatusUnauthorized)
		return "", nil, false
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "缺少 token", http.StatusUnauthorized)
		return "", nil, false
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		http.Error(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return "", nil, false
	}
	repo, ok := s.adminDeps.WorldEngine.Repo().(*world.SQLiteRepository)
	if !ok {
		http.Error(w, "存档功能需要 SQLite 存储", http.StatusServiceUnavailable)
		return "", nil, false
	}
	worldID := "web:" + token
	if s.adminDeps.WorldEngine.LoadOrNil(worldID) == nil {
		http.Error(w, "当前会话还没有进行中的世界（先加载剧本或创建世界）", http.StatusNotFound)
		return "", nil, false
	}
	return worldID, repo, true
}

func (s *Server) handleChatSaveList(w http.ResponseWriter, r *http.Request) {
	worldID, repo, ok := s.chatSaveContext(w, r)
	if !ok {
		return
	}
	list, err := repo.ListSaves(worldID)
	if err != nil {
		http.Error(w, "查询失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

func (s *Server) handleChatSaveCreate(w http.ResponseWriter, r *http.Request) {
	worldID, repo, ok := s.chatSaveContext(w, r)
	if !ok {
		return
	}
	var req saveCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "存档名称不能为空", http.StatusBadRequest)
		return
	}
	ws := s.adminDeps.WorldEngine.LoadOrNil(worldID)
	// 快照当前会话对话历史随档存入（恢复时回放；存储未配置则不带）
	var historyJSON []byte
	if s.history != nil {
		if msgs, err := s.history.List(worldID, chatHistoryKeep); err == nil && len(msgs) > 0 {
			historyJSON, _ = json.Marshal(msgs)
		}
	}
	info, err := repo.CreateSaveWithHistory(ws, req.Name, req.Note, false, historyJSON)
	if err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, info)
}

func (s *Server) handleChatSaveRestore(w http.ResponseWriter, r *http.Request) {
	worldID, _, ok := s.chatSaveContext(w, r)
	if !ok {
		return
	}
	var saveID int64
	if _, err := fmt.Sscanf(r.PathValue("id"), "%d", &saveID); err != nil {
		http.Error(w, "存档 ID 无效", http.StatusBadRequest)
		return
	}
	info, history, err := restoreWorldSave(s.adminDeps.WorldEngine, worldID, saveID, "player")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 存档带对话历史快照时同步回放（QQ 创建的档为 NULL 跳过）
	replaySaveHistory(s.history, worldID, history)
	writeJSON(w, map[string]string{"message": fmt.Sprintf("已恢复到存档「%s」（轮次 %d）", info.Name, info.RoundCount)})
}

// replaySaveHistory 用存档快照整体覆盖会话聊天记录（无快照或存储不可用时静默跳过）。
func replaySaveHistory(h ChatLogger, sessionID string, data []byte) {
	if h == nil || len(data) == 0 {
		return
	}
	var msgs []ChatMessage
	if err := json.Unmarshal(data, &msgs); err != nil || len(msgs) == 0 {
		return
	}
	_ = h.Replace(sessionID, msgs) // 回放失败不影响恢复主流程
}
