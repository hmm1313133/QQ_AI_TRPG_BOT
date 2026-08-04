// 剧本条目化（P4）测试。
package world

import (
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
)

func TestScriptLoreEntries(t *testing.T) {
	scr := &script.Script{
		ID:   "s1",
		Name: "测试剧本",
		Background: script.StoryBackground{
			Synopsis:  "调查员前往寒鸦堡寻找失踪的学者。",
			MainTheme: "悬疑",
			Tone:      "压抑",
			Setting:   "1920 年代的北方边境",
			Location:  "寒鸦堡",
			Era:       "", // 空字段应跳过
		},
		Timeline: []script.TimelineNode{
			{ID: "node_1", Name: "抵达寒鸦堡", Description: "玩家抵达要塞。", Order: 1, NPCs: []string{"老陈"}},
		},
		Characters: []script.ScriptCharacter{
			{ID: "c1", Name: "老陈", Personality: "沉默寡言", Background: "守堡人"},
		},
	}

	entries := ScriptLoreEntries(scr)
	byID := make(map[string]LoreEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
		if e.Source != LoreSourceScript || !e.Enabled {
			t.Fatalf("条目来源/启用状态不正确: %+v", e)
		}
	}

	// 主线恒定条目
	main := byID["lor_script_bg_main"]
	if !main.Constant || main.Priority != 100 || main.Category != LoreCategoryBackground {
		t.Fatalf("主线条目不正确: %+v", main)
	}
	for _, want := range []string{"调查员前往寒鸦堡", "悬疑", "压抑"} {
		if !strings.Contains(main.Content, want) {
			t.Fatalf("主线内容缺少 %q: %s", want, main.Content)
		}
	}

	// Location：字段值本身作 key
	loc := byID["lor_script_bg_location"]
	if len(loc.Keys) != 1 || loc.Keys[0] != "寒鸦堡" {
		t.Fatalf("Location 关键词应为字段值本身: %+v", loc.Keys)
	}
	// Setting：字段名（中文标签）+ 值前 2 个词
	setting := byID["lor_script_bg_setting"]
	if len(setting.Keys) < 2 || setting.Keys[0] != "世界观概述" {
		t.Fatalf("Setting 关键词不正确: %+v", setting.Keys)
	}
	// 空 Era 不生成条目
	if _, ok := byID["lor_script_bg_era"]; ok {
		t.Fatal("空字段不应生成条目")
	}

	// 时间轴节点
	node := byID["lor_script_node_node_1"]
	if node.Priority != 50 || node.Keys[0] != "抵达寒鸦堡" || node.Keys[1] != "老陈" {
		t.Fatalf("节点条目不正确: %+v", node)
	}

	// 角色
	ch := byID["lor_script_char_c1"]
	if ch.Category != LoreCategoryCharacter || ch.Priority != 60 || ch.Keys[0] != "老陈" {
		t.Fatalf("角色条目不正确: %+v", ch)
	}
	if !strings.Contains(ch.Content, "沉默寡言") || !strings.Contains(ch.Content, "守堡人") {
		t.Fatalf("角色内容不正确: %s", ch.Content)
	}
}

func TestScriptLoreEntriesEmpty(t *testing.T) {
	// 无 Background/Timeline/Characters 时返回空，行为与条目化之前一致
	if entries := ScriptLoreEntries(&script.Script{}); len(entries) != 0 {
		t.Fatalf("空剧本不应生成条目: %d", len(entries))
	}
	if entries := ScriptLoreEntries(nil); entries != nil {
		t.Fatal("nil 剧本应返回 nil")
	}
}

func TestScriptFieldKeys(t *testing.T) {
	// selfKey：字段值本身
	if keys := scriptFieldKeys("主要地点", "寒鸦堡", true); len(keys) != 1 || keys[0] != "寒鸦堡" {
		t.Fatalf("selfKey 不正确: %+v", keys)
	}
	// 非 selfKey：字段名 + 前 2 个词，单字词丢弃
	keys := scriptFieldKeys("时代背景", "1920s a b c", false)
	if len(keys) != 2 || keys[0] != "时代背景" || keys[1] != "1920s" {
		t.Fatalf("分词提取不正确: %+v", keys)
	}
	// 单字值 selfKey 丢弃
	if keys := scriptFieldKeys("主要地点", "堡", true); len(keys) != 0 {
		t.Fatalf("单字 key 应丢弃: %+v", keys)
	}
}
