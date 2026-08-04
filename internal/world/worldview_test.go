// 世界观素材（world 类型）单元测试（设计 §11.3）。
package world

import (
	"strings"
	"testing"
)

func TestWorldviewBuildText(t *testing.T) {
	wv := &Worldview{
		Setting:    "蒸汽与齿轮的帝国",
		Era:        "维多利亚时代",
		Location:   "雾都",
		Atmosphere: "阴霾",
		Tone:       "冷峻",
		Themes:     []string{"阶级", "革新"},
		Backstory:  "帝国历三百年，蒸汽机轰鸣。",
	}
	text := wv.BuildText()
	for _, want := range []string{"蒸汽与齿轮的帝国", "维多利亚时代", "雾都", "阴霾", "冷峻", "阶级、革新", "帝国历三百年"} {
		if !strings.Contains(text, want) {
			t.Fatalf("BuildText 应包含 %q: %s", want, text)
		}
	}

	empty := &Worldview{}
	if !empty.Empty() {
		t.Fatal("空 Worldview 应为 Empty")
	}
	if wv.Empty() {
		t.Fatal("有内容的 Worldview 不应为 Empty")
	}
}

func TestApplyWorldview(t *testing.T) {
	ws := NewWorldState("w1", ModeSimRPG)
	wv := &Worldview{
		Setting: "新世界设定",
		Lore: []LoreEntry{
			{Title: "条目A", Content: "内容A", Keys: []string{"a"}},
			{Title: "条目A", Content: "重复条目", Keys: []string{"a"}},
		},
	}

	// 空背景：skip 策略也会写入
	if !ApplyWorldview(ws, wv, false) {
		t.Fatal("应有变更")
	}
	if !strings.Contains(ws.Background, "新世界设定") {
		t.Fatalf("背景应被写入: %s", ws.Background)
	}
	if len(ws.Lore) != 1 {
		t.Fatalf("同名条目应去重，期望 1 条，实际 %d", len(ws.Lore))
	}
	if ws.Lore[0].ID == "" || ws.Lore[0].Category != LoreCategoryBackground || !ws.Lore[0].Enabled {
		t.Fatalf("条目应补默认值: %+v", ws.Lore[0])
	}

	// 已有背景 + skip：不覆盖
	wv2 := &Worldview{Setting: "另一套设定"}
	if ApplyWorldview(ws, wv2, false) {
		t.Fatal("skip 策略下已有背景不应变更")
	}
	if !strings.Contains(ws.Background, "新世界设定") {
		t.Fatal("skip 策略下背景不应被覆盖")
	}

	// overwrite：覆盖
	if !ApplyWorldview(ws, wv2, true) {
		t.Fatal("overwrite 应有变更")
	}
	if !strings.Contains(ws.Background, "另一套设定") {
		t.Fatal("overwrite 应覆盖背景")
	}
}

func TestWorldviewFromState(t *testing.T) {
	ws := NewWorldState("w2", ModeSimRPG)
	ws.Background = "背景文本"
	ws.Lore = []LoreEntry{
		{ID: "l1", Title: "手工", Content: "c", Source: LoreSourceManual},
		{ID: "l2", Title: "剧本", Content: "c", Source: LoreSourceScript},
	}
	wv := WorldviewFromState(ws)
	if wv.Backstory != "背景文本" {
		t.Fatalf("Backstory 应取世界背景: %s", wv.Backstory)
	}
	if len(wv.Lore) != 1 || wv.Lore[0].Title != "手工" {
		t.Fatalf("只应携带手工条目: %+v", wv.Lore)
	}
}
