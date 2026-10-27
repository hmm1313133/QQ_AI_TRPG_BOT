package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s, err := NewStore(db)
	if err != nil {
		t.Fatalf("初始化记忆存储失败: %v", err)
	}
	return s
}

func TestStore_Dedupe(t *testing.T) {
	s := newTestStore(t)
	c := Chunk{WorldID: "w1", Kind: KindEvent, Text: "老陈的态度变为 hostile", DedupeKey: "1|npc_disposition|老陈"}
	id1, err := s.Append(c)
	if err != nil || id1 == 0 {
		t.Fatalf("首次写入失败: id=%d err=%v", id1, err)
	}
	id2, err := s.Append(c)
	if err != nil || id2 != id1 {
		t.Fatalf("重复写入应命中去重并返回同一 ID: id1=%d id2=%d err=%v", id1, id2, err)
	}
	if n, _ := s.Count("w1"); n != 1 {
		t.Fatalf("去重后应只有 1 条: %d", n)
	}
}

// 两字中文专名（老陈）走实体路径：trigram 需要 ≥3 字，不能依赖 FTS。
func TestStore_RetrieveByEntityTwoCharName(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Append(Chunk{
		WorldID: "w1", Kind: KindEvent, Subject: "老陈", RoundTo: 10,
		Entities: []string{"老陈", "寒鸦堡"}, Importance: 8,
		Text: "老陈被玩家救下，答应带路",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(Chunk{
		WorldID: "w1", Kind: KindEvent, Subject: "路人", RoundTo: 11,
		Text: "集市上有人吵架",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Retrieve(Query{WorldID: "w1", Entities: []string{"老陈"}, Round: 12, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Subject != "老陈" {
		t.Fatalf("两字 NPC 名应命中实体记忆: %+v", got)
	}
}

func TestStore_RetrieveByKeywordChinese(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Append(Chunk{
		WorldID: "w1", Kind: KindEvent, RoundTo: 5, Importance: 6,
		Text: "寒鸦堡下着大雪，城门紧闭",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Retrieve(Query{WorldID: "w1", Text: "我想去寒鸦堡看看", Round: 6, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || !strings.Contains(got[0].Text, "寒鸦堡") {
		t.Fatalf("中文关键词应命中 FTS: %+v", got)
	}
}

func TestStore_EpisodesOrderAndLimit(t *testing.T) {
	s := newTestStore(t)
	for i := 1; i <= 5; i++ {
		if _, err := s.Append(Chunk{
			WorldID: "w1", Kind: KindEpisode, RoundFrom: i * 6, RoundTo: i*6 + 5,
			Text: "阶段摘要", DedupeKey: fmt.Sprintf("episode|%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.Episodes("w1", 0)
	if err != nil || len(all) != 5 {
		t.Fatalf("应返回全部摘要: n=%d err=%v", len(all), err)
	}
	if all[0].RoundFrom != 6 || all[4].RoundFrom != 30 {
		t.Fatalf("摘要应按时间升序: %+v", all)
	}
	last3, err := s.Episodes("w1", 3)
	if err != nil || len(last3) != 3 {
		t.Fatalf("应只返回最近 3 条: n=%d err=%v", len(last3), err)
	}
	if last3[0].RoundFrom != 18 || last3[2].RoundFrom != 30 {
		t.Fatalf("最近 3 条仍应升序: %+v", last3)
	}
}

func TestStore_UpdateAndDelete(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Append(Chunk{WorldID: "w1", Kind: KindEpisode, Text: "旧摘要"})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.UpdateText("w1", id, " 新摘要 ")
	if err != nil || !ok {
		t.Fatalf("编辑失败: ok=%v err=%v", ok, err)
	}
	eps, _ := s.Episodes("w1", 1)
	if len(eps) != 1 || eps[0].Text != "新摘要" {
		t.Fatalf("编辑应写回并 TrimSpace: %+v", eps)
	}
	if ok, err := s.UpdateText("w2", id, "x"); err != nil || ok {
		t.Fatalf("跨世界不应可改: ok=%v err=%v", ok, err)
	}
	if ok, err := s.Delete("w1", id); err != nil || !ok {
		t.Fatalf("删除失败: ok=%v err=%v", ok, err)
	}
	if n, _ := s.Count("w1"); n != 0 {
		t.Fatalf("删除后应为 0: %d", n)
	}
}

// 层级压缩：6 条阶段摘要合并为 1 条篇章摘要，子摘要归档后不再进时间线。
func TestStore_ArcCompaction(t *testing.T) {
	s := newTestStore(t)
	ids := make([]int64, 0, DefaultTestArcWindow)
	for i := 0; i < DefaultTestArcWindow; i++ {
		id, err := s.Append(Chunk{
			WorldID: "w1", Kind: KindEpisode, Level: LevelChunk,
			RoundFrom: i * 6, RoundTo: i*6 + 5,
			Text: fmt.Sprintf("阶段%d", i), DedupeKey: fmt.Sprintf("ep|%d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if eps, _ := s.UncompactedEpisodes("w1", DefaultTestArcWindow); len(eps) != DefaultTestArcWindow {
		t.Fatalf("未归档摘要应有 %d 条: %d", DefaultTestArcWindow, len(eps))
	}

	arc := Chunk{WorldID: "w1", Kind: KindEpisode, RoundFrom: 0, RoundTo: 35, Text: "篇章：玩家在边境小镇立足。", DedupeKey: "arc|0-35"}
	arcID, err := s.InsertArc(arc, ids)
	if err != nil || arcID == 0 {
		t.Fatalf("写入篇章摘要失败: id=%d err=%v", arcID, err)
	}
	if eps, _ := s.UncompactedEpisodes("w1", 10); len(eps) != 0 {
		t.Fatalf("子摘要应全部归档: %d", len(eps))
	}

	tl, err := s.Timeline("w1", 6, 12)
	if err != nil || len(tl) != 1 {
		t.Fatalf("时间线应只剩篇章摘要: n=%d err=%v", len(tl), err)
	}
	if tl[0].Level != LevelArc || tl[0].Text != arc.Text {
		t.Fatalf("时间线条目应为 arc: %+v", tl[0])
	}

	// 重复合并同一轮次范围：复用已有 arc，不产生重复条目
	arcID2, err := s.InsertArc(arc, ids)
	if err != nil || arcID2 != arcID {
		t.Fatalf("重复 arc 应去重: id1=%d id2=%d err=%v", arcID, arcID2, err)
	}
	if n, _ := s.Count("w1"); n != DefaultTestArcWindow+1 {
		t.Fatalf("重复合并不应新增条目: %d", n)
	}
}

// 实体记忆：按 NPC 取 importance 最高的条目。
func TestStore_SubjectMemories(t *testing.T) {
	s := newTestStore(t)
	low, _ := s.Append(Chunk{WorldID: "w1", Kind: KindEvent, Subject: "老陈", Text: "老陈提到过北境", Importance: 4})
	high, _ := s.Append(Chunk{WorldID: "w1", Kind: KindEvent, Subject: "老陈", Text: "老陈被玩家救下", Importance: 9})
	if _, err := s.Append(Chunk{WorldID: "w1", Kind: KindEvent, Subject: "老王", Text: "老王离开小镇", Importance: 9}); err != nil {
		t.Fatal(err)
	}
	got, err := s.SubjectMemories("w1", "老陈", 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("应返回老陈的 2 条记忆: n=%d err=%v", len(got), err)
	}
	if got[0].ID != high || got[1].ID != low {
		t.Fatalf("应按 importance 降序: %+v", got)
	}
}

const DefaultTestArcWindow = 6

// 重试回滚：删除指定回合起的事件记忆，阶段摘要不受影响。
func TestStore_DeleteRound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Append(Chunk{WorldID: "w1", Kind: KindEvent, RoundFrom: 4, RoundTo: 4, Text: "旧版：老陈敌对", Importance: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(Chunk{WorldID: "w1", Kind: KindEvent, RoundFrom: 3, RoundTo: 3, Text: "上一回合"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(Chunk{WorldID: "w1", Kind: KindEpisode, RoundFrom: 4, RoundTo: 4, Text: "阶段摘要"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.DeleteRound("w1", KindEvent, 4)
	if err != nil || n != 1 {
		t.Fatalf("应只删除第 4 轮的事件记忆: n=%d err=%v", n, err)
	}
	eps, _ := s.Episodes("w1", 0)
	if len(eps) != 1 {
		t.Fatalf("阶段摘要不应被删除: %+v", eps)
	}
	if cnt, _ := s.Count("w1"); cnt != 2 {
		t.Fatalf("应剩上一回合事件 + 阶段摘要: %d", cnt)
	}
}

func TestStore_RetrieveBudget(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 3; i++ {
		if _, err := s.Append(Chunk{
			WorldID: "w1", Kind: KindEvent, RoundTo: i, Importance: 9,
			Text: strings.Repeat("记", 100),
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Retrieve(Query{WorldID: "w1", Round: 3, Limit: 10, BudgetChars: 250})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("预算 250 应只装下 2 条（每条 108）: %d", len(got))
	}
}
