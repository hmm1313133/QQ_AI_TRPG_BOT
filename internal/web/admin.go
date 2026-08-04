// 管理后台 API（设计见《多渠道改造计划.md》4.4）。
//
// 安全原则：
//   - 全部端点需要 Bearer Token（ADMIN_TOKEN）；未设置时仅本机可访问
//   - 一切世界状态修改经 world.Engine.ApplyEvent 单写入入口，不做特权直写
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/assetparse"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/character"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/gamelog"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/pkg"
)

// AdminDeps 管理后台依赖。
type AdminDeps struct {
	Sessions    *core.SessionManager
	Service     *trpg.Service
	WorldEngine *world.Engine
	Archive     *script.Archive
	Analyzer    *script.ScriptAnalyzer
	CharMgr     *character.Manager
	AssetStore  *world.AssetStore // 全局素材库（设计 §9.3，可为 nil）
	AssetParser *assetparse.Parser // 素材 LLM 解析器（设计 §11.4，可为 nil）
	MemoryStore *world.MemoryStore
	GameLogger  *gamelog.GameLogger
	ConfigStore ConfigStore   // W4 接入，可为 nil
	Bot         BotController // QQ 机器人生命周期控制，可为 nil
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
	tasks      map[string]*taskStatus
	tasksMu    sync.Mutex
}

// taskStatus 异步任务状态（剧本分析等）。
type taskStatus struct {
	ID        string `json:"id"`
	Stage     string `json:"stage"`
	Message   string `json:"message"`
	Done      bool   `json:"done"`
	Error     string `json:"error,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// registerAdmin 注册管理后台路由。
func (s *Server) registerAdmin(mux *http.ServeMux, deps AdminDeps, adminToken string) {
	a := &adminAPI{
		deps:       deps,
		adminToken: adminToken,
		tasks:      make(map[string]*taskStatus),
	}

	mux.HandleFunc("GET /api/admin/status", a.wrap(a.handleStatus))
	mux.HandleFunc("GET /api/admin/sessions", a.wrap(a.handleSessions))
	mux.HandleFunc("GET /api/admin/worlds", a.wrap(a.handleWorlds))
	mux.HandleFunc("POST /api/admin/worlds", a.wrap(a.handleWorldCreate))
	mux.HandleFunc("GET /api/admin/worlds/{id}", a.wrap(a.handleWorldDetail))
	mux.HandleFunc("DELETE /api/admin/worlds/{id}", a.wrap(a.handleWorldDelete))
	mux.HandleFunc("POST /api/admin/worlds/{id}/advance", a.wrap(a.handleWorldAdvance))
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
	mux.HandleFunc("GET /api/admin/scripts", a.wrap(a.handleScripts))
	mux.HandleFunc("POST /api/admin/scripts", a.wrap(a.handleScriptCreate))
	mux.HandleFunc("GET /api/admin/scripts/{id}", a.wrap(a.handleScriptDetail))
	mux.HandleFunc("PUT /api/admin/scripts/{id}", a.wrap(a.handleScriptReplace))
	mux.HandleFunc("DELETE /api/admin/scripts/{id}", a.wrap(a.handleScriptDelete))
	mux.HandleFunc("POST /api/admin/scripts/upload", a.wrap(a.handleScriptUpload))
	mux.HandleFunc("GET /api/admin/characters", a.wrap(a.handleCharacters))
	mux.HandleFunc("POST /api/admin/characters", a.wrap(a.handleCharacterCreate))
	mux.HandleFunc("PUT /api/admin/characters/{id}", a.wrap(a.handleCharacterUpdate))
	mux.HandleFunc("DELETE /api/admin/characters/{id}", a.wrap(a.handleCharacterDelete))
	mux.HandleFunc("GET /api/admin/memory/{world}/{entity}", a.wrap(a.handleMemory))
	mux.HandleFunc("GET /api/admin/logs", a.wrap(a.handleLogSessions))
	mux.HandleFunc("GET /api/admin/logs/{sessionID}", a.wrap(a.handleLogs))
	mux.HandleFunc("GET /api/admin/config", a.wrap(a.handleConfigGet))
	mux.HandleFunc("PUT /api/admin/config", a.wrap(a.handleConfigSet))
	mux.HandleFunc("GET /api/admin/bot", a.wrap(a.handleBotStatus))
	mux.HandleFunc("POST /api/admin/bot/start", a.wrap(a.handleBotStart))
	mux.HandleFunc("POST /api/admin/bot/stop", a.wrap(a.handleBotStop))
	mux.HandleFunc("POST /api/admin/bot/restart", a.wrap(a.handleBotRestart))
	mux.HandleFunc("GET /api/admin/tasks/{id}", a.wrap(a.handleTask))
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
		Script  string `json:"script,omitempty"`
		AgentID string `json:"agent_id,omitempty"`
	}
	var list []sessionInfo
	for _, sess := range a.deps.Sessions.List() {
		info := sessionInfo{ID: sess.ID, Mode: sess.Mode.String(), AgentID: sess.AgentID}
		if v, ok := sess.Get("script_name"); ok {
			if s, ok := v.(string); ok {
				info.Script = s
			}
		}
		list = append(list, info)
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

// handleWorldAdvance 手动推进时间轴。
func (a *adminAPI) handleWorldAdvance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ws := a.deps.WorldEngine.LoadOrNil(id)
	if ws == nil {
		http.Error(w, "世界不存在", http.StatusNotFound)
		return
	}
	if a.deps.Archive == nil {
		http.Error(w, "剧本存档不可用", http.StatusServiceUnavailable)
		return
	}
	scr, err := a.deps.Archive.Get(ws.ScriptID)
	if err != nil {
		http.Error(w, "剧本不存在: "+err.Error(), http.StatusNotFound)
		return
	}
	next, err := scr.GetNextNode(ws.Scene.NodeID)
	if err != nil || next == nil {
		http.Error(w, "已是最后一个节点", http.StatusBadRequest)
		return
	}
	// 推进前自动备份（设计 §9.4：高风险操作留回滚点）
	if repo, ok := a.deps.WorldEngine.Repo().(*world.SQLiteRepository); ok {
		if _, err := repo.CreateSave(ws, "自动备份-推进前", "推进时间轴到「"+next.Name+"」前的进度", true); err != nil {
			log.Printf("[Admin] 推进前自动备份失败: %v", err)
		}
	}
	if err := a.deps.WorldEngine.RefreshScene(id, scr, next.ID); err != nil {
		http.Error(w, "推进失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": fmt.Sprintf("已推进到 %s", next.Name)})
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
	Mode       string          `json:"mode"`     // trpg / simrpg / roleplay
	Background string          `json:"background"`
	Scene      string          `json:"scene"`
	NPCs       []world.NPCSeed `json:"npcs"`
	Locations  []string        `json:"locations"`
	ScriptID   string          `json:"script_id"` // trpg 必填
	// 结构化素材（《世界编辑器与素材联动设计.md》§4.3，全部可选）
	Characters   []world.CharacterState `json:"characters"`
	LocationDefs []world.Location       `json:"location_defs"` // 结构化地点（locations 名称列表之外的完整形态）
	Items        []world.Item           `json:"items"`
	Factions     []world.Faction        `json:"factions"`
	Storyline    *world.Storyline       `json:"storyline"`
	ImportCards  []string               `json:"import_cards"` // 创建即关联的全局人物卡 ID
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
		req.WorldID = fmt.Sprintf("world_%d", time.Now().UnixNano())
	}

	// trpg 模式先按 script_id 取剧本，取不到视为请求参数错误
	var scr *script.Script
	if req.Mode == world.ModeTRPG {
		if req.ScriptID == "" {
			http.Error(w, "trpg 模式必须提供 script_id", http.StatusBadRequest)
			return
		}
		if a.deps.Archive == nil {
			http.Error(w, "剧本存档不可用", http.StatusServiceUnavailable)
			return
		}
		var err error
		scr, err = a.deps.Archive.Get(req.ScriptID)
		if err != nil {
			http.Error(w, "剧本不存在: "+err.Error(), http.StatusBadRequest)
			return
		}
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
	// 创建即关联的人物卡 → 结构化角色（CardRef 关联，数值真相仍在卡）
	for _, cardID := range req.ImportCards {
		card, err := a.deps.CharMgr.Get(cardID)
		if err != nil {
			http.Error(w, "人物卡不存在: "+cardID, http.StatusBadRequest)
			return
		}
		req.Characters = append(req.Characters, *world.CharacterStateFromCard(card))
	}
	state, err := a.deps.WorldEngine.SeedWorld(req.WorldID, world.SeedSpec{
		Mode:       req.Mode,
		Background: req.Background,
		Scene:      req.Scene,
		NPCs:       req.NPCs,
		Locations:  req.Locations,
		ScriptID:   req.ScriptID,
		Characters:   req.Characters,
		LocationDefs: req.LocationDefs,
		Items:        req.Items,
		Factions:     req.Factions,
		Storyline:    req.Storyline,
	}, scr)
	if err != nil {
		http.Error(w, "创建失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 创建向导随附的手工设定条目（trpg 世界也可在剧本生成条目之上追加）
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
// 剧本管理
// ============================================================

func (a *adminAPI) handleScripts(w http.ResponseWriter, r *http.Request) {
	type scriptInfo struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Title      string `json:"title"`
		System     string `json:"system"`
		Nodes      int    `json:"nodes"`
		Characters int    `json:"characters"`
		CreatedAt  string `json:"created_at"`
	}
	var list []scriptInfo
	for _, s := range a.deps.Archive.List() {
		list = append(list, scriptInfo{
			ID: s.ID, Name: s.Name, Title: s.Title, System: s.System,
			Nodes: len(s.Timeline), Characters: len(s.Characters), CreatedAt: s.CreatedAt,
		})
	}
	writeJSON(w, list)
}

func (a *adminAPI) handleScriptDetail(w http.ResponseWriter, r *http.Request) {
	scr, err := a.deps.Archive.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "剧本不存在", http.StatusNotFound)
		return
	}
	writeJSON(w, scr)
}

func (a *adminAPI) handleScriptDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.deps.Archive.Remove(r.PathValue("id")); err != nil {
		http.Error(w, "删除失败: "+err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"message": "已删除"})
}

// handleScriptCreate 手动创建剧本：ID 由服务端按名称生成，传入 ID 一律忽略。
func (a *adminAPI) handleScriptCreate(w http.ResponseWriter, r *http.Request) {
	if a.deps.Archive == nil {
		http.Error(w, "剧本存档不可用", http.StatusServiceUnavailable)
		return
	}
	var scr script.Script
	if err := json.NewDecoder(r.Body).Decode(&scr); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	scr.ID = script.GenerateScriptID(scr.Name)
	if scr.CreatedAt == "" {
		scr.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	if err := script.ValidateScript(&scr); err != nil {
		http.Error(w, "剧本校验失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := a.deps.Archive.Get(scr.ID); err == nil {
		http.Error(w, "剧本已存在: "+scr.ID, http.StatusConflict)
		return
	}
	if err := a.deps.Archive.Save(&scr); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, &scr)
}

// handleScriptReplace 整体替换剧本：URL id 必须等于 body 名称生成的 ID。
func (a *adminAPI) handleScriptReplace(w http.ResponseWriter, r *http.Request) {
	if a.deps.Archive == nil {
		http.Error(w, "剧本存档不可用", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	existing, err := a.deps.Archive.Get(id)
	if err != nil {
		http.Error(w, "剧本不存在", http.StatusNotFound)
		return
	}
	var scr script.Script
	if err := json.NewDecoder(r.Body).Decode(&scr); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	scr.ID = script.GenerateScriptID(scr.Name)
	if scr.ID != id {
		http.Error(w, fmt.Sprintf("ID 不可变: URL 为 %s，body 名称为 %s 生成 %s", id, scr.Name, scr.ID), http.StatusBadRequest)
		return
	}
	if scr.CreatedAt == "" {
		scr.CreatedAt = existing.CreatedAt // 整体替换不丢创建时间
	}
	if err := script.ValidateScript(&scr); err != nil {
		http.Error(w, "剧本校验失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.deps.Archive.Save(&scr); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, &scr)
}

// handleScriptUpload 上传剧本并异步分析（进度经任务表轮询）。
func (a *adminAPI) handleScriptUpload(w http.ResponseWriter, r *http.Request) {
	if a.deps.Analyzer == nil {
		http.Error(w, "剧本分析器未初始化", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "解析上传失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "缺少文件: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 50<<20))
	if err != nil {
		http.Error(w, "读取失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	task := a.newTask()
	go a.runScriptAnalysis(task, data, header.Filename)
	writeJSON(w, map[string]string{"task_id": task.ID})
}

// newTask 创建异步任务。
func (a *adminAPI) newTask() *taskStatus {
	a.tasksMu.Lock()
	defer a.tasksMu.Unlock()
	id := fmt.Sprintf("task_%d", time.Now().UnixNano())
	t := &taskStatus{ID: id, Stage: "pending", UpdatedAt: time.Now().Format("15:04:05")}
	a.tasks[id] = t
	return t
}

// updateTask 更新任务状态。
func (a *adminAPI) updateTask(id, stage, message string) {
	a.tasksMu.Lock()
	defer a.tasksMu.Unlock()
	if t, ok := a.tasks[id]; ok {
		t.Stage = stage
		t.Message = message
		t.UpdatedAt = time.Now().Format("15:04:05")
	}
}

// finishTask 结束任务。
func (a *adminAPI) finishTask(id, errMsg string) {
	a.tasksMu.Lock()
	defer a.tasksMu.Unlock()
	if t, ok := a.tasks[id]; ok {
		t.Done = true
		t.Error = errMsg
		t.UpdatedAt = time.Now().Format("15:04:05")
	}
}

// runScriptAnalysis 异步执行剧本分析。
func (a *adminAPI) runScriptAnalysis(task *taskStatus, data []byte, filename string) {
	a.updateTask(task.ID, "parsing", "正在解析文件...")

	text, err := script.ParseFromBytes(data, filename)
	if err != nil {
		a.finishTask(task.ID, "文件解析失败: "+err.Error())
		return
	}

	progress := func(stage, message string) {
		a.updateTask(task.ID, stage, message)
	}

	scr, err := a.deps.Analyzer.Analyze(context.Background(), text, filename, progress)
	if err != nil {
		a.finishTask(task.ID, "AI 分析失败: "+err.Error())
		return
	}

	if err := a.deps.Archive.Save(scr); err != nil {
		a.finishTask(task.ID, "保存失败: "+err.Error())
		return
	}

	a.updateTask(task.ID, "done", fmt.Sprintf("识别完成: %s | 节点 %d | 角色 %d",
		scr.Title, len(scr.Timeline), len(scr.Characters)))
	a.finishTask(task.ID, "")
}

func (a *adminAPI) handleTask(w http.ResponseWriter, r *http.Request) {
	a.tasksMu.Lock()
	t, ok := a.tasks[r.PathValue("id")]
	a.tasksMu.Unlock()
	if !ok {
		http.Error(w, "任务不存在", http.StatusNotFound)
		return
	}
	writeJSON(w, t)
}

// ============================================================
// 角色卡管理
// ============================================================

func (a *adminAPI) handleCharacters(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.deps.CharMgr.ListAll())
}

// handleCharacterCreate 新建角色卡：name/system 必填，system ∈ {coc7, dnd5e}。
// NPC 卡的 player 约定传 "npc:{scriptID}"（见《管理后台扩展设计.md》2.3）。
func (a *adminAPI) handleCharacterCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string         `json:"name"`
		Player    string         `json:"player"`
		System    string         `json:"system"`
		Attrs     map[string]int `json:"attrs"`
		Skills    map[string]int `json:"skills"`
		Status    map[string]int `json:"status"`
		Backstory string         `json:"backstory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.System == "" {
		http.Error(w, "名称（name）与规则集（system）不能为空", http.StatusBadRequest)
		return
	}
	if req.System != "coc7" && req.System != "dnd5e" {
		http.Error(w, "规则集（system）必须是 coc7 或 dnd5e", http.StatusBadRequest)
		return
	}

	card := &character.Card{
		Name:      req.Name,
		Player:    req.Player,
		System:    req.System,
		Attrs:     req.Attrs,
		Skills:    req.Skills,
		Status:    req.Status,
		Backstory: req.Backstory,
	}
	if card.Attrs == nil {
		card.Attrs = map[string]int{}
	}
	if card.Skills == nil {
		card.Skills = map[string]int{}
	}
	if card.Status == nil {
		card.Status = map[string]int{}
	}
	if err := a.deps.CharMgr.Create(card); err != nil {
		if strings.Contains(err.Error(), "已存在") {
			http.Error(w, "创建失败: "+err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "创建失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, card)
}

// handleCharacterUpdate 修改角色卡：name/system/backstory 传了才改；
// 三个 map 传了（含空对象）即整体替换、不传（null）则不动；ID 不可变。
func (a *adminAPI) handleCharacterUpdate(w http.ResponseWriter, r *http.Request) {
	card, err := a.deps.CharMgr.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "角色卡不存在", http.StatusNotFound)
		return
	}

	var req struct {
		Name      string         `json:"name,omitempty"`
		System    string         `json:"system,omitempty"`
		Backstory string         `json:"backstory,omitempty"`
		Attrs     map[string]int `json:"attrs"`
		Skills    map[string]int `json:"skills"`
		Status    map[string]int `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.System != "" && req.System != "coc7" && req.System != "dnd5e" {
		http.Error(w, "规则集（system）必须是 coc7 或 dnd5e", http.StatusBadRequest)
		return
	}
	if req.Name != "" {
		card.Name = req.Name
	}
	if req.System != "" {
		card.System = req.System
	}
	if req.Backstory != "" {
		card.Backstory = req.Backstory
	}
	if req.Attrs != nil {
		card.Attrs = req.Attrs
	}
	if req.Skills != nil {
		card.Skills = req.Skills
	}
	if req.Status != nil {
		card.Status = req.Status
	}
	if err := a.deps.CharMgr.Update(card); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "已更新"})
}

// handleCharacterDelete 删除角色卡。
func (a *adminAPI) handleCharacterDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.deps.CharMgr.Delete(r.PathValue("id")); err != nil {
		http.Error(w, "删除失败: "+err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"message": "已删除"})
}

// ============================================================
// 记忆查看
// ============================================================

func (a *adminAPI) handleMemory(w http.ResponseWriter, r *http.Request) {
	if a.deps.MemoryStore == nil {
		http.Error(w, "记忆存储不可用", http.StatusServiceUnavailable)
		return
	}
	entries, err := a.deps.MemoryStore.List(r.PathValue("world"), r.PathValue("entity"))
	if err != nil {
		http.Error(w, "读取失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, entries)
}

// ============================================================
// 聊天记录
// ============================================================

func (a *adminAPI) handleLogSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.deps.GameLogger.ListSessions())
}

func (a *adminAPI) handleLogs(w http.ResponseWriter, r *http.Request) {
	entries, err := a.deps.GameLogger.GetEntries(r.PathValue("sessionID"))
	if err != nil {
		http.Error(w, "读取失败: "+err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, entries)
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
