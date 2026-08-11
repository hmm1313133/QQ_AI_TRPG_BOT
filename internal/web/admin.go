// 管理后台 API（设计见《多渠道改造计划.md》4.4）。
//
// 安全原则：
//   - 全部端点需要 Bearer Token（ADMIN_TOKEN）；未设置时仅本机可访问
//   - 一切世界状态修改经 world.Engine.ApplyEvent 单写入入口，不做特权直写
package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/assetparse"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/pkg"
)

// idSeq 进程内单调序号：Windows 下 time.Now() 粒度粗（约 15.6ms 一档），
// 紧循环里 UnixNano 会重复，凡以时间戳生成的 ID 都追加该序号防主键冲突。
var idSeq atomic.Uint64

// AdminDeps 管理后台依赖。
type AdminDeps struct {
	Sessions    *core.SessionManager
	WorldEngine *world.Engine
	AssetStore  *world.AssetStore  // 全局素材库（设计 §9.3，可为 nil）
	AssetParser *assetparse.Parser // 素材 LLM 解析器（设计 §11.4，可为 nil）
	// PersonaStore 全局默认玩家人设存储（玩家侧 /api/chat/persona 用，可为 nil）。
	PersonaStore *persona.Store
	ConfigStore  ConfigStore // W4 接入，可为 nil
	// TurnEngine 回合引擎（lore 注入记录可观测性用，可为 nil）。
	TurnEngine *agent.TurnEngine
	StartTime  time.Time
}

// ConfigStore 运行时配置存储接口（W4 实现，此处定义解耦）。
type ConfigStore interface {
	All() map[string]string
	Set(key, value string) error
}

// adminAPI 管理后台 API 处理器。
type adminAPI struct {
	deps       AdminDeps
	adminToken string
	history    ChatLogger // 聊天记录存储（存档恢复时回放对话历史，可为 nil）
}

// registerAdmin 注册管理后台路由。
func (s *Server) registerAdmin(mux *http.ServeMux, deps AdminDeps, adminToken string) {
	a := &adminAPI{
		deps:       deps,
		adminToken: adminToken,
		history:    s.history,
	}

	mux.HandleFunc("GET /api/admin/status", a.wrap(a.handleStatus))
	mux.HandleFunc("GET /api/admin/sessions", a.wrap(a.handleSessions))
	mux.HandleFunc("GET /api/admin/worlds", a.wrap(a.handleWorlds))
	mux.HandleFunc("POST /api/admin/worlds", a.wrap(a.handleWorldCreate))
	mux.HandleFunc("GET /api/admin/worlds/{id}", a.wrap(a.handleWorldDetail))
	mux.HandleFunc("DELETE /api/admin/worlds/{id}", a.wrap(a.handleWorldDelete))
	mux.HandleFunc("PATCH /api/admin/worlds/{id}/state", a.wrap(a.handleWorldPatch))
	// 世界设定库（lore）与分区编辑（《世界设定库与按需加载设计.md》§4.4）
	mux.HandleFunc("GET /api/admin/worlds/{id}/lore", a.wrap(a.handleLoreList))
	mux.HandleFunc("POST /api/admin/worlds/{id}/lore", a.wrap(a.handleLoreCreate))
	mux.HandleFunc("PUT /api/admin/worlds/{id}/lore/{eid}", a.wrap(a.handleLoreUpdate))
	mux.HandleFunc("DELETE /api/admin/worlds/{id}/lore/{eid}", a.wrap(a.handleLoreDelete))
	mux.HandleFunc("POST /api/admin/worlds/{id}/lore/test", a.wrap(a.handleLoreTest))
	mux.HandleFunc("GET /api/admin/worlds/{id}/lore/injections", a.wrap(a.handleLoreInjections))
	mux.HandleFunc("POST /api/admin/worlds/{id}/lore/import", a.wrap(a.handleLoreImport))
	mux.HandleFunc("GET /api/admin/worlds/{id}/section", a.wrap(a.handleSectionGet))
	mux.HandleFunc("PATCH /api/admin/worlds/{id}/section", a.wrap(a.handleSectionPatch))
	mux.HandleFunc("GET /api/admin/config", a.wrap(a.handleConfigGet))
	mux.HandleFunc("PUT /api/admin/config", a.wrap(a.handleConfigSet))
	// 主提示词预设列表（内置只读，与玩家端共用 handler）
	mux.HandleFunc("GET /api/admin/prompt-presets", a.wrap(handlePromptPresets))
	// 素材联动与游玩存档（《世界编辑器与素材联动设计.md》§四/§9.4）
	a.registerAssetRoutes(mux)
}

// wrap 鉴权中间件：Bearer Token；未配置 token 时仅允许本机访问。
func (a *adminAPI) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.adminToken == "" {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil || (host != "127.0.0.1" && host != "::1") {
				http.Error(w, "管理后台未配置 ADMIN_TOKEN，仅允许本机访问", http.StatusForbidden)
				return
			}
		} else {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+a.adminToken {
				http.Error(w, "未授权", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

// ============================================================
// 状态与会话
// ============================================================

func (a *adminAPI) handleStatus(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(a.deps.StartTime).Round(time.Second)
	writeJSON(w, map[string]interface{}{
		"version":      pkg.Version,
		"uptime":       uptime.String(),
		"sessionCount": len(a.deps.Sessions.List()),
		"startedAt":    a.deps.StartTime.Format("2006-01-02 15:04:05"),
	})
}

func (a *adminAPI) handleSessions(w http.ResponseWriter, r *http.Request) {
	type sessionInfo struct {
		ID      string `json:"id"`
		Mode    string `json:"mode"`
		AgentID string `json:"agent_id,omitempty"`
	}
	var list []sessionInfo
	for _, sess := range a.deps.Sessions.List() {
		list = append(list, sessionInfo{ID: sess.ID, Mode: sess.Mode.String(), AgentID: sess.AgentID})
	}
	writeJSON(w, list)
}

// ============================================================
// 世界管理
// ============================================================

func (a *adminAPI) handleWorlds(w http.ResponseWriter, r *http.Request) {
	ids, err := a.deps.WorldEngine.ListWorlds()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type worldInfo struct {
		ID         string `json:"id"`
		Mode       string `json:"mode"`
		ScriptName string `json:"script_name,omitempty"`
		Scene      string `json:"scene"`
		Round      int    `json:"round"`
		UpdatedAt  string `json:"updated_at"`
	}
	var list []worldInfo
	for _, id := range ids {
		ws := a.deps.WorldEngine.LoadOrNil(id)
		if ws == nil {
			continue
		}
		list = append(list, worldInfo{
			ID: ws.WorldID, Mode: ws.Mode, ScriptName: ws.ScriptName,
			Scene: ws.Scene.NodeName, Round: ws.RoundCount, UpdatedAt: ws.LastUpdate,
		})
	}
	writeJSON(w, list)
}

func (a *adminAPI) handleWorldDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ws := a.deps.WorldEngine.LoadOrNil(id)
	if ws == nil {
		http.Error(w, "世界不存在", http.StatusNotFound)
		return
	}
	writeJSON(w, ws)
}

// patchReq 是受控状态修改请求（经 ApplyEvent 校验）。
type patchReq struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Value  string `json:"value"`
}

// handleWorldPatch 受控修改世界状态（ApplyEvent 单写入入口）。
func (a *adminAPI) handleWorldPatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ws := a.deps.WorldEngine.LoadOrNil(id)
	if ws == nil {
		http.Error(w, "世界不存在", http.StatusNotFound)
		return
	}

	var req patchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}

	ok, err := a.deps.WorldEngine.ApplyEvent(ws, world.WorldEvent{
		Type: req.Type, Actor: "admin", Target: req.Target, Value: req.Value,
	})
	if err != nil {
		http.Error(w, "变更被拒绝: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "未匹配到目标", http.StatusNotFound)
		return
	}
	if err := a.deps.WorldEngine.Save(ws); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "已应用"})
}

// worldCreateReq 手动创建世界请求（见《管理后台扩展设计.md》2.5）。
type worldCreateReq struct {
	WorldID    string          `json:"world_id"` // 可选，缺省自动生成
	Mode       string          `json:"mode"`     // simrpg / roleplay
	Background string          `json:"background"`
	Scene      string          `json:"scene"`
	NPCs       []world.NPCSeed `json:"npcs"`
	Locations  []string        `json:"locations"`
	// 结构化素材（《世界编辑器与素材联动设计.md》§4.3，全部可选）
	Characters   []world.CharacterState `json:"characters"`
	LocationDefs []world.Location       `json:"location_defs"` // 结构化地点（locations 名称列表之外的完整形态）
	Items        []world.Item           `json:"items"`
	Factions     []world.Faction        `json:"factions"`
	Storyline    *world.Storyline       `json:"storyline"`
	// Lore 创建时随世界写入的手工设定条目（可选；校验与默认值同 POST /lore）。
	Lore []loreUpsertReq `json:"lore"`
}

// handleWorldCreate 创建并播种世界（经 Engine.SeedWorld 单写入入口）。
func (a *adminAPI) handleWorldCreate(w http.ResponseWriter, r *http.Request) {
	if a.deps.WorldEngine == nil {
		http.Error(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return
	}
	var req worldCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		http.Error(w, "模式（mode）不能为空", http.StatusBadRequest)
		return
	}
	if req.WorldID == "" {
		req.WorldID = fmt.Sprintf("world_%d_%d", time.Now().UnixNano(), idSeq.Add(1))
	}

	if a.deps.WorldEngine.LoadOrNil(req.WorldID) != nil {
		http.Error(w, "世界已存在: "+req.WorldID, http.StatusConflict)
		return
	}
	// 随附 lore 先校验（避免世界已创建才报参数错）
	for i := range req.Lore {
		if err := req.Lore[i].validate(); err != nil {
			http.Error(w, fmt.Sprintf("第 %d 条 lore 校验失败: %v", i+1, err), http.StatusBadRequest)
			return
		}
	}
	state, err := a.deps.WorldEngine.SeedWorld(req.WorldID, world.SeedSpec{
		Mode:         req.Mode,
		Background:   req.Background,
		Scene:        req.Scene,
		NPCs:         req.NPCs,
		Locations:    req.Locations,
		Characters:   req.Characters,
		LocationDefs: req.LocationDefs,
		Items:        req.Items,
		Factions:     req.Factions,
		Storyline:    req.Storyline,
	})
	if err != nil {
		http.Error(w, "创建失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 创建向导随附的手工设定条目
	if len(req.Lore) > 0 {
		a.deps.WorldEngine.Lock(state.WorldID)
		for i := range req.Lore {
			entry := world.LoreEntry{ID: newLoreID(), Source: world.LoreSourceManual}
			req.Lore[i].applyTo(&entry, true)
			state.Lore = append(state.Lore, entry)
		}
		if err := a.saveWithAuditNote(state, "lore:init",
			fmt.Sprintf("创建世界时写入手工设定条目 %d 条", len(req.Lore))); err != nil {
			a.deps.WorldEngine.Unlock(state.WorldID)
			http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		a.deps.WorldEngine.Unlock(state.WorldID)
	}
	writeJSON(w, state)
}

// handleWorldDelete 删除世界；有会话正在使用（会话 ID == 世界 ID）时拒绝。
func (a *adminAPI) handleWorldDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if a.deps.WorldEngine.LoadOrNil(id) == nil {
		http.Error(w, "世界不存在", http.StatusNotFound)
		return
	}
	// 目前世界 ID == 会话 ID，存在同 ID 会话即视为占用（见设计文档 2.5 已知限制）
	for _, sess := range a.deps.Sessions.List() {
		if sess.ID == id {
			http.Error(w, "世界正被会话使用，无法删除", http.StatusConflict)
			return
		}
	}
	if err := a.deps.WorldEngine.Delete(id); err != nil {
		http.Error(w, "删除失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "已删除"})
}

// ============================================================
// 配置管理（W4 ConfigStore 接入；键注册表见 internal/config）
// ============================================================

// configEntry 配置项：注册元数据 + 当前值（敏感值已掩码）。
type configEntry struct {
	config.KeyMeta
	Value string `json:"value"`
}

// handleConfigGet 返回注册表全部键（[]{meta+value}，敏感值掩码，明文不出包）。
func (a *adminAPI) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	if a.deps.ConfigStore == nil {
		http.Error(w, "配置存储未初始化", http.StatusServiceUnavailable)
		return
	}
	all := a.deps.ConfigStore.All()
	list := make([]configEntry, 0, len(config.KeyRegistry))
	for _, meta := range config.KeyRegistry {
		v := all[meta.Key]
		if meta.Secret && v != "" {
			v = config.SecretMask
		}
		list = append(list, configEntry{KeyMeta: meta, Value: v})
	}
	writeJSON(w, list)
}

// handleConfigSet 批量更新配置（白名单校验：拒绝未注册键；敏感键传空或掩码原样时跳过）。
func (a *adminAPI) handleConfigSet(w http.ResponseWriter, r *http.Request) {
	if a.deps.ConfigStore == nil {
		http.Error(w, "配置存储未初始化", http.StatusServiceUnavailable)
		return
	}
	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	for k := range updates {
		if !config.Registered(k) {
			http.Error(w, fmt.Sprintf("未注册的配置键: %s", k), http.StatusBadRequest)
			return
		}
	}
	updated := 0
	for k, v := range updates {
		if meta, _ := config.Meta(k); meta.Secret && (v == "" || v == config.SecretMask) {
			continue // 敏感键：留空或回传掩码 = 不修改
		}
		if err := a.deps.ConfigStore.Set(k, v); err != nil {
			http.Error(w, fmt.Sprintf("设置 %s 失败: %v", k, err), http.StatusBadRequest)
			return
		}
		updated++
	}
	log.Printf("[Admin] 配置已更新: %d 项", updated)
	writeJSON(w, map[string]string{"message": "已保存"})
}
