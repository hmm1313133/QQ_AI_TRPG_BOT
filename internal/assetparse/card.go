// SillyTavern 角色卡（chara_card v1/v2/v3）程序化解码。
//
// 支持两种载体：
//   - JSON 文件/文本：v1 扁平字段，或 v2/v3 的 {spec, data:{...}} 包裹
//   - PNG 图片：手动解析 PNG chunks，取 tEXt/zTXt/iTXt 中 keyword 为
//     "chara"（v1/v2）或 "ccv3"（v3）的 base64 JSON payload
//
// 映射（设计 §11.4）：
//   description → 角色 backstory；personality → personality；
//   scenario 非空 → 额外产出 world 草稿（setting）；
//   character_book → world 草稿的 lore 条目（keys/content 直映）；
//   first_mes / mes_example 属于开场白与对话样例，不进素材（运行时由扮演 prompt 处理）。
package assetparse

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// charaCardData 角色卡数据段（v2/v3 的 data 字段，v1 即顶层）。
type charaCardData struct {
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
	Data charaCardData `json:"data"`
}

// ParseCardJSON 尝试把 JSON 字节解析为 SillyTavern 角色卡。
// 不是角色卡（缺少 spec 且无 v1 特征字段）时返回 nil, nil。
func ParseCardJSON(data []byte) (*Result, error) {
	var v2 charaCardV2
	var card *charaCardData
	if err := json.Unmarshal(data, &v2); err == nil &&
		strings.HasPrefix(v2.Spec, "chara_card_v") && v2.Data.Name != "" {
		card = &v2.Data
	} else {
		// v1 扁平结构：有 name 且至少有 description/personality/scenario 之一
		var v1 charaCardData
		if err := json.Unmarshal(data, &v1); err != nil || v1.Name == "" ||
			(v1.Description == "" && v1.Personality == "" && v1.Scenario == "") {
			return nil, nil
		}
		card = &v1
	}
	return cardToDrafts(card), nil
}

var pngSignature = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// ParseCardPNG 从 PNG 字节中提取内嵌角色卡（tEXt/zTXt/iTXt chunk）。
// 非 PNG 或不含角色卡 chunk 时返回 nil, nil。
func ParseCardPNG(data []byte) (*Result, error) {
	if !bytes.HasPrefix(data, pngSignature) {
		return nil, nil
	}
	r := bytes.NewReader(data[len(pngSignature):])
	for {
		var length uint32
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			return nil, nil // 读到末尾也没找到
		}
		typ := make([]byte, 4)
		if _, err := io.ReadFull(r, typ); err != nil {
			return nil, nil
		}
		if length > 64<<20 { // 防御：单个 chunk 不应超过 64MB
			return nil, fmt.Errorf("PNG chunk 长度异常: %d", length)
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, nil
		}
		// 跳过 CRC
		if _, err := r.Seek(4, io.SeekCurrent); err != nil {
			return nil, nil
		}
		chunkType := string(typ)
		if chunkType == "tEXt" || chunkType == "zTXt" || chunkType == "iTXt" {
			if cardJSON := extractCharaChunk(chunkType, payload); cardJSON != nil {
				res, err := ParseCardJSON(cardJSON)
				if err != nil {
					return nil, err
				}
				if res != nil {
					return res, nil
				}
			}
		}
	}
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

// cardToDrafts 角色卡 → 素材草稿（角色 + 可选世界观）。
func cardToDrafts(card *charaCardData) *Result {
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
	if card.Book != nil {
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
			wv.Lore = append(wv.Lore, world.LoreEntry{
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
	}
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
