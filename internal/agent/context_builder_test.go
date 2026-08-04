package agent

import (
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// 旧世界等价性：Background 迁移为恒定条目后，注入文本仍包含背景全文与全部 NPC，
// 且不再出现独立的【故事背景】块。
func TestBuildNarratorMessage_LegacyWorldEquivalence(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)
	ws.Scene.NodeName = "边境小镇"
	ws.Background = "这是一个剑与魔法的世界，龙眠于北方。"
	ws.Characters["老王"] = &world.CharacterState{Name: "老王", Role: "店主", Alive: true, Disposition: "friendly"}
	ws.Characters["老陈"] = &world.CharacterState{Name: "老陈", Role: "卫兵", Alive: true, Disposition: "neutral"}
	world.MigrateLegacyBackground(ws)

	lore := world.Resolve(ws, "玩家四下张望", world.DefaultLoreBudget, false)
	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, &lore, nil, "", "", "", "我四处看看", "", "")

	if !strings.Contains(msg, ws.Background) {
		t.Fatal("迁移后背景全文应经 lore front 分区注入")
	}
	if strings.Contains(msg, "【故事背景】") {
		t.Fatal("状态摘要不应再包含独立的【故事背景】块")
	}
	if !strings.Contains(msg, "老王") || !strings.Contains(msg, "老陈") {
		t.Fatal("无地点信息的旧世界应注入全部 NPC（行为等价）")
	}
	// front 分区在状态摘要之前
	if strings.Index(msg, "【世界设定】") > strings.Index(msg, "【当前游戏运行态摘要】") {
		t.Fatal("lore front 应位于状态摘要之前")
	}
}

// front/tail 位置：front 在状态摘要之前，tail 在玩家消息之后（Author's Note 位置）。
func TestBuildNarratorMessage_LoreFrontTailPosition(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)
	ws.Lore = []world.LoreEntry{
		{ID: "lor_f", Title: "北境", Category: "geo", Keys: []string{"北境"},
			Position: "front", Priority: 60, Enabled: true, Content: "北境常年积雪", Source: "manual"},
		{ID: "lor_t", Title: "语气", Category: "style", Keys: []string{"北境"},
			Position: "tail", Priority: 60, Enabled: true, Content: "始终保持冷峻语气", Source: "manual"},
	}
	lore := world.Resolve(ws, "玩家踏入北境", world.DefaultLoreBudget, false)
	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, &lore, nil, "", "", "", "我走进城门", "", "")

	iFront := strings.Index(msg, "北境常年积雪")
	iState := strings.Index(msg, "【当前游戏运行态摘要】")
	iPlayer := strings.Index(msg, "玩家: 我走进城门")
	iTail := strings.Index(msg, "始终保持冷峻语气")
	if iFront < 0 || iState < 0 || iPlayer < 0 || iTail < 0 {
		t.Fatalf("装配缺分区:\n%s", msg)
	}
	if iFront > iState {
		t.Fatal("front 条目应在状态摘要之前")
	}
	if iTail < iPlayer {
		t.Fatal("tail 条目应在玩家消息之后")
	}
}

// NPC 按需注入：有地点信息时只注入在场 NPC；lore 命中的角色条目对应 NPC 补充注入。
func TestBuildGameStateSummary_NPCOnDemand(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)
	ws.Scene.NodeName = "寒鸦堡"
	ws.Characters["在场者"] = &world.CharacterState{Name: "在场者", Alive: true, Disposition: "neutral", Location: "寒鸦堡"}
	ws.Characters["远方者"] = &world.CharacterState{Name: "远方者", Alive: true, Disposition: "neutral", Location: "南海"}

	// 无 lore：远方者不注入
	msg := buildGameStateSummary(ws, nil)
	if !strings.Contains(msg, "在场者") || strings.Contains(msg, "远方者") {
		t.Fatalf("应按场景过滤在场 NPC:\n%s", msg)
	}

	// lore 命中角色条目 -> 对应 NPC 补充注入
	lore := &world.LoreResult{Front: []world.LoreHit{{Entry: world.LoreEntry{
		ID: "lor_c", Title: "远方者的人物小传", Category: world.LoreCategoryCharacter,
		Enabled: true, Content: "……", Source: "manual",
	}}}}
	msg2 := buildGameStateSummary(ws, lore)
	if !strings.Contains(msg2, "远方者") {
		t.Fatalf("lore 命中的角色条目对应 NPC 应注入:\n%s", msg2)
	}
}

// 回复风格分区：ReplyStyle 非空时注入且位于玩家消息之后；空值不注入。
func TestBuildNarratorMessage_ReplyStyleInjection(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)
	ws.ReplyStyle = "冷峻克苏鲁风，重对话少环境铺陈"
	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")

	if !strings.Contains(msg, "【回复风格要求】") || !strings.Contains(msg, ws.ReplyStyle) {
		t.Fatalf("应注入回复风格分区:\n%s", msg)
	}
	if strings.Index(msg, ws.ReplyStyle) < strings.Index(msg, "玩家: 我推开门") {
		t.Fatal("回复风格应位于玩家消息之后（Author's Note 位置）")
	}

	// 空值不注入
	ws.ReplyStyle = ""
	msg2 := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")
	if strings.Contains(msg2, "【回复风格要求】") {
		t.Fatal("ReplyStyle 为空时不应注入回复风格分区")
	}
}

// 回复长度 hint：注入且必需（超预算也不裁剪）。
func TestBuildNarratorMessage_LengthHint(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)
	hint := LengthHint(ReplyLengthShort)
	if hint == "" {
		t.Fatal("short 档位应有注入文本")
	}

	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", hint, "")
	if !strings.Contains(msg, hint) {
		t.Fatalf("应注入长度 hint:\n%s", msg)
	}
	if strings.Index(msg, hint) < strings.Index(msg, "玩家: 我推开门") {
		t.Fatal("长度 hint 应位于玩家消息之后")
	}

// 必需分区豁免裁剪：预算为 1 时可选分区全裁，hint 仍保留
	tiny := NewContextBuilder(1).BuildNarratorMessage(ws, nil, nil, "可选上下文", "", "", "我推开门", hint, "")
	if !strings.Contains(tiny, hint) {
		t.Fatal("长度 hint 为必需分区，超预算也不应被裁剪")
	}
	if strings.Contains(tiny, "可选上下文") {
		t.Fatal("超预算时可选分区应被裁剪")
	}
}

// 世界基调 Tone：front 恒定区注入，位于状态摘要之前（稳定分区保前缀缓存）。
func TestBuildNarratorMessage_StyleToneFront(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)
	ws.Style = &world.StyleConfig{Tone: "维多利亚蒸汽朋克，阴霾压抑"}
	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")

	if !strings.Contains(msg, "【世界基调】") || !strings.Contains(msg, ws.Style.Tone) {
		t.Fatalf("应注入世界基调分区:\n%s", msg)
	}
	if strings.Index(msg, ws.Style.Tone) > strings.Index(msg, "【当前游戏运行态摘要】") {
		t.Fatal("世界基调应位于状态摘要之前（front 恒定区）")
	}

	// Tone 为空不注入
	ws.Style.Tone = ""
	msg2 := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")
	if strings.Contains(msg2, "【世界基调】") {
		t.Fatal("Tone 为空时不应注入世界基调分区")
	}
}

// 风格核心回退：Style.Core 优先；Style 为 nil 或 Core 为空时回退旧 ReplyStyle 字段。
func TestBuildNarratorMessage_StyleCoreFallback(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)
	ws.ReplyStyle = "旧字段风格"
	ws.Style = &world.StyleConfig{Core: "新结构风格"}

	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")
	if !strings.Contains(msg, "新结构风格") || strings.Contains(msg, "旧字段风格") {
		t.Fatal("Style.Core 应优先于旧 ReplyStyle")
	}

	// Core 为空回退 ReplyStyle
	ws.Style.Core = ""
	msg2 := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")
	if !strings.Contains(msg2, "旧字段风格") {
		t.Fatal("Core 为空时应回退旧 ReplyStyle")
	}

	// Style 为 nil 回退 ReplyStyle
	ws.Style = nil
	msg3 := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")
	if !strings.Contains(msg3, "旧字段风格") {
		t.Fatal("Style 为 nil 时应回退旧 ReplyStyle")
	}
}

// 思维链三态：关闭不注入；开启用内置默认；自定义 CoTGuide 覆盖默认文本（保留【输出格式要求】头）。
func TestBuildNarratorMessage_CoTGuide(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)

	// 关闭：不注入
	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")
	if strings.Contains(msg, "【输出格式要求】") {
		t.Fatal("EnableCoT 关闭时不应注入输出格式要求")
	}

	// 开启 + 无自定义：注入内置默认，且位于回复风格之后
	ws.ReplyStyle = "冷峻"
	ws.Style = &world.StyleConfig{EnableCoT: true}
	msg2 := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")
	if !strings.Contains(msg2, DefaultCoTGuide) {
		t.Fatalf("开启 CoT 应注入内置默认指导:\n%s", msg2)
	}
	if strings.Index(msg2, DefaultCoTGuide) < strings.Index(msg2, "【回复风格要求】") {
		t.Fatal("思维链指导应位于回复风格分区之后")
	}

	// 开启 + 自定义：覆盖默认文本，保留【输出格式要求】头
	ws.Style.CoTGuide = "先推理 NPC 动机，再决定场景描写"
	msg3 := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")
	if !strings.Contains(msg3, ws.Style.CoTGuide) || strings.Contains(msg3, "检查判断") {
		t.Fatal("自定义 CoTGuide 应覆盖内置默认文本")
	}
	if !strings.Contains(msg3, "【输出格式要求】") {
		t.Fatal("自定义指导也应保留【输出格式要求】头")
	}
}

// 玩家人设分区：非空注入且位于玩家消息之前；空值不注入。
func TestBuildNarratorMessage_PersonaBlock(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)
	block := buildPersonaBlock(&persona.Profile{Name: "林月", Description: "冷静果断的私家侦探"})
	if block == "" || !strings.Contains(block, "林月: 冷静果断的私家侦探") {
		t.Fatalf("人设块格式化错误: %q", block)
	}

	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", block)
	if !strings.Contains(msg, "【玩家人设】") {
		t.Fatalf("应注入玩家人设分区:\n%s", msg)
	}
	if strings.Index(msg, "【玩家人设】") > strings.Index(msg, "玩家: 我推开门") {
		t.Fatal("玩家人设应位于玩家消息之前")
	}

	// 空值不注入
	msg2 := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "", "")
	if strings.Contains(msg2, "【玩家人设】") {
		t.Fatal("personaBlock 为空时不应注入人设分区")
	}

	// 名字为空时省略名字段
	if got := buildPersonaBlock(&persona.Profile{Description: "无名描述"}); strings.Contains(got, ": ") {
		t.Fatalf("名字为空应省略名字段: %q", got)
	}
}
