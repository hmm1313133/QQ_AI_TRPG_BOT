// Package web - 素材联动与游玩存档 API（《世界编辑器与素材联动设计.md》§四/§9.4）。
//
//   - GET  /api/admin/assets                        素材目录（素材库/人物卡/其他世界/剧本角色）
//   - GET|POST       /api/admin/assets/library      素材库列表/新建
//   - GET|PATCH|DELETE /api/admin/assets/library/{aid} 素材详情/编辑/删除
//   - POST /api/admin/worlds/{id}/assets/collect    收藏世界实体进素材库
//   - POST /api/admin/worlds/{id}/assets/import     素材导入世界（四类来源）
//   - GET|POST       /api/admin/worlds/{id}/saves   存档列表/新建
//   - POST /api/admin/worlds/{id}/saves/{sid}/restore 恢复存档（先自动备份当前进度）
//   - DELETE /api/admin/worlds/{id}/saves/{sid}     删除存档
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// registerAssetRoutes 注册素材/存档路由（registerRoutes 中调用）。
func (a *adminAPI) registerAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/assets", a.wrap(a.handleAssetsCatalog))
	mux.HandleFunc("GET /api/admin/assets/library", a.wrap(a.handleAssetList))
	mux.HandleFunc("POST /api/admin/assets/library", a.wrap(a.handleAssetCreate))
	mux.HandleFunc("POST /api/admin/assets/library/batch", a.wrap(a.handleAssetBatchCreate))
	mux.HandleFunc("GET /api/admin/assets/library/{aid}", a.wrap(a.handleAssetGet))
	mux.HandleFunc("PATCH /api/admin/assets/library/{aid}", a.wrap(a.handleAssetUpdate))
	mux.HandleFunc("DELETE /api/admin/assets/library/{aid}", a.wrap(a.handleAssetDelete))
	mux.HandleFunc("POST /api/admin/assets/parse", a.wrap(a.handleAssetParse))
	mux.HandleFunc("POST /api/admin/scripts/{id}/assets/collect", a.wrap(a.handleScriptAssetsCollect))
	mux.HandleFunc("POST /api/admin/worlds/{id}/assets/collect", a.wrap(a.handleAssetCollect))
	mux.HandleFunc("POST /api/admin/worlds/{id}/assets/import", a.wrap(a.handleAssetImport))
	mux.HandleFunc("GET /api/admin/worlds/{id}/saves", a.wrap(a.handleSaveList))
	mux.HandleFunc("POST /api/admin/worlds/{id}/saves", a.wrap(a.handleSaveCreate))
	mux.HandleFunc("POST /api/admin/worlds/{id}/saves/{sid}/restore", a.wrap(a.handleSaveRestore))
	mux.HandleFunc("DELETE /api/admin/worlds/{id}/saves/{sid}", a.wrap(a.handleSaveDelete))
}

// sqliteRepo 取世界引擎的 SQLite 仓储（存档功能依赖；非 SQLite 时 503）。
func (a *adminAPI) sqliteRepo(w http.ResponseWriter) *world.SQLiteRepository {
	repo, ok := a.deps.WorldEngine.Repo().(*world.SQLiteRepository)
	if !ok {
		http.Error(w, "该功能需要 SQLite 存储", http.StatusServiceUnavailable)
		return nil
	}
	return repo
}

// ============================================================
// 素材目录（§4.1）
// ============================================================

func (a *adminAPI) handleAssetsCatalog(w http.ResponseWriter, r *http.Request) {
	type cardInfo struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		System       string `json:"system"`
		Player       string `json:"player"`
		HasBackstory bool   `json:"has_backstory"`
	}
	type worldInfo struct {
		ID         string   `json:"id"`
		Mode       string   `json:"mode"`
		ScriptName string   `json:"script_name,omitempty"`
		Characters []string `json:"characters"`
		Locations  []string `json:"locations"`
		Items      []string `json:"items"`
		Factions   []string `json:"factions"`
	}
	type scriptInfo struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		Characters []string `json:"characters"`
	}

	catalog := map[string]any{
		"library": []any{},
		"cards":   []cardInfo{},
		"worlds":  []worldInfo{},
		"scripts": []scriptInfo{},
	}

	// 素材库（不含 payload）
	if a.deps.AssetStore != nil {
		if list, err := a.deps.AssetStore.List("", "", ""); err == nil {
			catalog["library"] = list
		}
	}

	// 全局人物卡
	if a.deps.CharMgr != nil {
		var cards []cardInfo
		for _, c := range a.deps.CharMgr.ListAll() {
			cards = append(cards, cardInfo{
				ID: c.ID, Name: c.Name, System: c.System, Player: c.Player,
				HasBackstory: c.Backstory != "",
			})
		}
		catalog["cards"] = cards
	}

	// 各世界实体名（素材选择器的复制来源；世界量级小，逐个加载可接受）
	if ids, err := a.deps.WorldEngine.ListWorlds(); err == nil {
		var worlds []worldInfo
		for _, id := range ids {
			ws := a.deps.WorldEngine.LoadOrNil(id)
			if ws == nil {
				continue
			}
			wi := worldInfo{ID: id, Mode: ws.Mode, ScriptName: ws.ScriptName}
			for name := range ws.Characters {
				wi.Characters = append(wi.Characters, name)
			}
			for name := range ws.Locations {
				wi.Locations = append(wi.Locations, name)
			}
			for name := range ws.Items {
				wi.Items = append(wi.Items, name)
			}
			for name := range ws.Factions {
				wi.Factions = append(wi.Factions, name)
			}
			worlds = append(worlds, wi)
		}
		catalog["worlds"] = worlds
	}

	// 剧本角色
	if a.deps.Archive != nil {
		var scripts []scriptInfo
		for _, s := range a.deps.Archive.List() {
			si := scriptInfo{ID: s.ID, Name: s.Name}
			for _, ch := range s.Characters {
				si.Characters = append(si.Characters, ch.Name)
			}
			scripts = append(scripts, si)
		}
		catalog["scripts"] = scripts
	}

	writeJSON(w, catalog)
}

// ============================================================
// 素材库 CRUD（§4.1.1）
// ============================================================

func (a *adminAPI) handleAssetList(w http.ResponseWriter, r *http.Request) {
	if a.deps.AssetStore == nil {
		http.Error(w, "素材库不可用", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	list, err := a.deps.AssetStore.List(q.Get("kind"), q.Get("q"), q.Get("tag"))
	if err != nil {
		http.Error(w, "查询失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

type assetUpsertReq struct {
	Kind    string          `json:"kind"`
	Name    string          `json:"name"`
	Tags    []string        `json:"tags"`
	Summary string          `json:"summary"`
	Source  string          `json:"source"`
	Payload json.RawMessage `json:"payload"`
}

func (a *adminAPI) handleAssetCreate(w http.ResponseWriter, r *http.Request) {
	if a.deps.AssetStore == nil {
		http.Error(w, "素材库不可用", http.StatusServiceUnavailable)
		return
	}
	var req assetUpsertReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	asset := &world.Asset{
		Kind: req.Kind, Name: strings.TrimSpace(req.Name), Tags: req.Tags,
		Summary: req.Summary, Source: req.Source, Payload: req.Payload,
	}
	if asset.Source == "" {
		asset.Source = "手动创建"
	}
	if err := a.deps.AssetStore.Create(asset); err != nil {
		http.Error(w, "创建失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, asset)
}

func (a *adminAPI) handleAssetGet(w http.ResponseWriter, r *http.Request) {
	asset, err := a.deps.AssetStore.Get(r.PathValue("aid"))
	if err != nil {
		http.Error(w, "素材不存在", http.StatusNotFound)
		return
	}
	writeJSON(w, asset)
}

func (a *adminAPI) handleAssetUpdate(w http.ResponseWriter, r *http.Request) {
	asset, err := a.deps.AssetStore.Get(r.PathValue("aid"))
	if err != nil {
		http.Error(w, "素材不存在", http.StatusNotFound)
		return
	}
	var req assetUpsertReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Kind != "" {
		asset.Kind = req.Kind
	}
	if strings.TrimSpace(req.Name) != "" {
		asset.Name = strings.TrimSpace(req.Name)
	}
	if req.Tags != nil {
		asset.Tags = req.Tags
	}
	if req.Summary != "" {
		asset.Summary = req.Summary
	}
	if req.Source != "" {
		asset.Source = req.Source
	}
	if len(req.Payload) > 0 {
		asset.Payload = req.Payload
	}
	if err := a.deps.AssetStore.Update(asset); err != nil {
		http.Error(w, "更新失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, asset)
}

func (a *adminAPI) handleAssetDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.deps.AssetStore.Delete(r.PathValue("aid")); err != nil {
		http.Error(w, "删除失败: "+err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"message": "已删除"})
}

// ============================================================
// 收藏世界实体进素材库（§4.1.1 collect）
// ============================================================

type assetCollectReq struct {
	Kind    string   `json:"kind"`   // character / location / item / faction / storyline / world
	Name    string   `json:"name"`   // 实体名（storyline/world 忽略）
	Tags    []string `json:"tags"`   // 可选
	Summary string   `json:"summary"`
}

func (a *adminAPI) handleAssetCollect(w http.ResponseWriter, r *http.Request) {
	if a.deps.AssetStore == nil {
		http.Error(w, "素材库不可用", http.StatusServiceUnavailable)
		return
	}
	var req assetCollectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !world.AssetKinds[req.Kind] {
		http.Error(w, "未知素材类型: "+req.Kind, http.StatusBadRequest)
		return
	}
	worldID := r.PathValue("id")
	ws := a.deps.WorldEngine.LoadOrNil(worldID)
	if ws == nil {
		http.Error(w, "世界不存在", http.StatusNotFound)
		return
	}

	payload, name, err := extractEntityPayload(ws, req.Kind, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	asset := &world.Asset{
		Kind: req.Kind, Name: name, Tags: req.Tags, Summary: req.Summary,
		Source:  fmt.Sprintf("收藏自世界 %s", worldID),
		Payload: payload,
	}
	if err := a.deps.AssetStore.Create(asset); err != nil {
		http.Error(w, "收藏失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, asset)
}

// extractEntityPayload 从世界状态取出实体并序列化为素材 payload。
func extractEntityPayload(ws *world.WorldState, kind, name string) (json.RawMessage, string, error) {
	var v any
	outName := name
	switch kind {
	case "character":
		c, ok := ws.Characters[name]
		if !ok {
			return nil, "", fmt.Errorf("角色不存在: %s", name)
		}
		v = c
	case "location":
		l, ok := ws.Locations[name]
		if !ok {
			return nil, "", fmt.Errorf("地点不存在: %s", name)
		}
		v = l
	case "item":
		it, ok := ws.Items[name]
		if !ok {
			return nil, "", fmt.Errorf("物品不存在: %s", name)
		}
		v = it
	case "faction":
		f, ok := ws.Factions[name]
		if !ok {
			return nil, "", fmt.Errorf("势力不存在: %s", name)
		}
		v = f
	case "storyline":
		if ws.Storyline == nil {
			return nil, "", fmt.Errorf("该世界没有主线剧情")
		}
		v = ws.Storyline
		outName = ws.Storyline.Title
	case "world":
		// 世界观素材（设计 §11.3）：背景 + 手工设定条目
		wv := world.WorldviewFromState(ws)
		if wv.Empty() {
			return nil, "", fmt.Errorf("该世界没有可收藏的世界观（背景与手工设定均为空）")
		}
		v = wv
		outName = ws.ScriptName
		if outName == "" {
			outName = ws.WorldID
		}
		outName += " 的世界观"
	default:
		return nil, "", fmt.Errorf("未知素材类型: %s", kind)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, "", fmt.Errorf("序列化实体失败: %w", err)
	}
	return data, outName, nil
}

// ============================================================
// 素材导入（§4.2）
// ============================================================

type assetImportReq struct {
	Library          []string             `json:"library"` // 素材库条目 ID
	Cards            []string             `json:"cards"`   // 全局人物卡 ID（CardRef 关联）
	Copy             []assetImportCopyRef `json:"copy"`    // 跨世界复制
	ScriptCharacters []assetImportScriptRef `json:"script_characters"`
	OnConflict       string               `json:"on_conflict"` // skip（默认）/ overwrite
}

type assetImportCopyRef struct {
	WorldID string `json:"world_id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
}

type assetImportScriptRef struct {
	ScriptID string `json:"script_id"`
	Name     string `json:"name"`
}

type assetImportResult struct {
	Imported  int      `json:"imported"`
	Conflicts []string `json:"conflicts,omitempty"`
	Errors    []string `json:"errors,omitempty"`
}

func (a *adminAPI) handleAssetImport(w http.ResponseWriter, r *http.Request) {
	var req assetImportReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.OnConflict == "" {
		req.OnConflict = "skip"
	}
	if req.OnConflict != "skip" && req.OnConflict != "overwrite" {
		http.Error(w, "on_conflict 仅支持 skip / overwrite", http.StatusBadRequest)
		return
	}

	ws := a.loadWorldForEdit(w, r.PathValue("id"))
	if ws == nil {
		return
	}
	defer a.deps.WorldEngine.Unlock(ws.WorldID)

	res := &assetImportResult{}

	// 预校验：人物卡 / 素材库条目 / 剧本角色全部能解析，避免半导入
	type pendingChar struct {
		cs    *world.CharacterState
		label string
		merge bool // 人物卡关联：同名补字段而非冲突
	}
	var pendingChars []pendingChar
	type pendingEntity struct {
		kind  string
		label string
		value any // *world.Location / *world.Item / *world.Faction / *world.Storyline / *world.Worldview
	}
	var pendingEntities []pendingEntity

	for _, assetID := range req.Library {
		asset, err := a.deps.AssetStore.Get(assetID)
		if err != nil {
			res.Errors = append(res.Errors, "素材不存在: "+assetID)
			continue
		}
		switch asset.Kind {
		case "character":
			var c world.CharacterState
			if err := json.Unmarshal(asset.Payload, &c); err != nil || c.Name == "" {
				res.Errors = append(res.Errors, "素材角色数据无效: "+asset.Name)
				continue
			}
			pendingChars = append(pendingChars, pendingChar{cs: &c, label: "素材:" + asset.Name})
		case "location":
			var v world.Location
			if err := json.Unmarshal(asset.Payload, &v); err == nil && v.Name != "" {
				pendingEntities = append(pendingEntities, pendingEntity{"location", "素材:" + asset.Name, &v})
			}
		case "item":
			var v world.Item
			if err := json.Unmarshal(asset.Payload, &v); err == nil && v.Name != "" {
				pendingEntities = append(pendingEntities, pendingEntity{"item", "素材:" + asset.Name, &v})
			}
		case "faction":
			var v world.Faction
			if err := json.Unmarshal(asset.Payload, &v); err == nil && v.Name != "" {
				pendingEntities = append(pendingEntities, pendingEntity{"faction", "素材:" + asset.Name, &v})
			}
		case "storyline":
			var v world.Storyline
			if err := json.Unmarshal(asset.Payload, &v); err == nil && v.Title != "" {
				pendingEntities = append(pendingEntities, pendingEntity{"storyline", "素材:" + asset.Name, &v})
			}
		case "world":
			var v world.Worldview
			if err := json.Unmarshal(asset.Payload, &v); err == nil && !v.Empty() {
				pendingEntities = append(pendingEntities, pendingEntity{"world", "素材:" + asset.Name, &v})
			}
		}
	}

	for _, cardID := range req.Cards {
		card, err := a.deps.CharMgr.Get(cardID)
		if err != nil {
			res.Errors = append(res.Errors, "人物卡不存在: "+cardID)
			continue
		}
		pendingChars = append(pendingChars, pendingChar{
			cs: world.CharacterStateFromCard(card), label: "人物卡:" + card.Name, merge: true,
		})
	}

	for _, ref := range req.Copy {
		src := a.deps.WorldEngine.LoadOrNil(ref.WorldID)
		if src == nil {
			res.Errors = append(res.Errors, "来源世界不存在: "+ref.WorldID)
			continue
		}
		payload, name, err := extractEntityPayload(src, ref.Kind, ref.Name)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", ref.WorldID, err))
			continue
		}
		switch ref.Kind {
		case "character":
			var c world.CharacterState
			if json.Unmarshal(payload, &c) == nil {
				pendingChars = append(pendingChars, pendingChar{cs: &c, label: "复制:" + name})
			}
		case "location":
			var v world.Location
			if json.Unmarshal(payload, &v) == nil {
				pendingEntities = append(pendingEntities, pendingEntity{"location", "复制:" + name, &v})
			}
		case "item":
			var v world.Item
			if json.Unmarshal(payload, &v) == nil {
				pendingEntities = append(pendingEntities, pendingEntity{"item", "复制:" + name, &v})
			}
		case "faction":
			var v world.Faction
			if json.Unmarshal(payload, &v) == nil {
				pendingEntities = append(pendingEntities, pendingEntity{"faction", "复制:" + name, &v})
			}
		case "storyline":
			var v world.Storyline
			if json.Unmarshal(payload, &v) == nil {
				pendingEntities = append(pendingEntities, pendingEntity{"storyline", "复制:" + name, &v})
			}
		case "world":
			var v world.Worldview
			if json.Unmarshal(payload, &v) == nil {
				pendingEntities = append(pendingEntities, pendingEntity{"world", "复制:" + name, &v})
			}
		}
	}

	for _, ref := range req.ScriptCharacters {
		scr, err := a.deps.Archive.Get(ref.ScriptID)
		if err != nil {
			res.Errors = append(res.Errors, "剧本不存在: "+ref.ScriptID)
			continue
		}
		found := false
		for _, ch := range scr.Characters {
			if ch.Name == ref.Name {
				pendingChars = append(pendingChars, pendingChar{
					cs: world.CharacterFromScript(ch), label: "剧本:" + ref.Name,
				})
				found = true
				break
			}
		}
		if !found {
			res.Errors = append(res.Errors, fmt.Sprintf("剧本 %s 中无角色 %s", scr.Name, ref.Name))
		}
	}

	// 应用：角色（人物卡关联走合并，其余按冲突策略）
	for _, p := range pendingChars {
		if existing, ok := ws.Characters[p.cs.Name]; ok {
			if p.merge {
				world.MergeCharacterState(existing, p.cs)
				res.Imported++
				continue
			}
			if req.OnConflict == "overwrite" {
				p.cs.Alive = true
				ws.Characters[p.cs.Name] = p.cs
				res.Imported++
			} else {
				res.Conflicts = append(res.Conflicts, p.cs.Name+"（角色已存在，已跳过）")
			}
			continue
		}
		if p.cs.Kind == "" {
			p.cs.Kind = "npc"
		}
		if p.cs.Disposition == "" {
			p.cs.Disposition = "neutral"
		}
		p.cs.Alive = true
		ws.Characters[p.cs.Name] = p.cs
		res.Imported++
	}

	applyEntity := func(kind, label string, exists bool, set func()) {
		if exists && req.OnConflict != "overwrite" {
			res.Conflicts = append(res.Conflicts, label+"（已存在，已跳过）")
			return
		}
		set()
		res.Imported++
	}
	for _, p := range pendingEntities {
		switch p.kind {
		case "location":
			v := p.value.(*world.Location)
			_, exists := ws.Locations[v.Name]
			applyEntity("location", v.Name, exists, func() { ws.Locations[v.Name] = v })
		case "item":
			v := p.value.(*world.Item)
			_, exists := ws.Items[v.Name]
			applyEntity("item", v.Name, exists, func() { ws.Items[v.Name] = v })
		case "faction":
			v := p.value.(*world.Faction)
			_, exists := ws.Factions[v.Name]
			applyEntity("faction", v.Name, exists, func() { ws.Factions[v.Name] = v })
		case "storyline":
			v := p.value.(*world.Storyline)
			applyEntity("storyline", v.Title, ws.Storyline != nil, func() { ws.Storyline = v })
		case "world":
			v := p.value.(*world.Worldview)
			exists := strings.TrimSpace(ws.Background) != ""
			applyEntity("world", p.label, exists, func() { world.ApplyWorldview(ws, v, true) })
		}
	}

	if res.Imported == 0 && len(res.Errors) == 0 && len(res.Conflicts) == 0 {
		http.Error(w, "没有可导入的素材", http.StatusBadRequest)
		return
	}
	if res.Imported > 0 {
		if err := a.saveWithAuditNote(ws, "assets:import",
			fmt.Sprintf("管理端导入素材 %d 项", res.Imported)); err != nil {
			http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, res)
}

// ============================================================
// 游玩存档（§9.4）
// ============================================================

func (a *adminAPI) handleSaveList(w http.ResponseWriter, r *http.Request) {
	repo := a.sqliteRepo(w)
	if repo == nil {
		return
	}
	list, err := repo.ListSaves(r.PathValue("id"))
	if err != nil {
		http.Error(w, "查询失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

type saveCreateReq struct {
	Name string `json:"name"`
	Note string `json:"note"`
}

func (a *adminAPI) handleSaveCreate(w http.ResponseWriter, r *http.Request) {
	repo := a.sqliteRepo(w)
	if repo == nil {
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
	ws := a.deps.WorldEngine.LoadOrNil(r.PathValue("id"))
	if ws == nil {
		http.Error(w, "世界不存在", http.StatusNotFound)
		return
	}
	info, err := repo.CreateSave(ws, req.Name, req.Note, false)
	if err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, info)
}

// handleSaveRestore 恢复存档：先自动备份当前进度，再用快照覆盖（世界锁内）。
func (a *adminAPI) handleSaveRestore(w http.ResponseWriter, r *http.Request) {
	repo := a.sqliteRepo(w)
	if repo == nil {
		return
	}
	var saveID int64
	if _, err := fmt.Sscanf(r.PathValue("sid"), "%d", &saveID); err != nil {
		http.Error(w, "存档 ID 无效", http.StatusBadRequest)
		return
	}
	info, history, err := restoreWorldSave(a.deps.WorldEngine, r.PathValue("id"), saveID, "admin")
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "不存在") || strings.Contains(err.Error(), "不属于") || strings.Contains(err.Error(), "无效") {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	// 存档带对话历史快照时同步回放（仅 Web 会话世界有历史；QQ 创建的档为 NULL 跳过）
	replaySaveHistory(a.history, r.PathValue("id"), history)
	writeJSON(w, map[string]string{"message": fmt.Sprintf("已恢复到存档「%s」（轮次 %d）", info.Name, info.RoundCount)})
}

// restoreWorldSave 恢复存档的共享薄封装（管理端/玩家端共用）：
// 核心逻辑（加锁/归属校验/自动备份/写回）在 world.Engine.RestoreSave，
// 这里保留原有返回形态并带出对话历史快照（回放由调用方决定）。
func restoreWorldSave(engine *world.Engine, worldID string, saveID int64, actor string) (*world.SaveInfo, []byte, error) {
	return engine.RestoreSave(worldID, saveID, actor)
}

func (a *adminAPI) handleSaveDelete(w http.ResponseWriter, r *http.Request) {
	repo := a.sqliteRepo(w)
	if repo == nil {
		return
	}
	var saveID int64
	if _, err := fmt.Sscanf(r.PathValue("sid"), "%d", &saveID); err != nil {
		http.Error(w, "存档 ID 无效", http.StatusBadRequest)
		return
	}
	if err := repo.DeleteSave(saveID); err != nil {
		http.Error(w, "删除失败: "+err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"message": "已删除"})
}
