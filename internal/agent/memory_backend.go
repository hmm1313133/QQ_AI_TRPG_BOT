// Package agent - jsonMemoryService：框架 memory.Service 接口的本地 JSON 后端。
//
// 将 trpc-agent-go 的 memory.Service 抽象适配到我们的 world.MemoryStore
// （持久化 JSON 文件），使得框架的记忆工具/extractor 与未来的
// mem0/tencentdb 后端可以无缝替换。
//
// 映射关系：
//   - UserKey.AppName = worldID，UserKey.UserID = 记忆实体（NPC 名 / "_world"）
//   - DeleteMemory/ClearMemories 采用 Mem0 语义：标记失效而非物理删除
//   - 扩展字段（Importance/Pinned/WorldTime）经 clockFn 与默认值补齐，
//     本地三因子重排仍直接读取 world.MemoryStore 的完整模型
package agent

import (
	"context"
	"sort"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"

	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// 框架路径写入的记忆默认重要性（规则写入路径有各自的评分）。
const defaultFrameworkImportance = 5

// jsonMemoryService 是 memory.Service 的 JSON 持久化实现。
type jsonMemoryService struct {
	store   *world.MemoryStore
	clockFn func(worldID string) int64 // 世界时间提供器（可为 nil，回退现实时间分钟）
}

// NewJSONMemoryService 创建 JSON 后端。
// clockFn 按 worldID 返回当前世界时间（分钟），用于记忆时间戳。
func NewJSONMemoryService(store *world.MemoryStore, clockFn func(worldID string) int64) memory.Service {
	return &jsonMemoryService{store: store, clockFn: clockFn}
}

// worldTimeOf 获取指定世界的当前世界时间。
func (s *jsonMemoryService) worldTimeOf(worldID string) int64 {
	if s.clockFn != nil {
		return s.clockFn(worldID)
	}
	return time.Now().Unix() / 60
}

// toFrameworkEntry 将本地记忆条目转换为框架 Entry。
func toFrameworkEntry(worldID, entity string, e *world.MemoryEntry) *memory.Entry {
	now := time.Now()
	return &memory.Entry{
		ID:      e.ID,
		AppName: worldID,
		UserID:  entity,
		Memory: &memory.Memory{
			Memory:      e.Content,
			Topics:      e.Tags,
			LastUpdated: &now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// AddMemory implements memory.Service（幂等追加）。
func (s *jsonMemoryService) AddMemory(ctx context.Context, userKey memory.UserKey, mem string, topics []string, opts ...memory.AddOption) error {
	now := s.worldTimeOf(userKey.AppName)
	return s.store.Append(userKey.AppName, userKey.UserID, world.MemoryEntry{
		ID:         newMemoryID(),
		Content:    mem,
		Importance: defaultFrameworkImportance,
		Tags:       topics,
		WorldTime:  now,
		LastAccess: now,
	})
}

// UpdateMemory implements memory.Service。
func (s *jsonMemoryService) UpdateMemory(ctx context.Context, key memory.Key, mem string, topics []string, opts ...memory.UpdateOption) error {
	entries, err := s.store.List(key.AppName, key.UserID)
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].ID == key.MemoryID {
			entries[i].Content = mem
			if len(topics) > 0 {
				entries[i].Tags = topics
			}
			return s.store.SaveAll(key.AppName, key.UserID, entries)
		}
	}
	// 不存在则按 ADD 语义追加（与 Mem0 行为一致）
	return s.AddMemory(ctx, memory.UserKey{AppName: key.AppName, UserID: key.UserID}, mem, topics)
}

// DeleteMemory implements memory.Service（失效不删除，保留时序）。
func (s *jsonMemoryService) DeleteMemory(ctx context.Context, key memory.Key) error {
	entries, err := s.store.List(key.AppName, key.UserID)
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].ID == key.MemoryID {
			entries[i].Invalid = true
			return s.store.SaveAll(key.AppName, key.UserID, entries)
		}
	}
	return nil
}

// ClearMemories implements memory.Service（全部标记失效）。
func (s *jsonMemoryService) ClearMemories(ctx context.Context, userKey memory.UserKey) error {
	entries, err := s.store.List(userKey.AppName, userKey.UserID)
	if err != nil {
		return err
	}
	for i := range entries {
		entries[i].Invalid = true
	}
	return s.store.SaveAll(userKey.AppName, userKey.UserID, entries)
}

// ReadMemories implements memory.Service（按时间倒序，仅有效条目）。
func (s *jsonMemoryService) ReadMemories(ctx context.Context, userKey memory.UserKey, limit int) ([]*memory.Entry, error) {
	entries, err := s.store.List(userKey.AppName, userKey.UserID)
	if err != nil {
		return nil, err
	}
	var result []*memory.Entry
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Invalid {
			continue
		}
		result = append(result, toFrameworkEntry(userKey.AppName, userKey.UserID, &entries[i]))
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

// SearchMemories implements memory.Service（本地相关度打分 + top-N）。
// 完整的三因子重排在 MemoryService 侧进行，这里提供框架语义的基础检索。
func (s *jsonMemoryService) SearchMemories(ctx context.Context, userKey memory.UserKey, query string, opts ...memory.SearchOption) ([]*memory.Entry, error) {
	options := memory.ResolveSearchOptions(query, opts)

	entries, err := s.store.List(userKey.AppName, userKey.UserID)
	if err != nil {
		return nil, err
	}

	type scored struct {
		entry *memory.Entry
		score float64
	}
	var hits []scored
	for i := range entries {
		if entries[i].Invalid {
			continue
		}
		score := world.RetrieveScore(options.Query, entries[i].Content)
		if options.SimilarityThreshold > 0 && score < options.SimilarityThreshold {
			continue
		}
		fe := toFrameworkEntry(userKey.AppName, userKey.UserID, &entries[i])
		fe.Score = score
		hits = append(hits, scored{entry: fe, score: score})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })

	maxResults := options.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	if len(hits) > maxResults {
		hits = hits[:maxResults]
	}
	result := make([]*memory.Entry, 0, len(hits))
	for _, h := range hits {
		result = append(result, h.entry)
	}
	return result, nil
}

// Tools implements memory.Service。
// 记忆工具暂不开放给 Narrator（UserKey 作用域与我们的 NPC 记忆模型不同），
// 后续可按 worldID/entity 映射定制（见待决策事项）。
func (s *jsonMemoryService) Tools() []tool.Tool {
	return nil
}

// EnqueueAutoMemoryJob implements memory.Service。
// 自动抽取由 TurnEngine.AfterTurn 驱动，此处为 no-op。
func (s *jsonMemoryService) EnqueueAutoMemoryJob(ctx context.Context, sess *session.Session) error {
	return nil
}

// Close implements memory.Service。
func (s *jsonMemoryService) Close() error {
	return nil
}

// newMemoryID 生成记忆 ID。
func newMemoryID() string {
	return "mem_" + time.Now().Format("20060102150405") + "_" + randSuffix()
}

// randSuffix 生成短随机后缀（避免同毫秒冲突）。
func randSuffix() string {
	return strings36(time.Now().UnixNano() % 46656) // 36^3
}

func strings36(v int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if v < 0 {
		v = -v
	}
	out := make([]byte, 3)
	for i := 2; i >= 0; i-- {
		out[i] = digits[v%36]
		v /= 36
	}
	return string(out)
}
