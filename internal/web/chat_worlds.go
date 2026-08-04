// Package web - 玩家侧"进入世界"选择器 API（Web 聊天页）。
//
// 与 .world 指令同一机制：聊天会话（web:<token>）经实例化复制进入
// 管理后台创建的世界。这里只提供列表数据，进入动作仍走 .world enter 指令
// （复用指令链路，无需新写路径）。鉴权沿用聊天令牌（?auth=）+ 会话 token。
package web

import (
	"net/http"
)

// registerChatWorldRoutes 注册玩家侧进入世界路由（buildMux 中调用）。
func (s *Server) registerChatWorldRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/worlds", s.handleChatWorlds)
}

// chatWorldBrief 可进入世界的列表项。
type chatWorldBrief struct {
	ID             string `json:"id"`
	Mode           string `json:"mode"`
	ScriptName     string `json:"script_name,omitempty"`
	Background     string `json:"background,omitempty"` // 背景摘要（前 20 字）
	RoundCount     int    `json:"round_count"`
	CharacterCount int    `json:"character_count"`
}

// handleChatWorlds 返回当前会话状态与可进入的世界列表（排除当前会话自己的世界）。
func (s *Server) handleChatWorlds(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ChatToken != "" && r.URL.Query().Get("auth") != s.cfg.ChatToken {
		http.Error(w, "未授权", http.StatusUnauthorized)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "缺少 token", http.StatusUnauthorized)
		return
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		http.Error(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return
	}

	worldID := "web:" + token
	resp := map[string]any{"current_world_id": "", "worlds": []chatWorldBrief{}}
	if s.adminDeps.WorldEngine.LoadOrNil(worldID) != nil {
		resp["current_world_id"] = worldID
	}

	ids, err := s.adminDeps.WorldEngine.ListWorlds()
	if err != nil {
		http.Error(w, "查询失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	list := []chatWorldBrief{}
	for _, id := range ids {
		if id == worldID {
			continue // 排除当前会话自己的世界
		}
		ws := s.adminDeps.WorldEngine.LoadOrNil(id)
		if ws == nil {
			continue
		}
		list = append(list, chatWorldBrief{
			ID:             ws.WorldID,
			Mode:           ws.Mode,
			ScriptName:     ws.ScriptName,
			Background:     firstRunes(ws.Background, 20),
			RoundCount:     ws.RoundCount,
			CharacterCount: len(ws.Characters),
		})
	}
	resp["worlds"] = list
	writeJSON(w, resp)
}
