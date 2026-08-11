// 短期对话窗口（RecentTurns）测试。
package agent

import (
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func TestAppendTurnRingBuffer(t *testing.T) {
	ws := world.NewWorldState("w1", world.ModeSimRPG)
	var evictedTotal []world.DialogueTurn
	for i := 0; i < world.RecentTurnsCap+5; i++ {
		evictedTotal = append(evictedTotal, ws.AppendTurn("玩家行动", "KP 叙事")...)
	}
	if len(ws.RecentTurns) != world.RecentTurnsCap {
		t.Fatalf("应环形保留 %d 轮，实际 %d", world.RecentTurnsCap, len(ws.RecentTurns))
	}
	if ws.RecentTurns[0].Player != "玩家行动" || ws.RecentTurns[0].Narrator != "KP 叙事" {
		t.Fatalf("内容错误: %+v", ws.RecentTurns[0])
	}
	// 超出容量的 5 轮应作为淘汰轮返回（供 PendingSummaryTurns 积累）
	if len(evictedTotal) != 5 {
		t.Fatalf("应淘汰 5 轮，实际 %d", len(evictedTotal))
	}
}

func TestBuildDialogueBlock(t *testing.T) {
	// 空状态
	if buildDialogueBlock(nil, "") != "" {
		t.Fatal("nil 状态应为空")
	}
	ws := world.NewWorldState("w1", world.ModeSimRPG)
	if buildDialogueBlock(ws, "") != "" {
		t.Fatal("无对话记录应为空")
	}

	ws.AppendTurn("我检查书桌", "桌上有一封未寄出的信。")
	ws.AppendTurn("我读那封信", "信中提到了雾中灯塔。")
	block := buildDialogueBlock(ws, "")
	for _, want := range []string{"【近期对话】", "玩家: 我检查书桌", "世界: 桌上有一封未寄出的信。", "世界: 信中提到了雾中灯塔。"} {
		if !strings.Contains(block, want) {
			t.Fatalf("对话块应包含 %q: %s", want, block)
		}
	}

	// 人设名作为玩家侧标签
	named := buildDialogueBlock(ws, "林月")
	if !strings.Contains(named, "林月: 我检查书桌") {
		t.Fatalf("应用人设名作玩家标签: %s", named)
	}
}

func TestContextBuilderWithDialogue(t *testing.T) {
	ws := world.NewWorldState("w1", world.ModeSimRPG)
	lore := &world.LoreResult{}
	turns := []world.DialogueTurn{{Player: "我检查书桌", Narrator: "桌上有一封信。"}}
	dialogue := "【近期对话】\n玩家: 我检查书桌\n世界: 桌上有一封信。\n"
	msg := NewContextBuilder(6000).BuildNarratorInput(ws, lore, nil, "", "", turns, "我读信", "", "", "").Flatten()
	if !strings.Contains(msg, dialogue) {
		t.Fatal("上下文应包含近期对话块")
	}
	// 对话块应在玩家消息之前
	if strings.Index(msg, dialogue) > strings.Index(msg, "玩家: 我读信") {
		t.Fatal("近期对话应位于当前玩家消息之前")
	}

	// 近期对话为固定轮数不裁剪：超预算时可选分区被裁，对话窗口与玩家消息保留
	tiny := NewContextBuilder(10).BuildNarratorInput(ws, lore, nil, "", "", turns, "我读信", "", "", "").Flatten()
	if !strings.Contains(tiny, "玩家: 我读信") {
		t.Fatal("必需分区（玩家消息）不应被裁掉")
	}
	if !strings.Contains(tiny, dialogue) {
		t.Fatal("近期对话窗口不应被裁掉")
	}
}
