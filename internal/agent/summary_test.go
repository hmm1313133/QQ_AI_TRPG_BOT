// 滚动剧情摘要（SummaryService）测试。
package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newSummaryTestEnv 构造世界引擎（JSON 仓库存储）+ 注入假 LLM 的 SummaryService。
func newSummaryTestEnv(t *testing.T, complete func(worldID, task, prompt string) (string, error)) (*SummaryService, *world.Engine) {
	t.Helper()
	repo, err := world.NewJSONRepository(t.TempDir())
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}
	engine := world.NewEngine(repo)
	s := NewSummaryService(engine, nil, nil)
	s.complete = complete
	return s, engine
}

// seedPendingWorld 创建带有 N 轮待总结对话的世界。
func seedPendingWorld(t *testing.T, engine *world.Engine, worldID string, n int) {
	t.Helper()
	ws, err := engine.CreateWorld(worldID, world.ModeSimRPG)
	if err != nil {
		t.Fatalf("创建世界失败: %v", err)
	}
	for i := 0; i < n; i++ {
		ws.PendingSummaryTurns = append(ws.PendingSummaryTurns, world.DialogueTurn{
			Round: i, Player: fmt.Sprintf("行动%d", i), Narrator: fmt.Sprintf("叙事%d", i),
		})
	}
	if err := engine.Save(ws); err != nil {
		t.Fatalf("保存世界失败: %v", err)
	}
}

func TestMaybeSummarize_BelowThreshold(t *testing.T) {
	called := false
	s, engine := newSummaryTestEnv(t, func(worldID, task, prompt string) (string, error) {
		called = true
		return "摘要", nil
	})
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk-1)

	s.MaybeSummarize("w1")
	if called {
		t.Fatal("未攒够批次不应调用 LLM")
	}
	ws := engine.LoadOrNil("w1")
	if len(ws.PendingSummaryTurns) != DefaultSummaryChunk-1 {
		t.Fatalf("待总结队列应保留: %d", len(ws.PendingSummaryTurns))
	}
}

func TestMaybeSummarize_MergesIntoCampaignSummary(t *testing.T) {
	var gotPrompt string
	s, engine := newSummaryTestEnv(t, func(worldID, task, prompt string) (string, error) {
		gotPrompt = prompt
		return "玩家修好了屋顶，老陈欠下人情。", nil
	})
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk)
	// 已有摘要应进入 prompt 供合并
	ws := engine.LoadOrNil("w1")
	ws.CampaignSummary = "玩家来到村里。"
	if err := engine.Save(ws); err != nil {
		t.Fatal(err)
	}

	s.MaybeSummarize("w1")

	ws = engine.LoadOrNil("w1")
	if ws.CampaignSummary != "玩家修好了屋顶，老陈欠下人情。" {
		t.Fatalf("摘要应被合并写回: %q", ws.CampaignSummary)
	}
	if len(ws.PendingSummaryTurns) != 0 {
		t.Fatalf("成功后待总结队列应清空: %d", len(ws.PendingSummaryTurns))
	}
	// prompt 应携带已有摘要与待总结对话
	for _, want := range []string{"玩家来到村里。", "玩家:", "世界:", "行动0", "叙事0"} {
		if !strings.Contains(gotPrompt, want) {
			t.Fatalf("prompt 应包含 %q:\n%s", want, gotPrompt)
		}
	}
}

func TestMaybeSummarize_LLMFailureKeepsPending(t *testing.T) {
	s, engine := newSummaryTestEnv(t, func(worldID, task, prompt string) (string, error) {
		return "", fmt.Errorf("LLM 不可用")
	})
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk)

	s.MaybeSummarize("w1")

	ws := engine.LoadOrNil("w1")
	if len(ws.PendingSummaryTurns) != DefaultSummaryChunk {
		t.Fatalf("失败时待总结队列应保留: %d", len(ws.PendingSummaryTurns))
	}
	if ws.CampaignSummary != "" {
		t.Fatalf("失败时摘要不应被改写: %q", ws.CampaignSummary)
	}
}

func TestMaybeSummarize_EmptyReplyKeepsPending(t *testing.T) {
	s, engine := newSummaryTestEnv(t, func(worldID, task, prompt string) (string, error) {
		return "  ", nil
	})
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk)

	s.MaybeSummarize("w1")

	ws := engine.LoadOrNil("w1")
	if len(ws.PendingSummaryTurns) != DefaultSummaryChunk {
		t.Fatalf("空摘要时待总结队列应保留: %d", len(ws.PendingSummaryTurns))
	}
}
