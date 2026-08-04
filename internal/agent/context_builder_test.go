package agent

import (
	"strings"
	"testing"

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
	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, &lore, nil, "", "", "", "我四处看看", "")

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
	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, &lore, nil, "", "", "", "我走进城门", "")

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
	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "")

	if !strings.Contains(msg, "【回复风格要求】") || !strings.Contains(msg, ws.ReplyStyle) {
		t.Fatalf("应注入回复风格分区:\n%s", msg)
	}
	if strings.Index(msg, ws.ReplyStyle) < strings.Index(msg, "玩家: 我推开门") {
		t.Fatal("回复风格应位于玩家消息之后（Author's Note 位置）")
	}

	// 空值不注入
	ws.ReplyStyle = ""
	msg2 := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", "")
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

	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, nil, nil, "", "", "", "我推开门", hint)
	if !strings.Contains(msg, hint) {
		t.Fatalf("应注入长度 hint:\n%s", msg)
	}
	if strings.Index(msg, hint) < strings.Index(msg, "玩家: 我推开门") {
		t.Fatal("长度 hint 应位于玩家消息之后")
	}

	// 必需分区豁免裁剪：预算为 1 时可选分区全裁，hint 仍保留
	tiny := NewContextBuilder(1).BuildNarratorMessage(ws, nil, nil, "可选上下文", "", "", "我推开门", hint)
	if !strings.Contains(tiny, hint) {
		t.Fatal("长度 hint 为必需分区，超预算也不应被裁剪")
	}
	if strings.Contains(tiny, "可选上下文") {
		t.Fatal("超预算时可选分区应被裁剪")
	}
}
