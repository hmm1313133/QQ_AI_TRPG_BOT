// SillyTavern 角色卡解析测试（设计 §11.4）。
package assetparse

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

// buildPNGWithTextChunk 构造含一个 tEXt chunk 的最小 PNG 字节（解析器不校验 CRC）。
func buildPNGWithTextChunk(keyword, text string) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	payload := append([]byte(keyword), 0)
	payload = append(payload, []byte(text)...)
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(payload)))
	buf.WriteString("tEXt")
	buf.Write(payload)
	_ = binary.Write(&buf, binary.BigEndian, uint32(0)) // CRC 占位
	_ = binary.Write(&buf, binary.BigEndian, uint32(0))
	buf.WriteString("IEND")
	_ = binary.Write(&buf, binary.BigEndian, uint32(0))
	return buf.Bytes()
}

const v2CardJSON = `{
  "spec": "chara_card_v2",
  "spec_version": "2.0",
  "data": {
    "name": "艾拉",
    "description": "银发的流浪法师，四处寻找失落的魔法典籍。",
    "personality": "好奇、谨慎、略带毒舌",
    "scenario": "魔法衰微的边境大陆",
    "first_mes": "你好，旅人。",
    "tags": ["法师", "流浪者"],
    "character_book": {
      "entries": [
        {"keys": ["魔法学院"], "content": "位于旧都的最高学府。", "comment": "魔法学院", "insertion_order": 100},
        {"keys": ["空白条目"], "content": ""}
      ]
    }
  }
}`

func TestParseCardJSON_V2(t *testing.T) {
	res, err := ParseCardJSON([]byte(v2CardJSON))
	if err != nil || res == nil {
		t.Fatalf("v2 卡应解析成功: res=%v err=%v", res, err)
	}
	if res.Parser != "sillytavern" {
		t.Fatalf("parser 应为 sillytavern: %s", res.Parser)
	}
	if len(res.Drafts) != 2 {
		t.Fatalf("应产出角色+世界观两条草稿: %d", len(res.Drafts))
	}

	charDraft := res.Drafts[0]
	if charDraft.Kind != "character" || charDraft.Name != "艾拉" {
		t.Fatalf("角色草稿错误: %+v", charDraft)
	}
	var payload map[string]any
	if err := json.Unmarshal(charDraft.Payload, &payload); err != nil {
		t.Fatalf("角色 payload 应为合法 JSON: %v", err)
	}
	if payload["personality"] != "好奇、谨慎、略带毒舌" {
		t.Fatalf("personality 映射错误: %v", payload["personality"])
	}
	if payload["backstory"] != "银发的流浪法师，四处寻找失落的魔法典籍。" {
		t.Fatalf("description 应映射为 backstory: %v", payload["backstory"])
	}
	if len(charDraft.Tags) != 2 {
		t.Fatalf("标签应带入: %v", charDraft.Tags)
	}

	worldDraft := res.Drafts[1]
	if worldDraft.Kind != "world" {
		t.Fatalf("世界观草稿错误: %+v", worldDraft)
	}
	var wv map[string]any
	if err := json.Unmarshal(worldDraft.Payload, &wv); err != nil {
		t.Fatalf("世界观 payload 应为合法 JSON: %v", err)
	}
	if wv["setting"] != "魔法衰微的边境大陆" {
		t.Fatalf("scenario 应映射为 setting: %v", wv["setting"])
	}
	lore, _ := wv["lore"].([]any)
	if len(lore) != 1 {
		t.Fatalf("空 content 的条目应被过滤，期望 1 条 lore: %v", wv["lore"])
	}
}

func TestParseCardJSON_V1(t *testing.T) {
	v1 := `{"name":"老船长","description":"纵横七海三十年","personality":"豪爽","scenario":"","first_mes":"上船！"}`
	res, err := ParseCardJSON([]byte(v1))
	if err != nil || res == nil {
		t.Fatalf("v1 卡应解析成功: res=%v err=%v", res, err)
	}
	if len(res.Drafts) != 1 || res.Drafts[0].Name != "老船长" {
		t.Fatalf("v1 卡无 scenario 时应只产角色草稿: %+v", res.Drafts)
	}
}

func TestParseCardJSON_NotACard(t *testing.T) {
	for _, data := range []string{
		`{"foo":"bar"}`,
		`{"name":"x"}`, // 只有 name 没有任何描述字段
		`[1,2,3]`,
		`not json at all`,
	} {
		res, err := ParseCardJSON([]byte(data))
		if err != nil {
			t.Fatalf("非角色卡不应报错(%s): %v", data, err)
		}
		if res != nil {
			t.Fatalf("非角色卡应返回 nil(%s): %+v", data, res)
		}
	}
}

func TestParseCardPNG(t *testing.T) {
	cardB64 := base64.StdEncoding.EncodeToString([]byte(v2CardJSON))
	png := buildPNGWithTextChunk("chara", cardB64)

	res, err := ParseCardPNG(png)
	if err != nil || res == nil {
		t.Fatalf("PNG 角色卡应解析成功: res=%v err=%v", res, err)
	}
	if len(res.Drafts) != 2 || res.Drafts[0].Name != "艾拉" {
		t.Fatalf("PNG 角色卡草稿错误: %+v", res.Drafts)
	}

	// 不含角色卡 chunk 的 PNG
	plain := buildPNGWithTextChunk("Comment", "just a comment")
	if res, _ := ParseCardPNG(plain); res != nil {
		t.Fatalf("无角色卡 chunk 应返回 nil: %+v", res)
	}

	// 非 PNG
	if res, _ := ParseCardPNG([]byte("not a png")); res != nil {
		t.Fatalf("非 PNG 应返回 nil: %+v", res)
	}
}

func TestCleanCardText(t *testing.T) {
	in := "<character_design_complex>\n姓名：{{char}}\n同伴：{{user}}\n</character_design_complex>\n\n\n\n外貌：银发"
	got := cleanCardText("艾拉", in)
	if got == in {
		t.Fatalf("应发生清理: %q", got)
	}
	for _, bad := range []string{"<character_design_complex>", "</character_design_complex>", "{{char}}", "{{user}}"} {
		if strings.Contains(got, bad) {
			t.Fatalf("清理后不应残留 %q: %q", bad, got)
		}
	}
	if !strings.Contains(got, "姓名：艾拉") || !strings.Contains(got, "同伴：用户") {
		t.Fatalf("宏应替换为角色名/用户: %q", got)
	}
	if !strings.Contains(got, "外貌：银发") {
		t.Fatalf("tag 内文本应保留: %q", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("多余空行应被压缩: %q", got)
	}
}

func TestBuildCardText(t *testing.T) {
	card := DecodeCardJSON([]byte(v2CardJSON))
	if card == nil {
		t.Fatal("v2 卡应解码成功")
	}
	text := buildCardText(card)
	for _, want := range []string{"艾拉", "【角色描述】", "【性格】", "【场景设定】", "魔法衰微的边境大陆"} {
		if !strings.Contains(text, want) {
			t.Fatalf("LLM 输入应包含 %q: %q", want, text)
		}
	}

	// 三个自由文本字段全空时应返回空串（ParseCard 据此直接走程序化直映）
	empty := &CharaCardData{Name: "空卡"}
	if got := buildCardText(empty); got != "" {
		t.Fatalf("无自由文本时应返回空串: %q", got)
	}
}

func TestParseCard_NoFreeText(t *testing.T) {
	// 无自由文本时 ParseCard 不调用 LLM，直接程序化直映（不依赖 LLM 配置）
	p := &Parser{}
	res, err := p.ParseCard(t.Context(), &CharaCardData{Name: "空卡", Tags: []string{"t"}})
	if err != nil || res == nil {
		t.Fatalf("应直映成功: res=%v err=%v", res, err)
	}
	if res.Parser != "sillytavern" || len(res.Drafts) != 1 || res.Drafts[0].Name != "空卡" {
		t.Fatalf("应降级为 sillytavern 程序化直映: %+v", res)
	}
}

func TestMatchExtractedCharacter(t *testing.T) {
	out := &extractionOutput{Characters: []extractedCharacter{
		{Name: "别人"}, {Name: "艾拉"},
	}}
	if c := matchExtractedCharacter(out, "艾拉"); c == nil || c.Name != "艾拉" {
		t.Fatalf("应按名字精确匹配: %+v", c)
	}
	if c := matchExtractedCharacter(out, "不存在"); c == nil || c.Name != "别人" {
		t.Fatalf("无匹配时应取第一个: %+v", c)
	}
	if c := matchExtractedCharacter(&extractionOutput{}, "艾拉"); c != nil {
		t.Fatalf("空列表应返回 nil: %+v", c)
	}
}
