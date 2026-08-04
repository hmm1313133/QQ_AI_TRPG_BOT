// 世界状态持久化：StateRepository 接口 + JSON 文件实现。
//
// 设计文档 4.4：接口先行，当前 JSON 单文档，预留 SQLite。
// 写入语义：tick 内决策、tick 边界落账（ApplyEvent 修改内存对象后一次原子写）。
package world

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// StateRepository 世界状态存储接口。
type StateRepository interface {
	Load(worldID string) (*WorldState, error)
	Save(state *WorldState) error
	Delete(worldID string) error
	List() ([]string, error)
	// Archive 将 beforeRound 之前的事件日志压缩归档（P5 实现，当前保留接口）。
	Archive(worldID string, beforeRound int) error
}

// JSONRepository 每世界一个 JSON 文件的存储实现。
type JSONRepository struct {
	mu  sync.RWMutex
	dir string
}

// NewJSONRepository 创建 JSON 存储（dir 如 ./data/worlds）。
func NewJSONRepository(dir string) (*JSONRepository, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建 worlds 目录失败: %w", err)
	}
	return &JSONRepository{dir: dir}, nil
}

func (r *JSONRepository) path(worldID string) string {
	return filepath.Join(r.dir, sanitizeFileName(worldID)+".json")
}

// sanitizeFileName 防止 worldID 中的路径分隔符造成路径穿越。
func sanitizeFileName(id string) string {
	var out []rune
	for _, c := range id {
		switch {
		case c == '/' || c == '\\' || c == ':' || c == '.':
			out = append(out, '_')
		default:
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "world"
	}
	return string(out)
}

// Load 加载世界状态。
func (r *JSONRepository) Load(worldID string) (*WorldState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, err := os.ReadFile(r.path(worldID))
	if err != nil {
		return nil, fmt.Errorf("读取 WorldState 失败: %w", err)
	}
	var state WorldState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("解析 WorldState JSON 失败: %w", err)
	}
	state.ensureMaps()
	return &state, nil
}

// Save 原子写入（tmp + rename，失败时保留 .bak 兜底）。
func (r *JSONRepository) Save(state *WorldState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if state.WorldID == "" {
		return fmt.Errorf("WorldState WorldID 不能为空")
	}

	state.LastUpdate = time.Now().Format("2006-01-02 15:04:05")
	path := r.path(state.WorldID)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 WorldState 失败: %w", err)
	}

	// 先备份旧文件，再原子替换，避免 remove+rename 窗口丢数据
	if _, err := os.Stat(path); err == nil {
		_ = os.Rename(path, path+".bak")
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入 WorldState 文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Windows 上目标已存在时 rename 失败，回退为 remove+rename
		_ = os.Remove(path)
		if err2 := os.Rename(tmpPath, path); err2 != nil {
			return fmt.Errorf("重命名 WorldState 文件失败: %w", err2)
		}
	}
	return nil
}

// Delete 删除世界状态。
func (r *JSONRepository) Delete(worldID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = os.Remove(r.path(worldID))
	_ = os.Remove(r.path(worldID) + ".bak")
	return nil
}

// List 列出所有世界 ID（管理后台用）。
func (r *JSONRepository) List() ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") ||
			strings.HasSuffix(entry.Name(), ".bak") || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(entry.Name(), ".json"))
	}
	return ids, nil
}

// Archive 归档旧事件日志：将 beforeRound 之前的事件压缩为一条摘要事件。
// 完整实现（LLM 摘要）在 P5；当前为规则化截断。
func (r *JSONRepository) Archive(worldID string, beforeRound int) error {
	state, err := r.Load(worldID)
	if err != nil {
		return err
	}
	if !archiveEvents(state, beforeRound) {
		return nil
	}
	if err := r.Save(state); err != nil {
		return err
	}
	log.Printf("[World] 归档事件日志: world=%s, archived=%d", worldID, beforeRound)
	return nil
}

// archiveEvents 把 beforeRound 之前的事件压缩为一条计数摘要事件；有归档动作返回 true。
// JSON 与 SQLite 两种存储共用（Archive 方法仅差最后的持久化调用）。
func archiveEvents(state *WorldState, beforeRound int) bool {
	kept := state.EventLog[:0]
	archived := 0
	for _, ev := range state.EventLog {
		if ev.Round < beforeRound {
			archived++
			continue
		}
		kept = append(kept, ev)
	}
	if archived == 0 {
		return false
	}
	state.EventLog = append([]WorldEvent{{
		Type:   "note",
		Actor:  "engine",
		Target: "archive",
		Value:  fmt.Sprintf("已归档 %d 条早期事件（round < %d）", archived, beforeRound),
		Round:  beforeRound,
	}}, kept...)
	return true
}
