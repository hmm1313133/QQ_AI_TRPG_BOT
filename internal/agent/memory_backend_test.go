package agent

import (
	"context"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"

	"trpc.group/trpc-go/trpc-agent-go/memory"
)

func newTestBackend(t *testing.T) (memory.Service, *world.MemoryStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := world.NewMemoryStore(dir)
	if err != nil {
		t.Fatalf("创建记忆存储失败: %v", err)
	}
	backend := NewJSONMemoryService(store, func(worldID string) int64 { return 1000 })
	return backend, store
}

func TestJSONMemoryService_AddAndRead(t *testing.T) {
	backend, _ := newTestBackend(t)
	ctx := context.Background()
	key := memory.UserKey{AppName: "w1", UserID: "老陈"}

	if err := backend.AddMemory(ctx, key, "玩家在店里买过伤药", []string{"交易"}); err != nil {
		t.Fatalf("AddMemory 失败: %v", err)
	}

	entries, err := backend.ReadMemories(ctx, key, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ReadMemories 应返回 1 条: %d, err=%v", len(entries), err)
	}
	if entries[0].Memory.Memory != "玩家在店里买过伤药" {
		t.Fatalf("内容不匹配: %s", entries[0].Memory.Memory)
	}
	if entries[0].ID == "" {
		t.Fatal("应分配记忆 ID")
	}
}

func TestJSONMemoryService_UpdateUpsert(t *testing.T) {
	backend, _ := newTestBackend(t)
	ctx := context.Background()
	key := memory.UserKey{AppName: "w1", UserID: "老陈"}

	backend.AddMemory(ctx, key, "原始内容", nil)
	entries, _ := backend.ReadMemories(ctx, key, 10)
	memID := entries[0].ID

	// 更新已存在的记忆
	err := backend.UpdateMemory(ctx, memory.Key{AppName: "w1", UserID: "老陈", MemoryID: memID}, "更新后的内容", []string{"新标签"})
	if err != nil {
		t.Fatalf("UpdateMemory 失败: %v", err)
	}
	entries, _ = backend.ReadMemories(ctx, key, 10)
	if len(entries) != 1 || entries[0].Memory.Memory != "更新后的内容" {
		t.Fatalf("更新未生效: %+v", entries)
	}

	// 更新不存在的记忆 → 按 ADD 语义追加
	err = backend.UpdateMemory(ctx, memory.Key{AppName: "w1", UserID: "老陈", MemoryID: "不存在"}, "追加内容", nil)
	if err != nil {
		t.Fatalf("Update upsert 失败: %v", err)
	}
	entries, _ = backend.ReadMemories(ctx, key, 10)
	if len(entries) != 2 {
		t.Fatalf("upsert 应追加新记忆: %d 条", len(entries))
	}
}

func TestJSONMemoryService_DeleteIsInvalidation(t *testing.T) {
	backend, store := newTestBackend(t)
	ctx := context.Background()
	key := memory.UserKey{AppName: "w1", UserID: "老陈"}

	backend.AddMemory(ctx, key, "待删除的记忆", nil)
	entries, _ := backend.ReadMemories(ctx, key, 10)
	memID := entries[0].ID

	if err := backend.DeleteMemory(ctx, memory.Key{AppName: "w1", UserID: "老陈", MemoryID: memID}); err != nil {
		t.Fatalf("DeleteMemory 失败: %v", err)
	}

	// 读取不到（已失效）
	entries, _ = backend.ReadMemories(ctx, key, 10)
	if len(entries) != 0 {
		t.Fatalf("已删除记忆不应被读取: %d 条", len(entries))
	}

	// 但物理条目仍在（失效不删除，保留时序）
	raw, _ := store.List("w1", "老陈")
	if len(raw) != 1 || !raw[0].Invalid {
		t.Fatal("物理条目应保留且标记失效")
	}
}

func TestJSONMemoryService_Search(t *testing.T) {
	backend, _ := newTestBackend(t)
	ctx := context.Background()
	key := memory.UserKey{AppName: "w1", UserID: "_world"}

	backend.AddMemory(ctx, key, "玩家在老陈的店里偷了钱袋", nil)
	backend.AddMemory(ctx, key, "商队抵达了小镇", nil)
	backend.AddMemory(ctx, key, "玩家帮助老陈修好了屋顶", nil)

	results, err := backend.SearchMemories(ctx, key, "老陈 钱袋")
	if err != nil {
		t.Fatalf("SearchMemories 失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("应检索到相关记忆")
	}
	// 最相关的应排在第一
	if results[0].Memory.Memory != "玩家在老陈的店里偷了钱袋" {
		t.Fatalf("最相关记忆应排第一: %s", results[0].Memory.Memory)
	}
	if results[0].Score <= 0 {
		t.Fatal("相似度分数应大于 0")
	}
}
