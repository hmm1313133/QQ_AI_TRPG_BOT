// Package web - 玩家侧会话状态 API（Web 聊天页）。
//
// 替代旧的 .mode/.length/.persona/.world 指令透传：
//   - GET|PUT /api/chat/state        会话模式与回复长度
//   - GET|PUT /api/chat/persona      玩家人设（全局默认 + 每世界覆盖两层）
//   - POST    /api/chat/world/enter  进入世界（实例化复制到当前会话）
//   - POST    /api/chat/world/leave  离开世界（删除会话世界状态）
//
// 鉴权与 /api/saves 一致：聊天令牌（?auth=）+ 会话 token 定位会话（web:<token>）。
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// sessionKeyWorldSource 会话中记录"当前世界复制自哪个源世界"的键。
const sessionKeyWorldSource = "world_source_id"

// snapshotWorldPrefix 切换世界自动进度快照的世界 ID 前缀（隐藏世界，
// 不出现在管理端/玩家端世界列表；与玩家手动存档相互独立）。
const snapshotWorldPrefix = "websnap:"

// snapshotWorldID 进度快照世界 ID：websnap:<会话ID>:<源世界ID>。
func snapshotWorldID(sessionID, sourceID string) string {
	return snapshotWorldPrefix + sessionID + ":" + sourceID
}

// isHiddenWorldID 判断是否为会话实例世界（web:*）或进度快照（websnap:*），
// 管理端/玩家端世界列表统一过滤，避免实例复制与快照污染世界管理视图。
func isHiddenWorldID(id string) bool {
	return strings.HasPrefix(id, "web:") || strings.HasPrefix(id, snapshotWorldPrefix)
}

// registerChatSessionRoutes 注册玩家侧会话状态路由（buildMux 中调用）。
func (s *Server) registerChatSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/chat/state", s.handleChatStateGet)
	mux.HandleFunc("PUT /api/chat/state", s.handleChatStatePut)
	mux.HandleFunc("GET /api/chat/persona", s.handleChatPersonaGet)
	mux.HandleFunc("PUT /api/chat/persona", s.handleChatPersonaPut)
	mux.HandleFunc("POST /api/chat/world/enter", s.handleChatWorldEnter)
	mux.HandleFunc("POST /api/chat/world/leave", s.handleChatWorldLeave)
}

// chatSessionContext 校验聊天令牌 + 会话 token，返回会话 ID（web:<token>）。
func (s *Server) chatSessionContext(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.cfg.ChatToken != "" && r.URL.Query().Get("auth") != s.cfg.ChatToken {
		writeChatError(w, "未授权", http.StatusUnauthorized)
		return "", false
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		writeChatError(w, "缺少 token", http.StatusUnauthorized)
		return "", false
	}
	return "web:" + token, true
}

// writeChatError 输出 {"error":"..."} 形式的错误响应。
func writeChatError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// chatStateResp 会话状态响应（GET/PUT /api/chat/state 共用）。
type chatStateResp struct {
	Mode        string `json:"mode"`
	ReplyLength string `json:"reply_length"`
	WorldID     string `json:"world_id"`
	WorldName   string `json:"world_name"`
	Persona     string `json:"persona"`
}

// buildChatState 组装会话当前状态。
func (s *Server) buildChatState(sessionID string) chatStateResp {
	session := s.sess.GetSession(sessionID)
	resp := chatStateResp{
		Mode:        session.Mode.String(),
		ReplyLength: agent.SessionReplyLength(session),
	}
	if resp.ReplyLength == "" {
		resp.ReplyLength = agent.ReplyLengthStandard
	}

	var ws *world.WorldState
	if s.adminReady && s.adminDeps.WorldEngine != nil {
		ws = s.adminDeps.WorldEngine.LoadOrNil(sessionID)
	}
	if ws != nil {
		// 世界状态存在即世界内扮演（与 TurnEngine 同口径）：会话模式只存内存，
		// 进程重启后回落 normal，这里按世界状态纠正显示，避免聊天页显示
		// "已进入世界"但模式却是 normal 的不一致。
		if session.Mode == core.ModeNormal {
			resp.Mode = core.ModeFreeChat.String()
		}
		// 优先回报源世界 ID（进入时记账），否则回退会话世界自身 ID
		if v, ok := session.Get(sessionKeyWorldSource); ok {
			if id, ok := v.(string); ok {
				resp.WorldID = id
			}
		}
		if resp.WorldID == "" {
			resp.WorldID = ws.WorldID
		}
		resp.WorldName = chatWorldName(ws)
		if p := ws.EffectivePersona(sessionID, s.adminDeps.PersonaStore); !p.Empty() {
			resp.Persona = p.Description
		}
	} else if s.adminReady && s.adminDeps.PersonaStore != nil {
		if p, err := s.adminDeps.PersonaStore.Get(sessionID); err == nil && !p.Empty() {
			resp.Persona = p.Description
		}
	}
	return resp
}

// chatWorldName 世界显示名：显示名（Name/ScriptName）→ 背景摘要（前 20 字）→ 世界 ID。
func chatWorldName(ws *world.WorldState) string {
	if name := ws.DisplayName(); name != "" {
		return name
	}
	if name := firstRunes(strings.TrimSpace(ws.Background), 20); name != "" {
		return name
	}
	return ws.WorldID
}

// handleChatStateGet 返回会话当前状态。
func (s *Server) handleChatStateGet(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	writeJSON(w, s.buildChatState(sessionID))
}

// chatStatePutReq 更新会话状态请求（字段均可选，只更新传入的）。
type chatStatePutReq struct {
	Mode        *string `json:"mode"`
	ReplyLength *string `json:"reply_length"`
	WorldName   *string `json:"world_name"` // 重命名当前世界（需在世界内）
}

// handleChatStatePut 更新会话模式/回复长度/世界名称，返回更新后的完整状态。
func (s *Server) handleChatStatePut(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	var req chatStatePutReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Mode != nil {
		var mode core.SessionMode
		switch strings.TrimSpace(*req.Mode) {
		case "normal":
			mode = core.ModeNormal
		case "freechat":
			mode = core.ModeFreeChat
		default:
			writeChatError(w, "未知模式: "+*req.Mode+"（可用: normal / freechat）", http.StatusBadRequest)
			return
		}
		s.sess.SetMode(sessionID, mode)
	}
	if req.ReplyLength != nil {
		pref := agent.NormalizeReplyLength(strings.TrimSpace(*req.ReplyLength))
		if pref == "" {
			writeChatError(w, "未知回复长度档位: "+*req.ReplyLength+"（可用: short / standard / detailed）", http.StatusBadRequest)
			return
		}
		agent.SetSessionReplyLength(s.sess.GetSession(sessionID), pref)
	}
	if req.WorldName != nil {
		if err := s.renameChatWorld(sessionID, *req.WorldName); err != nil {
			writeChatError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	writeJSON(w, s.buildChatState(sessionID))
}

// renameChatWorld 重命名当前会话的世界（经 ApplyEvent 单写入口）。
func (s *Server) renameChatWorld(sessionID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("世界名称不能为空")
	}
	if len([]rune(name)) > 50 {
		return fmt.Errorf("世界名称不能超过 50 字")
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		return fmt.Errorf("世界引擎不可用")
	}
	ws := s.adminDeps.WorldEngine.LoadOrNil(sessionID)
	if ws == nil {
		return fmt.Errorf("当前不在世界中，无法命名")
	}
	s.adminDeps.WorldEngine.Lock(sessionID)
	defer s.adminDeps.WorldEngine.Unlock(sessionID)
	if ok, err := s.adminDeps.WorldEngine.ApplyEvent(ws, world.WorldEvent{
		Type: "rename", Actor: "player", Target: ws.WorldID, Value: name,
	}); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("名称未变化")
	}
	return s.adminDeps.WorldEngine.Save(ws)
}

// ============================================================
// 玩家人设（全局默认 + 每世界覆盖）
// ============================================================

// chatPersonaResp 人设查询响应。
type chatPersonaResp struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleChatPersonaGet 查询人设：?world=<id> 查该世界覆盖层，缺省查全局默认。
func (s *Server) handleChatPersonaGet(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	worldID := r.URL.Query().Get("world")
	var p *persona.Profile
	if worldID == "" {
		if s.adminReady && s.adminDeps.PersonaStore != nil {
			p, _ = s.adminDeps.PersonaStore.Get(sessionID)
		}
	} else {
		if !s.adminReady || s.adminDeps.WorldEngine == nil {
			writeChatError(w, "世界引擎不可用", http.StatusServiceUnavailable)
			return
		}
		if ws := s.adminDeps.WorldEngine.LoadOrNil(worldID); ws != nil {
			p = ws.Personas[sessionID]
		}
	}
	resp := chatPersonaResp{}
	if !p.Empty() {
		resp.Name = p.Name
		resp.Description = p.Description
	}
	writeJSON(w, resp)
}

// chatPersonaPutReq 人设写入请求（description 空串 = 清除该层人设）。
type chatPersonaPutReq struct {
	World       string `json:"world"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleChatPersonaPut 写入人设：world 空串 = 全局默认，否则为该世界覆盖层。
func (s *Server) handleChatPersonaPut(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	var req chatPersonaPutReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)

	if req.World == "" {
		if !s.adminReady || s.adminDeps.PersonaStore == nil {
			writeChatError(w, "人设存储不可用", http.StatusServiceUnavailable)
			return
		}
		var err error
		if req.Description == "" {
			err = s.adminDeps.PersonaStore.Delete(sessionID)
		} else {
			err = s.adminDeps.PersonaStore.Set(sessionID, persona.Profile{Name: req.Name, Description: req.Description})
		}
		if err != nil {
			writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}

	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		writeChatError(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return
	}
	engine := s.adminDeps.WorldEngine
	engine.Lock(req.World)
	defer engine.Unlock(req.World)
	ws := engine.LoadOrNil(req.World)
	if ws == nil {
		writeChatError(w, "世界不存在: "+req.World, http.StatusNotFound)
		return
	}
	if req.Description == "" {
		delete(ws.Personas, sessionID)
	} else {
		ws.Personas[sessionID] = &persona.Profile{Name: req.Name, Description: req.Description}
	}
	if err := engine.Save(ws); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ============================================================
// 进入 / 离开世界
// ============================================================

// chatWorldEnterReq 进入世界请求。
type chatWorldEnterReq struct {
	WorldID string `json:"world_id"`
}

// handleChatWorldEnter 进入世界：优先恢复该会话在此世界的进度快照（切换世界时
// 自动保存的进度），无快照则把源世界实例化复制到当前会话（整体快照），
// 并切换到 freechat（世界内扮演）模式。
func (s *Server) handleChatWorldEnter(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		writeChatError(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return
	}
	var req chatWorldEnterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.WorldID = strings.TrimSpace(req.WorldID)
	if req.WorldID == "" {
		writeChatError(w, "world_id 不能为空", http.StatusBadRequest)
		return
	}
	if req.WorldID == sessionID {
		writeChatError(w, "不能进入当前会话自己的世界", http.StatusBadRequest)
		return
	}

	engine := s.adminDeps.WorldEngine
	engine.Lock(sessionID)
	defer engine.Unlock(sessionID)

	if engine.LoadOrNil(sessionID) != nil {
		writeChatError(w, "当前会话已有进行中的世界，先离开后再进入", http.StatusConflict)
		return
	}

	// 优先恢复进度快照（切换世界自动保存的进度）；无快照则从源世界模板实例化。
	snapID := snapshotWorldID(sessionID, req.WorldID)
	src := engine.LoadOrNil(snapID)
	restored := src != nil
	if !restored {
		src = engine.LoadOrNil(req.WorldID)
		if src == nil {
			writeChatError(w, "世界不存在: "+req.WorldID, http.StatusNotFound)
			return
		}
	}

	worldName := chatWorldName(src)
	// 实例化复制：repo Load 返回的是新解码对象，直接改 ID 写回即为整体快照，
	// 轮次/场景/角色等状态原样带入，源世界（模板或进度快照）不受影响。
	src.WorldID = sessionID
	if err := engine.Save(src); err != nil {
		writeChatError(w, "进入失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if restored {
		// 快照被消费：离开时会重新保存最新进度，避免残留旧档
		_ = engine.Delete(snapID)
	}

	session := s.sess.GetSession(sessionID)
	session.Set(sessionKeyWorldSource, req.WorldID)
	s.sess.SetMode(sessionID, core.ModeFreeChat)

	writeJSON(w, map[string]any{"ok": true, "world_name": worldName, "restored": restored})
}

// handleChatWorldLeave 离开世界：把当前进度自动保存为隐藏进度快照（下次进入同一
// 世界时自动恢复；与手动存档相互独立），随后删除会话世界状态并切回 normal
// （纯对话）模式。幂等：无进行中世界也返回 ok。
func (s *Server) handleChatWorldLeave(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		writeChatError(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return
	}

	engine := s.adminDeps.WorldEngine
	engine.Lock(sessionID)
	defer engine.Unlock(sessionID)

	if ws := engine.LoadOrNil(sessionID); ws != nil {
		// 自动保存进度快照（按源世界归档）；无源世界记录的老数据直接删除
		session := s.sess.GetSession(sessionID)
		if v, ok := session.Get(sessionKeyWorldSource); ok {
			if sourceID, ok := v.(string); ok && sourceID != "" {
				snap := *ws // 拷贝后改 ID 另存，原会话世界随后删除
				snap.WorldID = snapshotWorldID(sessionID, sourceID)
				if err := engine.Save(&snap); err != nil {
					writeChatError(w, "保存进度失败: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
		if err := engine.Delete(sessionID); err != nil {
			writeChatError(w, "删除失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	session := s.sess.GetSession(sessionID)
	session.Set(sessionKeyWorldSource, nil)
	s.sess.SetMode(sessionID, core.ModeNormal)

	writeJSON(w, map[string]bool{"ok": true})
}
