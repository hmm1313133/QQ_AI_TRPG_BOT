// Package web - 主提示词预设列表 API。
//
// 预设为内置只读（agent.BuiltinPresets），选择结果按世界存于
// StyleConfig.PromptPreset / CustomPrompt（style 分区编辑）。
// 玩家端与管理端共用同一 handler，仅注册路径与鉴权不同。
package web

import (
	"encoding/json"
	"net/http"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
)

// handlePromptPresets 返回内置主提示词预设全量（含 content 供前端预览）。
func handlePromptPresets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(agent.BuiltinPresets)
}

// registerPromptPresetRoutes 注册玩家侧预设列表路由。
func (s *Server) registerPromptPresetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/chat/prompt-presets", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.chatSessionContext(w, r); !ok {
			return
		}
		handlePromptPresets(w, r)
	})
}
