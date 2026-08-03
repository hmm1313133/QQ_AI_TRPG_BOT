// 记忆层：长期记忆条目、文件存储与三因子检索。
//
// 设计文档第五章 + Generative Agents (arXiv:2304.03442)：
//   - 检索打分 = w_recency·0.995^Δh + w_importance·I/10 + w_relevance·cos
//   - recency 按"上次被检索"时间计算（被反复用到的记忆保鲜）
//   - 时间一律使用世界时间（暂停的团记忆不过期）
//   - 写入支持 Mem0 算子：ADD / UPDATE / DELETE / NOOP（失效不删除）
package world

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// 检索权重（设计文档 5.2：长团中重要性高于时效）。
const (
	WeightRecency    = 0.3
	WeightImportance = 0.4
	WeightRelevance  = 0.3
)

// recencyDecay 每小时衰减系数（GA 论文原值 0.995）。
const recencyDecay = 0.995

// MemoryEntry 长期记忆条目。
type MemoryEntry struct {
	ID         string   `json:"id"`
	Content    string   `json:"content"`
	Importance int      `json:"importance"` // 1-10
	WorldTime  int64    `json:"world_time"`
	LastAccess int64    `json:"last_access"`
	Tags       []string `json:"tags,omitempty"`
	Pinned     bool     `json:"pinned,omitempty"`  // 里程碑事件，永不压缩遗忘
	Invalid    bool     `json:"invalid,omitempty"` // UPDATE/DELETE 失效标记（保留时序，不物理删除）
	Evidence   []string `json:"evidence,omitempty"` // 反思洞察的证据记忆 ID（反思树回溯）
}

// MemoryStore 记忆文件存储（每世界每实体一个 JSON 文件）。
type MemoryStore struct {
	mu  sync.RWMutex
	dir string // 如 ./data/memories
}

// NewMemoryStore 创建记忆存储。
func NewMemoryStore(dir string) (*MemoryStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建记忆目录失败: %w", err)
	}
	return &MemoryStore{dir: dir}, nil
}

func (s *MemoryStore) path(worldID, entity string) string {
	return filepath.Join(s.dir, sanitizeFileName(worldID), sanitizeFileName(entity)+".json")
}

// List 读取实体的全部记忆。
func (s *MemoryStore) List(worldID, entity string) ([]MemoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path(worldID, entity))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []MemoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// SaveAll 全量写回实体记忆（用于访问时间更新与失效标记）。
func (s *MemoryStore) SaveAll(worldID, entity string, entries []MemoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.path(worldID, entity)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

// Append 追加一条记忆（ADD 算子）。
func (s *MemoryStore) Append(worldID, entity string, entry MemoryEntry) error {
	entries, err := s.List(worldID, entity)
	if err != nil {
		return err
	}
	entries = append(entries, entry)
	return s.SaveAll(worldID, entity, entries)
}

// Count 实体有效记忆条数（不含失效）。
func (s *MemoryStore) Count(worldID, entity string) int {
	entries, _ := s.List(worldID, entity)
	count := 0
	for _, e := range entries {
		if !e.Invalid {
			count++
		}
	}
	return count
}

// ============================================================
// 三因子检索
// ============================================================

// RelevanceFunc 语义相关度函数（OpenViking 启用时注入，返回 query 与各 content 的相似度）。
// 返回 map[记忆内容]相似度(0-1)；nil 时使用本地 bigram 重叠度。
type RelevanceFunc func(query string, contents []string) map[string]float64

// Retrieve 三因子检索 top-K 有效记忆。
// 返回的条目 LastAccess 已更新（调用方负责 SaveAll 持久化）。
func Retrieve(entries []MemoryEntry, query string, now int64, topK int, relFn RelevanceFunc) []MemoryEntry {
	if topK <= 0 {
		topK = 5
	}

	// 收集有效条目
	type scored struct {
		idx   int
		score float64
	}
	var active []scored

	// 预计算 relevance
	contents := make([]string, len(entries))
	for i := range entries {
		contents[i] = entries[i].Content
	}
	var relScores map[string]float64
	if relFn != nil {
		relScores = relFn(query, contents)
	}

	for i := range entries {
		e := &entries[i]
		if e.Invalid {
			continue
		}

		// recency: 0.995^(距上次访问的世界小时数)
		hours := float64(now-e.LastAccess) / 60.0
		if hours < 0 {
			hours = 0
		}
		recency := math.Pow(recencyDecay, hours)

		// importance: 归一化
		importance := float64(e.Importance) / 10.0

		// relevance: 语义（如有）或本地 bigram
		var relevance float64
		if relScores != nil {
			relevance = relScores[e.Content]
		} else {
			relevance = bigramOverlap(query, e.Content)
		}

		score := WeightRecency*recency + WeightImportance*importance + WeightRelevance*relevance
		active = append(active, scored{idx: i, score: score})
	}

	// 按分数降序取 topK
	sort.Slice(active, func(i, j int) bool { return active[i].score > active[j].score })
	if len(active) > topK {
		active = active[:topK]
	}

	result := make([]MemoryEntry, 0, len(active))
	for _, a := range active {
		entries[a.idx].LastAccess = now // 保鲜
		result = append(result, entries[a.idx])
	}
	return result
}

// RetrieveScore 本地相关度打分（0-1），供框架适配器等外部调用。
func RetrieveScore(query, content string) float64 {
	return bigramOverlap(query, content)
}

// bigramOverlap 查询与内容的字符 bigram 重叠度（0-1），中文友好的本地相关度。
func bigramOverlap(query, content string) float64 {
	qr := []rune(query)
	if len(qr) < 2 {
		if strings.Contains(content, query) && query != "" {
			return 1
		}
		return 0
	}
	hits := 0
	for i := 0; i+2 <= len(qr); i += 2 {
		if strings.Contains(content, string(qr[i:i+2])) {
			hits++
		}
	}
	total := len(qr) / 2
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}
