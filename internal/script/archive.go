// 剧本存档管理：将识别后的 Script 以 JSON 文件持久化到 ./data/scripts/ 目录。
// 复用 character.Manager 的原子写入模式（tmp + rename），保证数据一致性。
//
// 同时管理跑团进度（Progress）的存档，存储到 ./data/scripts/progress/ 子目录。
package script

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Archive 管理剧本和进度的持久化存储。
type Archive struct {
	mu       sync.RWMutex
	dir      string                // 剧本存储目录（./data/scripts/）
	progressDir string             // 进度存储目录（./data/scripts/progress/）
	scripts  map[string]*Script    // 内存缓存：scriptID -> Script
}

// NewArchive 创建剧本存档管理器并加载已有剧本。
func NewArchive(dir string) (*Archive, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建剧本目录失败: %w", err)
	}
	progressDir := filepath.Join(dir, "progress")
	if err := os.MkdirAll(progressDir, 0755); err != nil {
		return nil, fmt.Errorf("创建进度目录失败: %w", err)
	}

	a := &Archive{
		dir:         dir,
		progressDir: progressDir,
		scripts:     make(map[string]*Script),
	}

	if err := a.loadAllScripts(); err != nil {
		return nil, fmt.Errorf("加载剧本失败: %w", err)
	}

	return a, nil
}

// --- 剧本管理 ---

// Save 保存剧本到磁盘和内存。
func (a *Archive) Save(script *Script) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if script.ID == "" {
		return fmt.Errorf("剧本 ID 不能为空")
	}

	script.FilePath = filepath.Join(a.dir, script.ID+".json")
	data, err := json.MarshalIndent(script, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化剧本失败: %w", err)
	}

	// 原子写入
	tmpPath := script.FilePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入剧本文件失败: %w", err)
	}
	_ = os.Remove(script.FilePath)
	if err := os.Rename(tmpPath, script.FilePath); err != nil {
		return fmt.Errorf("重命名剧本文件失败: %w", err)
	}

	a.scripts[script.ID] = script
	return nil
}

// Get 获取剧本 by ID。
func (a *Archive) Get(scriptID string) (*Script, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	script, ok := a.scripts[scriptID]
	if !ok {
		return nil, fmt.Errorf("剧本 %s 不存在", scriptID)
	}
	return script, nil
}

// GetByName 根据名称获取剧本。
func (a *Archive) GetByName(name string) (*Script, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, s := range a.scripts {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("剧本 %s 不存在", name)
}

// List 列出所有剧本摘要。
func (a *Archive) List() []*Script {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]*Script, 0, len(a.scripts))
	for _, s := range a.scripts {
		result = append(result, s)
	}
	return result
}

// Remove 删除剧本。
func (a *Archive) Remove(scriptID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	script, ok := a.scripts[scriptID]
	if !ok {
		return fmt.Errorf("剧本 %s 不存在", scriptID)
	}

	if script.FilePath != "" {
		_ = os.Remove(script.FilePath)
	}
	delete(a.scripts, scriptID)
	return nil
}

// --- 进度管理 ---

// SaveProgress 保存跑团进度。
func (a *Archive) SaveProgress(progress *Progress) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if progress.SessionID == "" {
		return fmt.Errorf("进度 SessionID 不能为空")
	}

	progress.LastUpdate = time.Now().Format("2006-01-02 15:04:05")
	progress.FilePath = filepath.Join(a.progressDir, progress.SessionID+".json")

	data, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化进度失败: %w", err)
	}

	// 原子写入
	tmpPath := progress.FilePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入进度文件失败: %w", err)
	}
	_ = os.Remove(progress.FilePath)
	if err := os.Rename(tmpPath, progress.FilePath); err != nil {
		return fmt.Errorf("重命名进度文件失败: %w", err)
	}

	return nil
}

// LoadProgress 加载跑团进度。
func (a *Archive) LoadProgress(sessionID string) (*Progress, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	path := filepath.Join(a.progressDir, sessionID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取进度文件失败: %w", err)
	}

	var progress Progress
	if err := json.Unmarshal(data, &progress); err != nil {
		return nil, fmt.Errorf("解析进度 JSON 失败: %w", err)
	}
	progress.FilePath = path
	return &progress, nil
}

// LoadProgressOrDefault 加载进度，不存在则返回默认值。
func (a *Archive) LoadProgressOrDefault(sessionID, scriptID, scriptName string) *Progress {
	progress, err := a.LoadProgress(sessionID)
	if err == nil {
		return progress
	}
	// 返回默认进度
	firstNode := ""
	if script, err := a.Get(scriptID); err == nil && script.GetFirstNode() != nil {
		firstNode = script.GetFirstNode().ID
	}
	return &Progress{
		SessionID:      sessionID,
		ScriptID:       scriptID,
		ScriptName:     scriptName,
		CurrentNodeID:  firstNode,
		CompletedNodes: []string{},
		PlayerDecisions: []Decision{},
		IsActive:       true,
	}
}

// DeleteProgress 删除跑团进度。
func (a *Archive) DeleteProgress(sessionID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	path := filepath.Join(a.progressDir, sessionID+".json")
	_ = os.Remove(path)
	return nil
}

// --- 内部方法 ---

// loadAllScripts 启动时从磁盘加载所有剧本。
func (a *Archive) loadAllScripts() error {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(a.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var script Script
		if err := json.Unmarshal(data, &script); err != nil {
			continue
		}
		script.FilePath = path
		a.scripts[script.ID] = &script
	}

	return nil
}
