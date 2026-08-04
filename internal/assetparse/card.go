// SillyTavern 角色卡（chara_card v1/v2/v3）程序化解码。
//
// 支持两种载体：
//   - JSON 文件/文本：v1 扁平字段，或 v2/v3 的 {spec, data:{...}} 包裹
//   - PNG 图片：手动解析 PNG chunks，取 tEXt/zTXt/iTXt 中 keyword 为
//     "chara"（v1/v2）或 "ccv3"（v3）的 base64 JSON payload
//
// 映射（设计 §11.4）：
//   程序化解码后有两条路径：
//   - 无 LLM：description → 角色 backstory；personality → personality；
//     scenario 非空 → 额外产出 world 草稿（setting）——原样直映，不做整理；
//   - 有 LLM（Parser.ParseCard）：description/personality/scenario 经宏替换与
//     tag 清理后交给 LLM 整理，回写到 appearance/personality/backstory/skills
//     及 Worldview 各对应字段；
//   两条路径的 character_book 都程序化直映为 world 草稿的 lore 条目（keys/content），
//   不经 LLM，避免结构化内容被改写。
//   first_mes / mes_example 属于开场白与对话样例，不进素材（运行时由扮演 prompt 处理）。
package assetparse

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// CharaCardData 角色卡数据段（v2/v3 的 data 字段，v1 即顶层）。
type CharaCardData struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Personality string   `json:"personality"`
	Scenario    string   `json:"scenario"`
	FirstMes    string   `json:"first_mes"`
	MesExample  string   `json:"mes_example"`
	Tags        []string `json:"tags"`
	Creator     string   `json:"creator"`
	Book        *charaBook `json:"character_book"`
}

type charaBook struct {
	Name    string          `json:"name"`
	Entries []charaBookEntry `json:"entries"`
}

type charaBookEntry struct {
	Keys      []string `json:"keys"`
	Content   string   `json:"content"`
	Comment   string   `json:"comment"`
	Enabled   *bool    `json:"enabled"`
	Insertion int      `json:"insertion_order"`
}

// charaCardV2 v2/v3 顶层包裹。
type charaCardV2 struct {
	Spec string         `json:"spec"`
	Data CharaCardData `json:"data"`
}

// DecodeCardJSON 把 JSON 字节解码为 SillyTavern 角色卡数据段。
// 不是角色卡（缺少 spec 且无 v1 特征字段）时返回 nil。
func DecodeCardJSON(data []byte) *CharaCardData {
	var v2 charaCardV2
	if err := json.Unmarshal(data, &v2); err == nil &&
		strings.HasPrefix(v2.Spec, "chara_card_v") && v2.Data.Name != "" {
		return &v2.Data
	}
	// v1 扁平结构：有 name 且至少有 description/personality/scenario 之一
	var v1 CharaCardData
	if err := json.Unmarshal(data, &v1); err != nil || v1.Name == "" ||
		(v1.Description == "" && v1.Personality == "" && v1.Scenario == "") {
		return nil
	}
	return &v1
}

// ParseCardJSON 尝试把 JSON 字节解析为 SillyTavern 角色卡（纯程序化直映，不经 LLM）。
// 不是角色卡（缺少 spec 且无 v1 特征字段）时返回 nil, nil。
func ParseCardJSON(data []byte) (*Result, error) {
	card := DecodeCardJSON(data)
	if card == nil {
		return nil, nil
	}
	return CardToDrafts(card), nil
}

var pngSignature = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// DecodeCardPNG 从 PNG 字节中解码内嵌角色卡（tEXt/zTXt/iTXt chunk）。
// 非 PNG 或不含角色卡 chunk 时返回 nil。
func DecodeCardPNG(data []byte) *CharaCardData {
	if !bytes.HasPrefix(data, pngSignature) {
		return nil
	}
	r := bytes.NewReader(data[len(pngSignature):])
	for {
		var length uint32
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			return nil // 读到末尾也没找到
		}
		typ := make([]byte, 4)
		if _, err := io.ReadFull(r, typ); err != nil {
			return nil
		}
		if length > 64<<20 { // 防御：单个 chunk 不应超过 64MB
			return nil
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil
		}
		// 跳过 CRC
		if _, err := r.Seek(4, io.SeekCurrent); err != nil {
			return nil
		}
		chunkType := string(typ)
		if chunkType == "tEXt" || chunkType == "zTXt" || chunkType == "iTXt" {
			if cardJSON := extractCharaChunk(chunkType, payload); cardJSON != nil {
				if card := DecodeCardJSON(cardJSON); card != nil {
					return card
				}
			}
		}
	}
}

// ParseCardPNG 从 PNG 字节中提取内嵌角色卡（纯程序化直映，不经 LLM）。
// 非 PNG 或不含角色卡 chunk 时返回 nil, nil。
func ParseCardPNG(data []byte) (*Result, error) {
	card := DecodeCardPNG(data)
	if card == nil {
		return nil, nil
	}
	return CardToDrafts(card), nil
}

// extractCharaChunk 从文本 chunk 中提取 keyword 为 chara/ccv3 的 base64 JSON。
// 非角色卡 chunk 返回 nil。
func extractCharaChunk(chunkType string, payload []byte) []byte {
	nul := bytes.IndexByte(payload, 0)
	if nul <= 0 {
		return nil
	}
	keyword := string(payload[:nul])
	if keyword != "chara" && keyword != "ccv3" {
		return nil
	}
	rest := payload[nul+1:]
	switch chunkType {
	case "tEXt":
		// keyword\0 text(latin-1, base64)
	case "zTXt":
		// keyword\0 compressionMethod(1) compressedText(zlib)
		if len(rest) < 1 {
			return nil
		}
		zr, err := zlib.NewReader(bytes.NewReader(rest[1:]))
		if err != nil {
			return nil
		}
		defer zr.Close()
		if rest, err = io.ReadAll(zr); err != nil {
			return nil
		}
	case "iTXt":
		// keyword\0 compressionFlag(1) compressionMethod(1) languageTag\0 translatedKeyword\0 text(utf-8)
		if len(rest) < 2 {
			return nil
		}
		compressed := rest[0] == 1
		rest = rest[2:]
		for i := 0; i < 2; i++ { // 跳过 languageTag、translatedKeyword
			idx := bytes.IndexByte(rest, 0)
			if idx < 0 {
				return nil
			}
			rest = rest[idx+1:]
		}
		if compressed {
			zr, err := zlib.NewReader(bytes.NewReader(rest))
			if err != nil {
				return nil
			}
			defer zr.Close()
			if rest, err = io.ReadAll(zr); err != nil {
				return nil
			}
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(rest)))
	if err != nil {
		return nil
	}
	return decoded
}

// CardToDrafts 角色卡 → 素材草稿（纯程序化直映：角色 + 可选世界观）。
// 用于无 LLM 或 LLM 失败时的降级路径；有 LLM 时应走 Parser.ParseCard。
func CardToDrafts(card *CharaCardData) *Result {
	res := &Result{Parser: "sillytavern"}

	summary := card.Description
	if runes := []rune(summary); len(runes) > 80 {
		summary = string(runes[:80]) + "…"
	}
	charPayload, _ := json.Marshal(world.CharacterState{
		Name:        card.Name,
		Kind:        "npc",
		Alive:       true,
		Disposition: "neutral",
		Personality: card.Personality,
		Backstory:   card.Description,
	})
	res.Drafts = append(res.Drafts, Draft{
		Kind:    "character",
		Name:    card.Name,
		Summary: summary,
		Tags:    card.Tags,
		Payload: charPayload,
	})

	// scenario / character_book → 世界观素材
	wv := &world.Worldview{Setting: card.Scenario}
	wv.Lore = cardLoreEntries(card)
	if !wv.Empty() {
		wvPayload, _ := json.Marshal(wv)
		res.Drafts = append(res.Drafts, Draft{
			Kind:    "world",
			Name:    card.Name + " 的世界观",
			Summary: "随角色卡导入的场景设定与世界书",
			Payload: wvPayload,
		})
	}
	return res
}

// cardLoreEntries 把 character_book 条目直映为 lore 条目（程序化，不经 LLM）。
func cardLoreEntries(card *CharaCardData) []world.LoreEntry {
	if card.Book == nil {
		return nil
	}
	var lore []world.LoreEntry
	for i, e := range card.Book.Entries {
		if len(e.Keys) == 0 || strings.TrimSpace(e.Content) == "" {
			continue
		}
		title := e.Comment
		if title == "" {
			title = e.Keys[0]
		}
		enabled := true
		if e.Enabled != nil {
			enabled = *e.Enabled
		}
		lore = append(lore, world.LoreEntry{
			ID:       fmt.Sprintf("lor_st_%d", i+1),
			Title:    title,
			Category: world.LoreCategoryBackground,
			Keys:     e.Keys,
			Position: world.LorePositionFront,
			Priority: 50,
			Enabled:  enabled,
			Content:  e.Content,
			Source:   world.LoreSourceManual,
		})
	}
	return lore
}

// pseudoTagRe 匹配卡作者自造的伪 XML 标记（如 <character_design_complex>、</section>），
// 只剥壳不删内容；正常文本里以字母开头的尖括号串极少见，误伤可接受。
var pseudoTagRe = regexp.MustCompile(`</?[a-zA-Z][^>\n]{0,60}>`)

var multiBlankLineRe = regexp.MustCompile(`\n{3,}`)

// cleanCardText 对角色卡自由文本字段做机械清理：
// 替换 {{char}}/{{user}} 宏、剥掉伪 XML tag 壳（保留壳内文本）、压缩多余空行。
// 语义层面的归类整理由 LLM 完成，这里只做无损清洗。
func cleanCardText(name, text string) string {
	text = strings.ReplaceAll(text, "{{char}}", name)
	text = strings.ReplaceAll(text, "{{Char}}", name)
	text = strings.ReplaceAll(text, "{{user}}", "用户")
	text = strings.ReplaceAll(text, "{{User}}", "用户")
	text = pseudoTagRe.ReplaceAllString(text, "")
	text = multiBlankLineRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// ParseCard 角色卡混合解析（参考剧本解析的「LLM 提取 + 程序化保底」模式）：
//   - description/personality/scenario 机械清理后交给 LLM 整理，
//     回写到 CharacterState 的 appearance/personality/backstory/skills
//     及 Worldview 的 setting/era/atmosphere 等对应字段；
//   - character_book 保持程序化直映为 lore 条目，不经 LLM；
//   - LLM 失败或未提取出内容时，降级为 CardToDrafts 的纯程序化直映。
func (p *Parser) ParseCard(ctx context.Context, card *CharaCardData) (*Result, error) {
	text := buildCardText(card)
	if text == "" {
		// 卡上没有可整理的自由文本，直接程序化直映
		return CardToDrafts(card), nil
	}
	out, err := p.parseStructured(ctx, text)
	if err != nil {
		log.Printf("[AssetParser] 角色卡 LLM 整理失败，降级程序化直映: %v", err)
		return CardToDrafts(card), nil
	}

	res := &Result{Parser: "sillytavern+llm"}

	// 角色：名字/标签以卡为准，叙事字段取 LLM 整理结果
	cs := world.CharacterState{
		Name: card.Name, Kind: "npc", Alive: true, Disposition: "neutral",
	}
	summary := ""
	tags := card.Tags
	if c := matchExtractedCharacter(out, card.Name); c != nil {
		cs.Appearance = c.Appearance
		cs.Personality = c.Personality
		cs.Backstory = c.Backstory
		cs.Skills = c.Skills
		summary = c.Summary
		if len(tags) == 0 {
			tags = c.Tags
		}
	}
	// LLM 未提取出角色（或字段缺失）时保底原样直映
	if cs.Personality == "" {
		cs.Personality = card.Personality
	}
	if cs.Backstory == "" {
		cs.Backstory = card.Description
	}
	if summary == "" {
		summary = cs.Backstory
	}
	if runes := []rune(summary); len(runes) > 80 {
		summary = string(runes[:80]) + "…"
	}
	charPayload, _ := json.Marshal(cs)
	res.Drafts = append(res.Drafts, Draft{
		Kind:    "character",
		Name:    card.Name,
		Summary: summary,
		Tags:    tags,
		Payload: charPayload,
	})

	// 世界观：LLM 整理的 setting/era/atmosphere 等 + 程序化 lore
	wv := &world.Worldview{}
	if out.Worldview != nil {
		w := out.Worldview
		wv.Setting = w.Setting
		wv.Era = w.Era
		wv.Location = w.Location
		wv.Atmosphere = w.Atmosphere
		wv.Tone = w.Tone
		wv.Backstory = w.Backstory
		wv.Themes = w.Themes
	}
	if wv.Setting == "" && strings.TrimSpace(card.Scenario) != "" {
		wv.Setting = cleanCardText(card.Name, card.Scenario)
	}
	wv.Lore = cardLoreEntries(card)
	if !wv.Empty() {
		wvSummary := wv.Setting
		if runes := []rune(wvSummary); len(runes) > 80 {
			wvSummary = string(runes[:80]) + "…"
		}
		if wvSummary == "" {
			wvSummary = "随角色卡导入的场景设定与世界书"
		}
		wvPayload, _ := json.Marshal(wv)
		res.Drafts = append(res.Drafts, Draft{
			Kind:    "world",
			Name:    card.Name + " 的世界观",
			Summary: wvSummary,
			Payload: wvPayload,
		})
	}
	return res, nil
}

// buildCardText 把角色卡的自由文本字段拼成 LLM 输入（机械清理后分节给出）。
// 三个字段全空时返回空串。
func buildCardText(card *CharaCardData) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "以下文本来自 SillyTavern 角色卡「%s」，原文混有卡作者自造的非标准标记（已做初步清理）。请提取创作素材，角色名固定为「%s」。\n\n", card.Name, card.Name)
	wrote := false
	if d := cleanCardText(card.Name, card.Description); d != "" {
		fmt.Fprintf(&sb, "【角色描述】\n%s\n\n", d)
		wrote = true
	}
	if p := cleanCardText(card.Name, card.Personality); p != "" {
		fmt.Fprintf(&sb, "【性格】\n%s\n\n", p)
		wrote = true
	}
	if s := cleanCardText(card.Name, card.Scenario); s != "" {
		fmt.Fprintf(&sb, "【场景设定】\n%s\n\n", s)
		wrote = true
	}
	if !wrote {
		return ""
	}
	return sb.String()
}

// matchExtractedCharacter 在 LLM 提取结果中找与角色卡对应的角色：
// 先按名字精确匹配，其次大小写不敏感匹配，最后取第一个。
func matchExtractedCharacter(out *extractionOutput, name string) *extractedCharacter {
	for i := range out.Characters {
		if out.Characters[i].Name == name {
			return &out.Characters[i]
		}
	}
	for i := range out.Characters {
		if strings.EqualFold(out.Characters[i].Name, name) {
			return &out.Characters[i]
		}
	}
	if len(out.Characters) > 0 {
		return &out.Characters[0]
	}
	return nil
}
