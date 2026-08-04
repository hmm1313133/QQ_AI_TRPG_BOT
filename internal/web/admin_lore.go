// 管理后台：世界设定库（lore）与分区编辑 API（设计文档《世界设定库与按需加载设计.md》§4.4）。
//
// 与 /state 的 ApplyEvent 受控修改不同，本文件的写接口属于"GM 修正"：
// Load → 直改字段 → Save，并统一追加一条 note 事件进 EventLog 作为审计痕迹。
// ApplyEvent 仍是游戏运行时的唯一写入口，此约束不变。
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// loreUpsertReq 新增/更新设定条目请求。
// Priority/Enabled 用指针以区分"未传"与显式零值。
type loreUpsertReq struct {
	Title         string   `json:"title"`
	Category      string   `json:"category"`
	Keys          []string `json:"keys"`
	SecondaryKeys []string `json:"secondary_keys"`
	SecondaryMode string   `json:"secondary_mode"`
	Constant      bool     `json:"constant"`
	Position      string   `json:"position"`
	Priority      *int     `json:"priority"`
	Enabled       *bool    `json:"enabled"`
	Content       string   `json:"content"`
}

// validate 校验必填字段（Title/Content）。
func (r *loreUpsertReq) validate() error {
	if strings.TrimSpace(r.Title) == "" {
		return fmt.Errorf("标题（title）不能为空")
	}
	if strings.TrimSpace(r.Content) == "" {
		return fmt.Errorf("正文（content）不能为空")
	}
	return nil
}

// applyTo 把请求字段写入条目。isCreate=true 时未传字段填默认值
// （Category=background / Position=front / Priority=50 / Enabled=true）；
// 更新时未传的指针字段保留原值。
func (r *loreUpsertReq) applyTo(e *world.LoreEntry, isCreate bool) {
	e.Title = strings.TrimSpace(r.Title)
	e.Content = r.Content
	e.Keys = r.Keys
	e.SecondaryKeys = r.SecondaryKeys
	e.SecondaryMode = r.SecondaryMode
	e.Constant = r.Constant
	if r.Category != "" {
		e.Category = r.Category
	} else if isCreate {
		e.Category = world.LoreCategoryBackground
	}
	if r.Position != "" {
		e.Position = r.Position
	} else if isCreate {
		e.Position = world.LorePositionFront
	}
	if r.Priority != nil {
		e.Priority = *r.Priority
	} else if isCreate {
		e.Priority = 50
	}
	if r.Enabled != nil {
		e.Enabled = *r.Enabled
	} else if isCreate {
		e.Enabled = true
	}
}

// newLoreID 生成世界内唯一的条目 ID（lor_ 前缀）。
func newLoreID() string {
	return fmt.Sprintf("lor_%d", time.Now().UnixNano())
}

// saveWithAuditNote 保存世界并追加一条 note 审计事件（GM 修正留痕，设计文档 §4.4）。
func (a *adminAPI) saveWithAuditNote(ws *world.WorldState, target, value string) error {
	if _, err := a.deps.WorldEngine.ApplyEvent(ws, world.WorldEvent{
		Type: "note", Actor: "admin", Target: target, Value: value,
	}); err != nil {
		return err
	}
	return a.deps.WorldEngine.Save(ws)
}

// loadWorldForEdit 加载世界并加写锁（调用方负责 Unlock）；不存在时写 404 并返回 nil。
func (a *adminAPI) loadWorldForEdit(w http.ResponseWriter, id string) *world.WorldState {
	a.deps.WorldEngine.Lock(id)
	ws := a.deps.WorldEngine.LoadOrNil(id)
	if ws == nil {
		a.deps.WorldEngine.Unlock(id)
		http.Error(w, "世界不存在", http.StatusNotFound)
		return nil
	}
	return ws
}

// ============================================================
// lore 条目 CRUD
// ============================================================

// handleLoreList 条目列表，支持 ?category=&enabled= 过滤。
func (a *adminAPI) handleLoreList(w http.ResponseWriter, r *http.Request) {
	ws := a.deps.WorldEngine.LoadOrNil(r.PathValue("id"))
	if ws == nil {
		http.Error(w, "世界不存在", http.StatusNotFound)
		return
	}
	category := r.URL.Query().Get("category")
	enabledFilter := r.URL.Query().Get("enabled")
	list := make([]world.LoreEntry, 0, len(ws.Lore))
	for _, e := range ws.Lore {
		if category != "" && e.Category != category {
			continue
		}
		if enabledFilter != "" && fmt.Sprintf("%t", e.Enabled) != enabledFilter {
			continue
		}
		list = append(list, e)
	}
	writeJSON(w, list)
}

// handleLoreCreate 新增条目（ID 服务端生成，Source 强制 manual）。
func (a *adminAPI) handleLoreCreate(w http.ResponseWriter, r *http.Request) {
	var req loreUpsertReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := req.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ws := a.loadWorldForEdit(w, r.PathValue("id"))
	if ws == nil {
		return
	}
	defer a.deps.WorldEngine.Unlock(ws.WorldID)

	entry := world.LoreEntry{ID: newLoreID(), Source: world.LoreSourceManual}
	req.applyTo(&entry, true)
	ws.Lore = append(ws.Lore, entry)
	if err := a.saveWithAuditNote(ws, "lore:"+entry.ID, "管理端新增设定条目《"+entry.Title+"》"); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, entry)
}

// handleLoreUpdate 更新条目（ID/Source 不可变）。
func (a *adminAPI) handleLoreUpdate(w http.ResponseWriter, r *http.Request) {
	var req loreUpsertReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := req.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ws := a.loadWorldForEdit(w, r.PathValue("id"))
	if ws == nil {
		return
	}
	defer a.deps.WorldEngine.Unlock(ws.WorldID)

	eid := r.PathValue("eid")
	for i := range ws.Lore {
		if ws.Lore[i].ID == eid {
			req.applyTo(&ws.Lore[i], false)
			if err := a.saveWithAuditNote(ws, "lore:"+eid, "管理端更新设定条目《"+ws.Lore[i].Title+"》"); err != nil {
				http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, ws.Lore[i])
			return
		}
	}
	http.Error(w, "条目不存在", http.StatusNotFound)
}

// handleLoreDelete 删除条目（script 来源条目允许删，Source 字段本身即追溯依据）。
func (a *adminAPI) handleLoreDelete(w http.ResponseWriter, r *http.Request) {
	ws := a.loadWorldForEdit(w, r.PathValue("id"))
	if ws == nil {
		return
	}
	defer a.deps.WorldEngine.Unlock(ws.WorldID)

	eid := r.PathValue("eid")
	for i := range ws.Lore {
		if ws.Lore[i].ID == eid {
			title := ws.Lore[i].Title
			ws.Lore = append(ws.Lore[:i], ws.Lore[i+1:]...)
			if err := a.saveWithAuditNote(ws, "lore:"+eid, "管理端删除设定条目《"+title+"》"); err != nil {
				http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]string{"message": "已删除"})
			return
		}
	}
	http.Error(w, "条目不存在", http.StatusNotFound)
}

// ============================================================
// 命中测试 / 注入记录 / 批量导入
// ============================================================

// loreRuntimeConfig 从 ConfigStore 读 lore 检索参数所需的最小接口（*config.Store 满足）。
type loreRuntimeConfig interface {
	GetInt(key string, def int) int
	GetBool(key string, def bool) bool
}

// handleLoreTest 命中测试：body {text} → 直接调 LoreResolver，返回命中条目+原因+预算占用+被裁条目。
func (a *adminAPI) handleLoreTest(w http.ResponseWriter, r *http.Request) {
	ws := a.deps.WorldEngine.LoadOrNil(r.PathValue("id"))
	if ws == nil {
		http.Error(w, "世界不存在", http.StatusNotFound)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	// budget/recursion 与运行时回合读取同一组配置键（热更新）
	budget := world.DefaultLoreBudget
	recursion := false
	if cs, ok := a.deps.ConfigStore.(loreRuntimeConfig); ok {
		budget = cs.GetInt(config.KeyLoreBudget, budget)
		recursion = cs.GetBool(config.KeyLoreRecursion, false)
	}
	res := world.Resolve(ws, req.Text, budget, recursion)
	writeJSON(w, map[string]interface{}{
		"budget":    budget,
		"recursion": recursion,
		"front":     res.Front,
		"tail":      res.Tail,
		"dropped":   res.Dropped,
	})
}

// handleLoreInjections 最近 N 回合的实际注入记录（转调 TurnEngine.RecentInjections）。
func (a *adminAPI) handleLoreInjections(w http.ResponseWriter, r *http.Request) {
	if a.deps.TurnEngine == nil {
		http.Error(w, "回合引擎不可用", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, a.deps.TurnEngine.RecentInjections(r.PathValue("id")))
}

// stLoreEntry SillyTavern lorebook 条目（导入映射用）。
type stLoreEntry struct {
	Name          string   `json:"name"`
	Comment       string   `json:"comment"`
	Keys          []string `json:"keys"`
	SecondaryKeys []string `json:"secondary_keys"`
	Constant      bool     `json:"constant"`
	Priority      *int     `json:"insertion_order"`
	Content       string   `json:"content"`
	Enabled       *bool    `json:"enabled"`
	Position      string   `json:"position"`
}

// handleLoreImport 批量导入，两种模式（设计文档 §4.4/§4.6 P3/P4）：
//  1. body {text}：大段文本按空行拆段落，每段成一条目草稿（Title=首行前 20 字，Keys 空）
//  2. body {entries: {...} 或 [...]}：SillyTavern lorebook JSON，按字段映射转换
func (a *adminAPI) handleLoreImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text    string          `json:"text"`
		Entries json.RawMessage `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}

	var drafts []world.LoreEntry
	switch {
	case strings.TrimSpace(req.Text) != "":
		drafts = importTextDrafts(req.Text)
	case len(req.Entries) > 0:
		var err error
		drafts, err = importSTLorebook(req.Entries)
		if err != nil {
			http.Error(w, "lorebook 解析失败: "+err.Error(), http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "请提供 text（段落拆分）或 entries（SillyTavern lorebook）", http.StatusBadRequest)
		return
	}
	if len(drafts) == 0 {
		http.Error(w, "未解析到有效条目", http.StatusBadRequest)
		return
	}

	ws := a.loadWorldForEdit(w, r.PathValue("id"))
	if ws == nil {
		return
	}
	defer a.deps.WorldEngine.Unlock(ws.WorldID)

	for i := range drafts {
		drafts[i].ID = newLoreID()
		drafts[i].Source = world.LoreSourceManual
		ws.Lore = append(ws.Lore, drafts[i])
	}
	if err := a.saveWithAuditNote(ws, "lore:import",
		fmt.Sprintf("管理端批量导入设定条目 %d 条", len(drafts))); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, drafts)
}

// importTextDrafts 大段文本按空行/段落拆成条目草稿：Title=首行前 20 字，Keys 空。
func importTextDrafts(text string) []world.LoreEntry {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var drafts []world.LoreEntry
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		title := para
		if i := strings.IndexByte(para, '\n'); i >= 0 {
			title = para[:i]
		}
		if r := []rune(title); len(r) > 20 {
			title = string(r[:20])
		}
		drafts = append(drafts, world.LoreEntry{
			Title:    title,
			Category: world.LoreCategoryBackground,
			Position: world.LorePositionFront,
			Priority: 50,
			Enabled:  true,
			Content:  para,
		})
	}
	return drafts
}

// importSTLorebook 解析 SillyTavern lorebook JSON（entries 支持对象与数组两种形态）。
func importSTLorebook(raw json.RawMessage) ([]world.LoreEntry, error) {
	var list []stLoreEntry
	// 先试对象形态（ST 官方格式：uid -> entry）
	var obj map[string]stLoreEntry
	if err := json.Unmarshal(raw, &obj); err == nil && obj != nil {
		for _, e := range obj {
			list = append(list, e)
		}
	} else {
		// 再试数组形态
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
	}
	var drafts []world.LoreEntry
	for _, e := range list {
		if strings.TrimSpace(e.Content) == "" {
			continue // 无正文的条目无意义，跳过
		}
		title := e.Name
		if title == "" {
			title = e.Comment
		}
		if title == "" {
			title = firstRunes(e.Content, 20)
		}
		entry := world.LoreEntry{
			Title:         title,
			Category:      world.LoreCategoryBackground,
			Keys:          e.Keys,
			SecondaryKeys: e.SecondaryKeys,
			Constant:      e.Constant,
			Position:      world.LorePositionFront, // position 默认 front
			Priority:      50,
			Enabled:       true,
			Content:       e.Content,
		}
		if e.Priority != nil {
			entry.Priority = *e.Priority
		}
		if e.Enabled != nil {
			entry.Enabled = *e.Enabled
		}
		if e.Position == world.LorePositionTail {
			entry.Position = world.LorePositionTail
		}
		drafts = append(drafts, entry)
	}
	return drafts, nil
}

// firstRunes 取字符串前 n 个字符。
func firstRunes(s string, n int) string {
	if r := []rune(strings.TrimSpace(s)); len(r) > n {
		return string(r[:n])
	}
	return strings.TrimSpace(s)
}

// ============================================================
// 分区编辑（编辑器数据源；全量 detail 保留给 JSON 页签）
// ============================================================

// sectionParts 分区白名单。
var sectionParts = map[string]bool{
	"scene": true, "characters": true, "locations": true,
	"factions": true, "quests": true, "hidden": true, "metrics": true,
}

// handleSectionGet 按需返回指定分区 JSON。
func (a *adminAPI) handleSectionGet(w http.ResponseWriter, r *http.Request) {
	ws := a.deps.WorldEngine.LoadOrNil(r.PathValue("id"))
	if ws == nil {
		http.Error(w, "世界不存在", http.StatusNotFound)
		return
	}
	switch part := r.URL.Query().Get("part"); part {
	case "scene":
		writeJSON(w, ws.Scene)
	case "characters":
		writeJSON(w, ws.Characters)
	case "locations":
		writeJSON(w, ws.Locations)
	case "factions":
		writeJSON(w, ws.Factions)
	case "quests":
		writeJSON(w, ws.Quests)
	case "hidden":
		writeJSON(w, ws.HiddenInfo)
	case "metrics":
		writeJSON(w, ws.Metrics)
	default:
		http.Error(w, "未知分区: "+part, http.StatusBadRequest)
	}
}

// handleSectionPatch 按 part 把 data 反序列化到对应字段后 Save + note 审计事件。
func (a *adminAPI) handleSectionPatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Part string          `json:"part"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !sectionParts[req.Part] {
		http.Error(w, "未知分区: "+req.Part, http.StatusBadRequest)
		return
	}
	if len(req.Data) == 0 {
		http.Error(w, "缺少 data", http.StatusBadRequest)
		return
	}

	ws := a.loadWorldForEdit(w, r.PathValue("id"))
	if ws == nil {
		return
	}
	defer a.deps.WorldEngine.Unlock(ws.WorldID)

	if err := applySection(ws, req.Part, req.Data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.saveWithAuditNote(ws, "section:"+req.Part, "管理端编辑分区 "+req.Part); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "已应用"})
}

// applySection 把 data 反序列化到 WorldState 对应分区字段（含基本校验）。
func applySection(ws *world.WorldState, part string, data json.RawMessage) error {
	switch part {
	case "scene":
		var v world.SceneState
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("scene 数据格式错误: %w", err)
		}
		if strings.TrimSpace(v.NodeName) == "" {
			return fmt.Errorf("scene.node_name 不能为空")
		}
		ws.Scene = v
	case "characters":
		var v map[string]*world.CharacterState
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("characters 数据格式错误: %w", err)
		}
		for name, c := range v {
			if c == nil {
				return fmt.Errorf("角色 %s 不能为 null", name)
			}
			if c.Name == "" {
				c.Name = name // map 键即权威名称
			}
		}
		ws.Characters = v
	case "locations":
		var v map[string]*world.Location
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("locations 数据格式错误: %w", err)
		}
		ws.Locations = v
	case "factions":
		var v map[string]*world.Faction
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("factions 数据格式错误: %w", err)
		}
		ws.Factions = v
	case "quests":
		var v []world.QuestState
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("quests 数据格式错误: %w", err)
		}
		ws.Quests = v
	case "hidden":
		var v []world.HiddenItem
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("hidden 数据格式错误: %w", err)
		}
		ws.HiddenInfo = v
	case "metrics":
		var v world.Metrics
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("metrics 数据格式错误: %w", err)
		}
		ws.Metrics = v
	}
	return nil
}
