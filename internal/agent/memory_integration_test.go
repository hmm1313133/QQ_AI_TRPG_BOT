// 长期记忆与回合链路整合测试：迁移 / 事件写入 / 摘要写入 / 上下文注入。
package agent

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/memory"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newMemoryTestEnv 使用内存 SQLite（不落盘，避免 Windows 临时目录清理竞态）。
func newMemoryTestEnv(t *testing.T) (*world.Engine, *memory.Store) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	repo, err := world.NewSQLiteRepository(db)
	if err != nil {
		t.Fatalf("创建世界仓库失败: %v", err)
	}
	store, err := memory.NewStore(db)
	if err != nil {
		t.Fatalf("创建记忆存储失败: %v", err)
	}
	return world.NewEngine(repo), store
}

// 旧 Summaries/CampaignSummary 直接迁移进 world_memory，并清空旧字段；重复迁移幂等。
func TestMigrateSummariesToMemory(t *testing.T) {
	engine, store := newMemoryTestEnv(t)
	ws, err := engine.CreateWorld("w1", world.ModeSimRPG)
	if err != nil {
		t.Fatal(err)
	}
	ws.CampaignSummary = "旧滚动摘要。"
	ws.AppendSummary("阶段一。", 0, 5, "")
	ws.AppendSummary("阶段二。", 6, 11, "")
	if err := engine.Save(ws); err != nil {
		t.Fatal(err)
	}

	te := NewTurnEngine(nil, nil, nil, engine, nil, 0)
	te.SetMemory(store)

	loaded := engine.LoadOrNil("w1")
	te.migrateSummaries(loaded)

	eps, err := store.Episodes("w1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 3 || eps[0].Text != "旧滚动摘要。" || eps[2].Text != "阶段二。" {
		t.Fatalf("三条旧摘要应按时间迁移: %+v", eps)
	}
	got := engine.LoadOrNil("w1")
	if len(got.Summaries) != 0 || got.CampaignSummary != "" {
		t.Fatalf("迁移成功后应清空旧字段: %+v / %q", got.Summaries, got.CampaignSummary)
	}

	// 幂等：再跑一次不产生重复条目
	te.migrateSummaries(got)
	if eps2, _ := store.Episodes("w1", 0); len(eps2) != 3 {
		t.Fatalf("重复迁移不应新增: %d", len(eps2))
	}
}

// 回合记账应把本回合事件（含 guidance 回写与玩家决策）规则化写入记忆。
func TestBookkeepWritesEventMemories(t *testing.T) {
	engine, store := newMemoryTestEnv(t)
	ws, err := engine.CreateWorld("w1", world.ModeSimRPG)
	if err != nil {
		t.Fatal(err)
	}
	ws.Characters["老陈"] = &world.CharacterState{Name: "老陈", Kind: "npc", Alive: true, Disposition: "neutral"}
	roundStart := ws.RoundCount
	if _, err := engine.ApplyEvent(ws, world.WorldEvent{
		Type: "npc_disposition", Target: "老陈", Value: "hostile", Actor: "guidance",
	}); err != nil {
		t.Fatal(err)
	}

	te := NewTurnEngine(nil, nil, nil, engine, nil, 0)
	te.SetMemory(store)
	if err := te.bookkeep(ws, "w1", "我逼问老陈", "老陈翻脸。", roundStart); err != nil {
		t.Fatal(err)
	}

	chunks, err := store.Retrieve(memory.Query{WorldID: "w1", Entities: []string{"老陈"}, Round: ws.RoundCount, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var hasDisposition, hasDecision bool
	for _, c := range chunks {
		if strings.Contains(c.Text, "老陈 转为敌对") {
			hasDisposition = true
			if c.Importance != 7 {
				t.Fatalf("敌对事件重要性应为 7: %+v", c)
			}
		}
		if strings.Contains(c.Text, "玩家决定：我逼问老陈") {
			hasDecision = true
		}
	}
	if !hasDisposition || !hasDecision {
		t.Fatalf("事件与决策都应写入记忆: %+v", chunks)
	}
}

// 摘要服务在注入记忆存储后，阶段摘要写 world_memory，不再写 WorldState.Summaries。
func TestMaybeSummarize_WritesEpisodeToMemory(t *testing.T) {
	engine, store := newMemoryTestEnv(t)
	s := NewSummaryService(engine, nil, nil)
	s.SetMemory(store)
	s.complete = func(worldID, task, prompt string) (string, error) {
		return "玩家修好屋顶，老陈欠下人情。", nil
	}
	seedPendingWorld(t, engine, "w1", DefaultSummaryChunk)

	s.MaybeSummarize("w1")

	eps, err := store.Episodes("w1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].Text != "玩家修好屋顶，老陈欠下人情。" {
		t.Fatalf("摘要应写入长期记忆: %+v", eps)
	}
	if eps[0].RoundFrom != 0 || eps[0].RoundTo != DefaultSummaryChunk-1 {
		t.Fatalf("轮次范围应覆盖批次: %+v", eps[0])
	}
	ws := engine.LoadOrNil("w1")
	if len(ws.Summaries) != 0 {
		t.Fatalf("直接迁移后不应再写 Summaries: %+v", ws.Summaries)
	}
	if len(ws.PendingSummaryTurns) != 0 {
		t.Fatalf("本批次应被消费: %d", len(ws.PendingSummaryTurns))
	}
}

// 层级压缩：攒够 DefaultArcWindow 条阶段摘要后合并为一条篇章摘要（level=2）。
func TestMaybeCompact_MergesEpisodes(t *testing.T) {
	engine, store := newMemoryTestEnv(t)
	s := NewSummaryService(engine, nil, nil)
	s.SetMemory(store)
	var gotTask string
	s.complete = func(worldID, task, prompt string) (string, error) {
		gotTask = task
		if task == "archivist" {
			if !strings.Contains(prompt, "[第0-5轮]") {
				t.Errorf("篇章 prompt 应带轮次范围:\n%s", prompt)
			}
			return "篇章摘要：玩家在边境小镇立足，与老陈结下人情。", nil
		}
		return "阶段摘要", nil
	}
	for i := 0; i < DefaultArcWindow; i++ {
		if _, err := store.Append(memory.Chunk{
			WorldID: "w1", Kind: memory.KindEpisode, Level: memory.LevelChunk,
			RoundFrom: i * 6, RoundTo: i*6 + 5,
			Text: fmt.Sprintf("阶段%d", i), DedupeKey: fmt.Sprintf("ep|%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	s.MaybeCompact("w1")

	if gotTask != "archivist" {
		t.Fatalf("应调用篇章合并任务: %q", gotTask)
	}
	tl, err := store.Timeline("w1", 6, 12)
	if err != nil || len(tl) != 1 {
		t.Fatalf("时间线应只剩篇章摘要: n=%d err=%v", len(tl), err)
	}
	if tl[0].Level != memory.LevelArc || !strings.Contains(tl[0].Text, "边境小镇") {
		t.Fatalf("篇章摘要内容错误: %+v", tl[0])
	}
}

// 在场 NPC 记忆块：取归属于该 NPC 的高重要性记忆，并被其他分区去重。
func TestBuildNPCMemoryBlock(t *testing.T) {
	engine, store := newMemoryTestEnv(t)
	ws := world.NewWorldState("w1", world.ModeUnified)
	ws.Scene.NodeName = "酒馆"
	ws.Characters["老陈"] = &world.CharacterState{Name: "老陈", Alive: true, Location: "酒馆"}
	ws.Characters["老王"] = &world.CharacterState{Name: "老王", Alive: true, Location: "南海"}

	id, err := store.Append(memory.Chunk{
		WorldID: "w1", Kind: memory.KindEvent, Subject: "老陈",
		Text: "老陈欠玩家一个人情", Importance: 9, RoundTo: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	te := NewTurnEngine(nil, nil, nil, engine, nil, 0)
	te.SetMemory(store)
	block := te.buildNPCMemoryBlock("w1", ws, "我去找老陈", map[int64]bool{})
	if !strings.Contains(block, "老陈：") || !strings.Contains(block, "欠玩家一个人情") {
		t.Fatalf("NPC 记忆块错误:\n%s", block)
	}
	if strings.Contains(block, "老王") {
		t.Fatalf("不在场 NPC 不应注入: %s", block)
	}

	// 已进入其他分区（排除集）的记忆不再重复
	if dup := te.buildNPCMemoryBlock("w1", ws, "我去找老陈", map[int64]bool{id: true}); dup != "" {
		t.Fatalf("去重后应为空: %q", dup)
	}
}

// 关系变化事件的记忆归属应为 Value 里的 to（NPC），而不是关系起点 player。
func TestWorldEventToMemory_RelationChangeSubject(t *testing.T) {
	ws := world.NewWorldState("w1", world.ModeSimRPG)
	ws.Scene.NodeName = "酒馆"
	c, ok := worldEventToMemory(ws, world.WorldEvent{
		Type: "relation_change", Actor: "guidance", Target: "player",
		Value: "to=老陈,trust=-20",
	})
	if !ok {
		t.Fatal("关系变化应生成记忆块")
	}
	if c.Subject != "老陈" {
		t.Fatalf("NPC 记忆归属应为 to=老陈: %+v", c)
	}
	found := false
	for _, e := range c.Entities {
		if e == "老陈" {
			found = true
		}
	}
	if !found {
		t.Fatalf("实体应包含老陈: %+v", c.Entities)
	}

	// 管理端审计 note 不进记忆
	if _, ok := worldEventToMemory(ws, world.WorldEvent{
		Type: "note", Target: "section:summary", Value: "玩家编辑剧情摘要 #1",
	}); ok {
		t.Fatal("审计备注不应写入记忆")
	}
}

// 记忆分区注入：相关回忆与剧情回顾进入设定块，且超预算时可整体裁剪。
func TestBuildNarratorInputWithMemorySections(t *testing.T) {
	ws := world.NewWorldState("w", world.ModeSimRPG)
	in := NewContextBuilder(6000).BuildNarratorInputWithMemory(6000, ws, nil, nil, "", "", nil,
		"我推开门", "", "", "",
		"【相关回忆】\n  · [第3-3轮·酒馆·老陈] 老陈欠你一个人情\n",
		"【剧情回顾】\n  · [第0-5轮] 玩家来到边境小镇\n")

	if !strings.Contains(in.ContextBlock, "老陈欠你一个人情") {
		t.Fatalf("相关回忆应注入设定块:\n%s", in.ContextBlock)
	}
	if !strings.Contains(in.ContextBlock, "第0-5轮") {
		t.Fatalf("剧情回顾应注入设定块:\n%s", in.ContextBlock)
	}
	if strings.Index(in.ContextBlock, "老陈欠你一个人情") > strings.Index(in.ContextBlock, "【剧情回顾】") {
		t.Fatal("相关回忆应排在剧情回顾之前")
	}
}
