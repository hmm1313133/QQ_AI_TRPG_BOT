// 短期对话窗口（RecentTurns）测试。
package agent

import (
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func TestAppendTurnRingBuffer(t *testing.T) {
	ws := world.NewWorldState("w1", world.ModeSimRPG)
	for i := 0; i < world.RecentTurnsCap+5; i++ {
		ws.AppendTurn("玩家行动", "KP 叙事")
	}
	if len(ws.RecentTurns) != world.RecentTurnsCap {
		t.Fatalf("应环形保留 %d 轮，实际 %d", world.RecentTurnsCap, len(ws.RecentTurns))
	}
	if ws.RecentTurns[0].Player != "玩家行动" || ws.RecentTurns[0].Narrator != "KP 叙事" {
		t.Fatalf("内容错误: %+v", ws.RecentTurns[0])
	}
}

func TestBuildDialogueBlock(t *testing.T) {
	// 空状态
	if buildDialogueBlock(nil) != "" {
		t.Fatal("nil 状态应为空")
	}
	ws := world.NewWorldState("w1", world.ModeSimRPG)
	if buildDialogueBlock(ws) != "" {
		t.Fatal("无对话记录应为空")
	}

	ws.AppendTurn("我检查书桌", "桌上有一封未寄出的信。")
	ws.AppendTurn("我读那封信", "信中提到了雾中灯塔。")
	block := buildDialogueBlock(ws)
	for _, want := range []string{"【近期对话】", "玩家: 我检查书桌", "KP: 桌上有一封未寄出的信。", "KP: 信中提到了雾中灯塔。"} {
		if !strings.Contains(block, want) {
			t.Fatalf("对话块应包含 %q: %s", want, block)
		}
	}
}

func TestContextBuilderWithDialogue(t *testing.T) {
	ws := world.NewWorldState("w1", world.ModeSimRPG)
	lore := &world.LoreResult{}
	dialogue := "【近期对话】\n玩家: 我检查书桌\nKP: 桌上有一封信。\n"
	msg := NewContextBuilder(6000).BuildNarratorMessage(ws, lore, nil, "", "", dialogue, "我读信")
	if !strings.Contains(msg, dialogue) {
		t.Fatal("上下文应包含近期对话块")
	}
	// 对话块应在玩家消息之前
	if strings.Index(msg, dialogue) > strings.Index(msg, "玩家: 我读信") {
		t.Fatal("近期对话应位于当前玩家消息之前")
	}

	// 超预算时对话块作为可选分区被裁剪（不 panic、必需分区保留）
	tiny := NewContextBuilder(10).BuildNarratorMessage(ws, lore, nil, "", "", dialogue, "我读信")
	if !strings.Contains(tiny, "玩家: 我读信") {
		t.Fatal("必需分区（玩家消息）不应被裁掉")
	}
}
