// Package web - 素材解析与剧本素材收藏 API（《世界编辑器与素材联动设计.md》§11）。
//
//   - POST /api/admin/assets/parse                  文本/文件解析为素材草稿（不落库）
//   - POST /api/admin/assets/library/batch          批量入库（解析草稿确认后）
package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/assetparse"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// ============================================================
// 素材解析（§11.4）
// ============================================================

// parseMaxBytes 上传文件大小上限（20MB）。
const parseMaxBytes = 20 << 20

type assetParseTextReq struct {
	Text string `json:"text"`
}

// handleAssetParse 解析文本/文件为素材草稿（不落库，前端确认后走 batch 入库）。
// 支持：multipart 文件（.png/.json 先走 SillyTavern 卡程序化解码；
// 解码成功且有 LLM 时走 ParseCard 混合解析（LLM 整理回写字段），否则纯程序化直映；
// .txt/.md 直接走 LLM）或 JSON {"text": "..."}（直接 LLM）。
func (a *adminAPI) handleAssetParse(w http.ResponseWriter, r *http.Request) {
	var filename string
	var data []byte

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, parseMaxBytes)
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "读取上传文件失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		filename = header.Filename
		if data, err = io.ReadAll(io.LimitReader(file, parseMaxBytes)); err != nil {
			http.Error(w, "读取文件内容失败: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		var req assetParseTextReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
			return
		}
		data = []byte(req.Text)
	}
	if len(data) == 0 {
		http.Error(w, "内容为空", http.StatusBadRequest)
		return
	}

	// 1) 角色卡路径：SillyTavern 角色卡（PNG 内嵌 / JSON）程序化解码，
	//    有 LLM 时走混合解析整理内容，否则纯程序化直映
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".png") || hasPNGSignature(data) {
		card := assetparse.DecodeCardPNG(data)
		if card == nil {
			http.Error(w, "该 PNG 中未找到角色卡数据（需要含 chara/ccv3 文本块的 SillyTavern 角色卡）", http.StatusBadRequest)
			return
		}
		a.writeCardDrafts(w, r, card)
		return
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasSuffix(lower, ".json") || strings.HasPrefix(trimmed, "{") {
		if card := assetparse.DecodeCardJSON(data); card != nil {
			a.writeCardDrafts(w, r, card)
			return
		}
		// 不是角色卡 JSON：有 LLM 则按自由文本降级，否则报无法识别
		if a.deps.AssetParser == nil {
			http.Error(w, "无法识别的 JSON 格式（非 SillyTavern 角色卡；LLM 解析未配置）", http.StatusBadRequest)
			return
		}
	}

	// 2) LLM 路径：自由文本提取
	if a.deps.AssetParser == nil {
		http.Error(w, "LLM 素材解析未配置（仅支持 SillyTavern 角色卡导入）", http.StatusServiceUnavailable)
		return
	}
	res, err := a.deps.AssetParser.ParseText(r.Context(), string(data))
	if err != nil {
		http.Error(w, "LLM 解析失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// writeCardDrafts 输出角色卡草稿：有 LLM 时走混合解析（LLM 整理回写字段），
// 否则纯程序化直映。
func (a *adminAPI) writeCardDrafts(w http.ResponseWriter, r *http.Request, card *assetparse.CharaCardData) {
	if a.deps.AssetParser == nil {
		writeJSON(w, assetparse.CardToDrafts(card))
		return
	}
	res, err := a.deps.AssetParser.ParseCard(r.Context(), card)
	if err != nil {
		http.Error(w, "角色卡解析失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

func hasPNGSignature(data []byte) bool {
	sig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if len(data) < len(sig) {
		return false
	}
	for i := range sig {
		if data[i] != sig[i] {
			return false
		}
	}
	return true
}

// ============================================================
// 素材 AI 补全
// ============================================================

// assetCompleteReq AI 补全请求：以已有草稿为参考生成补充素材。
type assetCompleteReq struct {
	Drafts []assetparse.Draft `json:"drafts"`
}

// handleAssetComplete 根据已有草稿让 LLM 查漏补缺（不落库，结果并入解析预览列表）。
func (a *adminAPI) handleAssetComplete(w http.ResponseWriter, r *http.Request) {
	var req assetCompleteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Drafts) == 0 {
		http.Error(w, "没有可参考的素材草稿（先解析或勾选）", http.StatusBadRequest)
		return
	}
	if a.deps.AssetParser == nil {
		http.Error(w, "LLM 素材解析未配置", http.StatusServiceUnavailable)
		return
	}
	drafts, err := a.deps.AssetParser.CompleteDrafts(r.Context(), req.Drafts)
	if err != nil {
		http.Error(w, "AI 补全失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, &assetparse.Result{Parser: "llm-complete", Drafts: drafts})
}

// ============================================================
// 批量入库（§11.4）
// ============================================================

type assetBatchCreateReq struct {
	Assets []assetUpsertReq `json:"assets"`
}

type assetBatchResult struct {
	Created int      `json:"created"`
	Errors  []string `json:"errors,omitempty"`
}

// handleAssetBatchCreate 批量创建素材（逐项尝试，单项失败不阻塞其他项）。
func (a *adminAPI) handleAssetBatchCreate(w http.ResponseWriter, r *http.Request) {
	if a.deps.AssetStore == nil {
		http.Error(w, "素材库不可用", http.StatusServiceUnavailable)
		return
	}
	var req assetBatchCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Assets) == 0 {
		http.Error(w, "没有要入库的素材", http.StatusBadRequest)
		return
	}
	res := &assetBatchResult{}
	for i, item := range req.Assets {
		asset := &world.Asset{
			Kind: item.Kind, Name: strings.TrimSpace(item.Name), Tags: item.Tags,
			Summary: item.Summary, Source: item.Source, Payload: item.Payload,
		}
		if asset.Source == "" {
			asset.Source = "解析导入"
		}
		if err := a.deps.AssetStore.Create(asset); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("第 %d 项（%s）: %v", i+1, asset.Name, err))
			continue
		}
		res.Created++
	}
	if res.Created == 0 {
		http.Error(w, "全部入库失败: "+strings.Join(res.Errors, "；"), http.StatusBadRequest)
		return
	}
	writeJSON(w, res)
}

// ============================================================
// 解析草稿直接生成世界
// ============================================================

// worldFromDraftsReq 从解析草稿生成世界的请求。
type worldFromDraftsReq struct {
	Name   string             `json:"name"` // 世界名称（写入 ScriptName 便于列表展示，可选）
	Mode   string             `json:"mode"` // simrpg / roleplay，缺省 roleplay
	Drafts []assetparse.Draft `json:"drafts"`
}

// handleWorldFromDrafts 把解析预览确认后的草稿直接播种为新世界（与"入库素材库"并列的另一条出口）。
//
// 草稿 → 世界实体映射（payload 即各实体结构的 JSON，直接反序列化）：
//   - character → Characters；location → LocationDefs；item → Items
//   - faction → Factions；storyline → Storyline（多条时最后一条生效）
//   - world → ApplyWorldview（背景文本 + lore 条目，播种后落地）
//   - lore → 设定库条目（ApplyWorldview 按标题去重追加，重发 ID）
//
// 初始场景取世界观主要地点，其次第一个地点名。单条草稿 payload 损坏不阻塞
// 整体，错误汇总进响应 errors 字段。
func (a *adminAPI) handleWorldFromDrafts(w http.ResponseWriter, r *http.Request) {
	if a.deps.WorldEngine == nil {
		http.Error(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return
	}
	var req worldFromDraftsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Drafts) == 0 {
		http.Error(w, "没有可用草稿（先解析并勾选）", http.StatusBadRequest)
		return
	}

	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = world.ModeRoleplay
	}
	spec := world.SeedSpec{Mode: mode}
	var worldviews []*world.Worldview
	var loreEntries []world.LoreEntry
	var errs []string
	for i, d := range req.Drafts {
		bad := func(err error) {
			errs = append(errs, fmt.Sprintf("第 %d 条（%s/%s）: %v", i+1, d.Kind, d.Name, err))
		}
		switch d.Kind {
		case "character":
			var v world.CharacterState
			if err := json.Unmarshal(d.Payload, &v); err != nil {
				bad(err)
				continue
			}
			if v.Name == "" {
				v.Name = strings.TrimSpace(d.Name)
			}
			spec.Characters = append(spec.Characters, v)
		case "location":
			var v world.Location
			if err := json.Unmarshal(d.Payload, &v); err != nil {
				bad(err)
				continue
			}
			spec.LocationDefs = append(spec.LocationDefs, v)
		case "item":
			var v world.Item
			if err := json.Unmarshal(d.Payload, &v); err != nil {
				bad(err)
				continue
			}
			spec.Items = append(spec.Items, v)
		case "faction":
			var v world.Faction
			if err := json.Unmarshal(d.Payload, &v); err != nil {
				bad(err)
				continue
			}
			spec.Factions = append(spec.Factions, v)
		case "storyline":
			var v world.Storyline
			if err := json.Unmarshal(d.Payload, &v); err != nil {
				bad(err)
				continue
			}
			spec.Storyline = &v
		case "world":
			var v world.Worldview
			if err := json.Unmarshal(d.Payload, &v); err != nil {
				bad(err)
				continue
			}
			worldviews = append(worldviews, &v)
		case "lore":
			var v world.LoreEntry
			if err := json.Unmarshal(d.Payload, &v); err != nil || v.Content == "" {
				bad(err)
				continue
			}
			if v.Title == "" {
				v.Title = strings.TrimSpace(d.Name)
			}
			loreEntries = append(loreEntries, v)
		default:
			errs = append(errs, fmt.Sprintf("第 %d 条（%s/%s）: 未知类型，已跳过", i+1, d.Kind, d.Name))
		}
	}

	// 初始场景：世界观主要地点优先，其次第一个地点名
	for _, wv := range worldviews {
		if spec.Scene == "" && strings.TrimSpace(wv.Location) != "" {
			spec.Scene = strings.TrimSpace(wv.Location)
		}
	}
	if spec.Scene == "" && len(spec.LocationDefs) > 0 {
		spec.Scene = spec.LocationDefs[0].Name
	}

	worldID := fmt.Sprintf("world_%d_%d", time.Now().UnixNano(), idSeq.Add(1))
	state, err := a.deps.WorldEngine.SeedWorld(worldID, spec)
	if err != nil {
		http.Error(w, "创建失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 世界观落地（背景文本 + lore 条目）与世界名称
	a.deps.WorldEngine.Lock(worldID)
	for _, wv := range worldviews {
		world.ApplyWorldview(state, wv, true)
	}
	// 通用设定条目落地（包成 Worldview 走 ApplyWorldview：按标题去重、重发 ID）
	if len(loreEntries) > 0 {
		world.ApplyWorldview(state, &world.Worldview{Lore: loreEntries}, false)
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		state.Name = name
	}
	if err := a.deps.WorldEngine.Save(state); err != nil {
		a.deps.WorldEngine.Unlock(worldID)
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	a.deps.WorldEngine.Unlock(worldID)

	writeJSON(w, map[string]any{
		"world_id": worldID,
		"name":     state.DisplayName(),
		"errors":   errs,
	})
}
