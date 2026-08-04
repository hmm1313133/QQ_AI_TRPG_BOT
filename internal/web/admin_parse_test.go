// 素材解析 / 批量入库 / 剧本素材收藏 API 测试（《世界编辑器与素材联动设计.md》§11）。
package web

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/assetparse"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// 保存一个带完整模组内容的测试剧本。
func saveTestScript(t *testing.T, deps AdminDeps) *script.Script {
	t.Helper()
	scr := &script.Script{
		ID: "scr_collect", Name: "雾都疑云", Title: "雾都疑云（导演剪辑版）",
		Background: script.StoryBackground{
			Setting: "蒸汽雾都", Synopsis: "连环失踪案",
			KeyOrganizations: []string{"雾都警厅"},
		},
		Timeline: []script.TimelineNode{
			{ID: "node_1", Name: "序幕", Type: "act", Order: 1, Description: "案发"},
			{ID: "node_2", Name: "码头追踪", Type: "scene", Order: 2, IsKeyNode: true},
		},
		Characters: []script.ScriptCharacter{
			{ID: "char_1", Name: "莫里亚警长", Personality: "固执"},
		},
		Scenes: []script.ScriptScene{
			{ID: "scene_1", Name: "旧码头", Description: "鱼腥味", DangerLevel: "危险"},
		},
	}
	if err := deps.Archive.Save(scr); err != nil {
		t.Fatalf("保存测试剧本失败: %v", err)
	}
	return scr
}

// 剧本派生素材一键入库：首次创建、再次幂等跳过、素材带标签。
func TestAdmin_ScriptAssetsCollect(t *testing.T) {
	ts, deps := newAssetTestServer(t)
	defer ts.Close()
	scr := saveTestScript(t, deps)

	resp := adminReq(t, "POST", ts.URL+"/api/admin/scripts/"+scr.ID+"/assets/collect", "")
	var res scriptCollectResult
	readJSON(t, resp, &res)
	// world + storyline + character + location + faction = 5
	if res.Created != 5 {
		t.Fatalf("首次收藏应创建 5 条: %+v", res)
	}
	if res.Skipped != 0 || len(res.Errors) != 0 {
		t.Fatalf("首次收藏不应有跳过/错误: %+v", res)
	}

	// 幂等：再次收藏全部跳过
	resp = adminReq(t, "POST", ts.URL+"/api/admin/scripts/"+scr.ID+"/assets/collect", "")
	res = scriptCollectResult{}
	readJSON(t, resp, &res)
	if res.Created != 0 || res.Skipped != 5 {
		t.Fatalf("再次收藏应全部跳过: %+v", res)
	}

	// 素材带「剧本:雾都疑云」标签，可按标签检索
	resp = adminReq(t, "GET", ts.URL+"/api/admin/assets/library?tag="+
		"%E5%89%A7%E6%9C%AC:%E9%9B%BE%E9%83%BD%E7%96%91%E4%BA%91", "")
	var list []world.Asset
	readJSON(t, resp, &list)
	if len(list) != 5 {
		t.Fatalf("按标签应检索到 5 条: %d", len(list))
	}
	for _, item := range list {
		if item.Source != "剧本《雾都疑云（导演剪辑版）》" {
			t.Fatalf("来源标记错误: %+v", item)
		}
	}

	// 不存在的剧本 → 404
	resp = adminReq(t, "POST", ts.URL+"/api/admin/scripts/nonexistent/assets/collect", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("剧本不存在应 404: %d", resp.StatusCode)
	}
}

// trpg 世界创建即自动带入模组素材（无需再手动导入）。
func TestAdmin_TRPGWorldCreateDerivesAssets(t *testing.T) {
	ts, deps := newAssetTestServer(t)
	defer ts.Close()
	scr := saveTestScript(t, deps)

	resp := adminReq(t, "POST", ts.URL+"/api/admin/worlds",
		fmt.Sprintf(`{"world_id":"w_mod","mode":"trpg","script_id":"%s"}`, scr.ID))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("创建 trpg 世界失败: %d", resp.StatusCode)
	}

	ws := deps.WorldEngine.LoadOrNil("w_mod")
	if ws == nil {
		t.Fatal("世界应已创建")
	}
	if ws.Storyline == nil || len(ws.Storyline.Acts) == 0 {
		t.Fatal("主线应从剧本时间轴派生")
	}
	if len(ws.Locations) != 1 || ws.Locations["旧码头"].Danger != "危险" {
		t.Fatalf("地点应从场景派生: %+v", ws.Locations)
	}
	if len(ws.Factions) != 1 {
		t.Fatalf("势力应从关键组织派生: %+v", ws.Factions)
	}
	if ws.Characters["莫里亚警长"] == nil {
		t.Fatal("角色应带入")
	}
}

// world 类型素材：导入写入背景，冲突策略生效。
func TestAdmin_WorldAssetImport(t *testing.T) {
	ts, _ := newAssetTestServer(t)
	defer ts.Close()

	// 创建 world 素材
	resp := adminReq(t, "POST", ts.URL+"/api/admin/assets/library",
		`{"kind":"world","name":"雾都世界观","payload":{"setting":"蒸汽雾都","era":"维多利亚","backstory":"雾都三百年","lore":[{"title":"雾都警厅","content":"腐朽的执法机构","keys":["警厅"]}]}}`)
	var asset world.Asset
	readJSON(t, resp, &asset)
	if asset.ID == "" {
		t.Fatal("world 素材应创建成功")
	}

	// 空背景世界：skip 策略也写入
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds",
		`{"world_id":"w_wv","mode":"simrpg","background":""}`)
	resp.Body.Close()
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_wv/assets/import",
		`{"library":["`+asset.ID+`"]}`)
	var result map[string]any
	readJSON(t, resp, &result)
	if int(result["imported"].(float64)) != 1 {
		t.Fatalf("空背景世界应导入成功: %v", result)
	}

	// 已有背景：skip → 冲突
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_wv/assets/import",
		`{"library":["`+asset.ID+`"]}`)
	result = map[string]any{}
	readJSON(t, resp, &result)
	if int(result["imported"].(float64)) != 0 {
		t.Fatalf("已有背景 skip 应冲突: %v", result)
	}

	// overwrite → 成功
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_wv/assets/import",
		`{"library":["`+asset.ID+`"],"on_conflict":"overwrite"}`)
	result = map[string]any{}
	readJSON(t, resp, &result)
	if int(result["imported"].(float64)) != 1 {
		t.Fatalf("overwrite 应导入成功: %v", result)
	}

	// 收藏世界背景为 world 素材（roundtrip）
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_wv/assets/collect", `{"kind":"world"}`)
	var collected world.Asset
	readJSON(t, resp, &collected)
	if collected.Kind != "world" || collected.ID == "" {
		t.Fatalf("收藏 world 素材失败: %+v", collected)
	}
}

// 批量入库：部分失败不阻塞。
func TestAdmin_AssetBatchCreate(t *testing.T) {
	ts, _ := newAssetTestServer(t)
	defer ts.Close()

	resp := adminReq(t, "POST", ts.URL+"/api/admin/assets/library/batch",
		`{"assets":[
			{"kind":"item","name":"银钥匙","payload":{"name":"银钥匙","type":"key"}},
			{"kind":"bad_kind","name":"坏素材","payload":{}},
			{"kind":"faction","name":"银暮会","payload":{"name":"银暮会"}}
		]}`)
	var res assetBatchResult
	readJSON(t, resp, &res)
	if res.Created != 2 || len(res.Errors) != 1 {
		t.Fatalf("应成功 2 条失败 1 条: %+v", res)
	}
}

// 素材解析：SillyTavern JSON 卡（无需 LLM）。
func TestAdmin_AssetParseSillyTavernJSON(t *testing.T) {
	ts, _ := newAssetTestServer(t)
	defer ts.Close()

	card := `{"spec":"chara_card_v2","data":{"name":"艾拉","description":"银发法师","personality":"好奇","scenario":"边境大陆"}}`
	body, _ := json.Marshal(map[string]string{"text": card})
	req, err := http.NewRequest("POST", ts.URL+"/api/admin/assets/parse", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var res assetparse.Result
	readJSON(t, resp, &res)
	if res.Parser != "sillytavern" {
		t.Fatalf("应走 sillytavern 解析器: %s", res.Parser)
	}
	if len(res.Drafts) != 2 {
		t.Fatalf("应产 2 条草稿: %+v", res.Drafts)
	}
}

// 素材解析：PNG 角色卡（multipart 上传）。
func TestAdmin_AssetParseSillyTavernPNG(t *testing.T) {
	ts, _ := newAssetTestServer(t)
	defer ts.Close()

	card := `{"spec":"chara_card_v2","data":{"name":"卡厄斯","description":"深渊行者"}}`
	png := buildTestPNG("chara", card)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "card.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(png); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	req, err := http.NewRequest("POST", ts.URL+"/api/admin/assets/parse", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var res assetparse.Result
	readJSON(t, resp, &res)
	if res.Parser != "sillytavern" || len(res.Drafts) != 1 || res.Drafts[0].Name != "卡厄斯" {
		t.Fatalf("PNG 角色卡解析错误: %+v", res)
	}
}

// 素材解析：非角色卡内容且未配置 LLM → 明确报错（不静默失败）。
func TestAdmin_AssetParseNoLLM(t *testing.T) {
	ts, _ := newAssetTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"text": "一段普通的角色设定文字，不是 JSON。"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/admin/assets/parse", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("未配置 LLM 应 503: %d", resp.StatusCode)
	}
}

// buildTestPNG 构造含 tEXt 角色卡 chunk 的最小 PNG。
func buildTestPNG(keyword, cardJSON string) []byte {
	b64 := base64.StdEncoding.EncodeToString([]byte(cardJSON))
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	payload := append([]byte(keyword), 0)
	payload = append(payload, []byte(b64)...)
	writeChunk(&buf, "tEXt", payload)
	writeChunk(&buf, "IEND", nil)
	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, typ string, payload []byte) {
	_ = binary.Write(buf, binary.BigEndian, uint32(len(payload)))
	buf.WriteString(typ)
	buf.Write(payload)
	_ = binary.Write(buf, binary.BigEndian, uint32(0)) // CRC 占位（解析器不校验）
}
