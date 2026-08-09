// 素材解析 / 批量入库 API 测试（《世界编辑器与素材联动设计.md》§11）。
package web

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/assetparse"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

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
