// 阶段性剧情摘要（SummaryService）测试。
package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newSummaryTestEnv 构造世界引擎（内存 SQLite）+ 注入假 LLM 的 SummaryService。
func newSummaryTestEnv(t *testing.T, complete func(worldID, task, prompt string) (string, error)) (*SummaryService, *world.Engine) {
	t.Helper()
	engine := newAgentTestEngine(t)
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

// 攒够一批 → 追加一条阶段摘要（带轮次范围），只消化一个批次
func TestMaybeSummarize_AppendsStageEntry(t *testing.T) {
	var gotPrompt string
	s, engine := newSummaryTestEnv(t, func(worldID, task, prompt string) (string, error) {
		gotPrompt = prompt
		return "玩家修好了屋顶，老陈欠下人情。", nil
	})
	// 攒两批的量：一次调用只总结最早一批
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk+2)

	s.MaybeSummarize("w1")

	ws := engine.LoadOrNil("w1")
	if len(ws.Summaries) != 1 {
		t.Fatalf("应追加一条阶段摘要: %+v", ws.Summaries)
	}
	entry := ws.Summaries[0]
	if entry.ID != 1 || entry.Text != "玩家修好了屋顶，老陈欠下人情。" {
		t.Fatalf("摘要条目错误: %+v", entry)
	}
	if entry.RoundFrom != 0 || entry.RoundTo != DefaultSummaryChunk-1 {
		t.Fatalf("轮次范围应覆盖本批次: %+v", entry)
	}
	if len(ws.PendingSummaryTurns) != 2 {
		t.Fatalf("应只消化一个批次，剩余 2 轮: %d", len(ws.PendingSummaryTurns))
	}
	// prompt：无既有摘要时标注"故事开头"，且携带本批次对话
	for _, want := range []string{"故事的开头", "玩家:", "世界:", "行动0", "叙事0"} {
		if !strings.Contains(gotPrompt, want) {
			t.Fatalf("prompt 应包含 %q:\n%s", want, gotPrompt)
		}
	}
	// 不应包含本批次之外的轮次（只总结最早一批）
	if strings.Contains(gotPrompt, fmt.Sprintf("行动%d", DefaultSummaryChunk)) {
		t.Fatalf("prompt 不应包含下一批次的对话:\n%s", gotPrompt)
	}
}

// 已有摘要时：上一条进入 prompt 供衔接（但新条目独立追加，不合并改写旧条目）
func TestMaybeSummarize_PrevEntryForContinuity(t *testing.T) {
	var gotPrompt string
	s, engine := newSummaryTestEnv(t, func(worldID, task, prompt string) (string, error) {
		gotPrompt = prompt
		return "玩家告别老陈，踏上北上的路。", nil
	})
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk)
	ws := engine.LoadOrNil("w1")
	ws.AppendSummary("玩家来到村里。", 0, 5, "")
	if err := engine.Save(ws); err != nil {
		t.Fatal(err)
	}

	s.MaybeSummarize("w1")

	ws = engine.LoadOrNil("w1")
	if len(ws.Summaries) != 2 {
		t.Fatalf("应追加为第二条: %+v", ws.Summaries)
	}
	if ws.Summaries[0].Text != "玩家来到村里。" {
		t.Fatalf("旧条目不应被改写: %+v", ws.Summaries[0])
	}
	if ws.Summaries[1].ID != 2 || ws.Summaries[1].Text != "玩家告别老陈，踏上北上的路。" {
		t.Fatalf("新条目错误: %+v", ws.Summaries[1])
	}
	if !strings.Contains(gotPrompt, "玩家来到村里。") {
		t.Fatalf("prompt 应携带上一条摘要供衔接:\n%s", gotPrompt)
	}
}

// 旧数据（仅 CampaignSummary）首次总结时迁移为首条
func TestMaybeSummarize_LegacyMigration(t *testing.T) {
	s, engine := newSummaryTestEnv(t, func(worldID, task, prompt string) (string, error) {
		return "新阶段摘要。", nil
	})
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk)
	ws := engine.LoadOrNil("w1")
	ws.CampaignSummary = "旧滚动摘要。"
	if err := engine.Save(ws); err != nil {
		t.Fatal(err)
	}

	s.MaybeSummarize("w1")

	ws = engine.LoadOrNil("w1")
	if ws.CampaignSummary != "" {
		t.Fatalf("legacy 字段应被清空: %q", ws.CampaignSummary)
	}
	if len(ws.Summaries) != 2 || ws.Summaries[0].Text != "旧滚动摘要。" || ws.Summaries[1].Text != "新阶段摘要。" {
		t.Fatalf("旧摘要应迁移为首条: %+v", ws.Summaries)
	}
}

// 摘要 LLM 期间回合继续推进（Revision 变化）不应丢弃结果——旧实现用整份
// 状态 Revision 相等校验，高频会话里摘要会长期失败并反复重试。
func TestMaybeSummarize_SucceedsWhenWorldAdvanced(t *testing.T) {
	var eng *world.Engine
	s, engine := newSummaryTestEnv(t, func(worldID, task, prompt string) (string, error) {
		ws := eng.LoadOrNil(worldID)
		if ws == nil {
			return "", fmt.Errorf("世界不存在")
		}
		ws.RoundCount += 3 // 模拟摘要期间有新回合落库
		if err := eng.Save(ws); err != nil {
			return "", err
		}
		return "世界推进后仍应落库的摘要。", nil
	})
	eng = engine
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk)

	s.MaybeSummarize("w1")

	ws := engine.LoadOrNil("w1")
	if len(ws.Summaries) != 1 || ws.Summaries[0].Text != "世界推进后仍应落库的摘要。" {
		t.Fatalf("回合推进不应使摘要结果失效: %+v", ws.Summaries)
	}
	if len(ws.PendingSummaryTurns) != 0 {
		t.Fatalf("本批次应被消费: %d", len(ws.PendingSummaryTurns))
	}
}

// 待总结队列前缀变化（如溢出丢最旧）时应丢弃结果，避免消费错位。
func TestMaybeSummarize_DiscardsWhenPendingChanged(t *testing.T) {
	var eng *world.Engine
	s, engine := newSummaryTestEnv(t, func(worldID, task, prompt string) (string, error) {
		ws := eng.LoadOrNil(worldID)
		if ws == nil {
			return "", fmt.Errorf("世界不存在")
		}
		ws.PendingSummaryTurns = ws.PendingSummaryTurns[1:]
		if err := eng.Save(ws); err != nil {
			return "", err
		}
		return "不应落库的摘要。", nil
	})
	eng = engine
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk)

	s.MaybeSummarize("w1")

	ws := engine.LoadOrNil("w1")
	if len(ws.Summaries) != 0 {
		t.Fatalf("队列已变化时不应追加摘要: %+v", ws.Summaries)
	}
	if len(ws.PendingSummaryTurns) != DefaultSummaryChunk-1 {
		t.Fatalf("待总结队列不应被消费: %d", len(ws.PendingSummaryTurns))
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
	if len(ws.Summaries) != 0 {
		t.Fatalf("失败时不应追加摘要: %+v", ws.Summaries)
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
