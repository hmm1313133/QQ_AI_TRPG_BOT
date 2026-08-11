// 自由文本解析的分块/合并/lore 草稿纯函数测试。
package assetparse

import (
	"encoding/json"
	"strings"
	"testing"
)

// 短文本单块；长文本按段落切块且每块不超限。
func TestSplitTextChunks(t *testing.T) {
	if got := splitTextChunks("  ", 100); got != nil {
		t.Fatalf("空文本应返回 nil: %v", got)
	}
	short := "一段短文本"
	if got := splitTextChunks(short, 100); len(got) != 1 || got[0] != short {
		t.Fatalf("短文本应单块返回: %v", got)
	}

	// 10 段 × 300 字，块上限 1000 → 应切多块，且每块 ≤1000 字
	var paras []string
	for i := 0; i < 10; i++ {
		paras = append(paras, strings.Repeat("段", 300))
	}
	chunks := splitTextChunks(strings.Join(paras, "\n\n"), 1000)
	if len(chunks) < 3 {
		t.Fatalf("长文本应切多块: %d", len(chunks))
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n > 1000 {
			t.Fatalf("块 %d 超限: %d", i, n)
		}
	}
	// 内容无丢失（拼接后字数一致）
	total := 0
	for _, c := range chunks {
		total += len([]rune(strings.ReplaceAll(c, "\n", "")))
	}
	if total != 3000 {
		t.Fatalf("切块不应丢内容: %d", total)
	}
}

// 单段超长：硬切且每块不超限。
func TestSplitTextChunksLongParagraph(t *testing.T) {
	long := strings.Repeat("字", 5000)
	chunks := splitTextChunks(long, 1000)
	if len(chunks) != 5 {
		t.Fatalf("5000 字单段应切 5 块: %d", len(chunks))
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n > 1000 {
			t.Fatalf("块 %d 超限: %d", i, n)
		}
	}
}

// 合并：同名实体字段留长、列表并集、lore 按标题去重留长。
func TestMergeOutputs(t *testing.T) {
	a := &extractionOutput{
		Characters: []extractedCharacter{
			{Name: "王婶", Summary: "小卖部老板娘", Personality: "热心", Skills: []string{"算账"}},
		},
		LoreEntries: []extractedLore{
			{Title: "祭山神", Category: "rule", Keys: []string{"山神"}, Content: "短版本"},
		},
		Worldview: &extractedWorldview{Name: "李家村", Setting: "北方农村"},
	}
	b := &extractionOutput{
		Characters: []extractedCharacter{
			{Name: "王婶", Personality: "嘴上不饶人，心里门儿清，谁家有难处她第一个到", KeyDialogue: []string{"哟？"}, Skills: []string{"打听消息"}},
			{Name: "李四", Summary: "村支书"},
		},
		LoreEntries: []extractedLore{
			{Title: "祭山神", Content: "每年霜降后第一个逢七的日子，全村男人天没亮就要上山……（长版本）"},
			{Title: "编年史", Category: "history", Content: "1983 年包产到户……"},
		},
		Worldview: &extractedWorldview{Backstory: "详细背景长文"},
	}
	m := mergeOutputs([]*extractionOutput{a, b})

	if len(m.Characters) != 2 {
		t.Fatalf("同名角色应合并: %+v", m.Characters)
	}
	ws := m.Characters[0]
	if ws.Personality != "嘴上不饶人，心里门儿清，谁家有难处她第一个到" {
		t.Fatalf("性格字段应保留更长版本: %q", ws.Personality)
	}
	if ws.Summary != "小卖部老板娘" {
		t.Fatalf("摘要应保留更长版本: %q", ws.Summary)
	}
	if len(ws.Skills) != 2 {
		t.Fatalf("技能应并集: %v", ws.Skills)
	}
	if len(m.LoreEntries) != 2 {
		t.Fatalf("lore 应按标题去重: %+v", m.LoreEntries)
	}
	if !strings.Contains(m.LoreEntries[0].Content, "长版本") {
		t.Fatalf("lore 应保留更长 content: %q", m.LoreEntries[0].Content)
	}
	if m.LoreEntries[0].Category != "rule" {
		t.Fatalf("lore 分类应保留首个非空: %q", m.LoreEntries[0].Category)
	}
	if m.Worldview == nil || m.Worldview.Setting != "北方农村" || m.Worldview.Backstory != "详细背景长文" {
		t.Fatalf("世界观应合并: %+v", m.Worldview)
	}
}

// lore 草稿：payload 与 world.LoreEntry 同形、缺省值补齐、空内容跳过。
func TestOutputToDraftsLore(t *testing.T) {
	out := &extractionOutput{
		LoreEntries: []extractedLore{
			{Title: "祭山神仪式", Keys: []string{"山神"}, Content: "第一步……"},
			{Title: "空内容", Content: ""},
		},
	}
	drafts := outputToDrafts(out)
	if len(drafts) != 1 {
		t.Fatalf("空内容条目应跳过: %+v", drafts)
	}
	d := drafts[0]
	if d.Kind != "lore" || d.Name != "祭山神仪式" {
		t.Fatalf("lore 草稿元数据错误: %+v", d)
	}
	var payload map[string]any
	if err := json.Unmarshal(d.Payload, &payload); err != nil {
		t.Fatalf("payload 应为合法 JSON: %v", err)
	}
	if payload["category"] != "background" || payload["position"] != "front" {
		t.Fatalf("缺省分类/位置应补齐: %+v", payload)
	}
	if payload["content"] != "第一步……" {
		t.Fatalf("content 应保留原文: %+v", payload)
	}
}
