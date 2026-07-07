// Package module 提供 TRPG 剧本/模组的加载和管理功能。
package module

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Manager 管理可用的 TRPG 模组。
type Manager struct {
	mu      sync.RWMutex
	dir     string
	modules map[string]*Module
}

// NewManager 创建模组管理器。
func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建模组目录失败: %w", err)
	}
	m := &Manager{
		dir:     dir,
		modules: make(map[string]*Module),
	}
	// TODO: 扫描目录加载所有模组
	return m, nil
}

// Module 表示一个 TRPG 模组/剧本。
type Module struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	System      string            `json:"system"`       // 适用规则: coc7 / dnd5e
	MinPlayers  int               `json:"min_players"`
	MaxPlayers  int               `json:"max_players"`
	Scenes      []Scene           `json:"scenes"`       // 场景列表
	NPCs        map[string]*NPC   `json:"npcs"`         // NPC 定义
	Items       map[string]*Item  `json:"items"`        // 道具定义
	FilePath    string            `json:"-"`
}

// Scene 模组中的一个场景。
type Scene struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OnEnter     string `json:"on_enter"`    // 进入场景时的触发脚本
	Exits       []Exit `json:"exits"`       // 可前往的场景
}

// Exit 场景出口。
type Exit struct {
	To        string `json:"to"`          // 目标场景 ID
	Condition string `json:"condition"`   // 触发条件描述
}

// NPC 非玩家角色定义。
type NPC struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Attrs    map[string]int `json:"attrs"`
	Personality string      `json:"personality"`  // 性格描述，供 AI 扮演参考
}

// Item 道具定义。
type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Usable      bool   `json:"usable"`
}

// Get 获取模组。
func (m *Manager) Get(name string) (*Module, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mod, ok := m.modules[name]
	if !ok {
		return nil, fmt.Errorf("模组 %s 不存在", name)
	}
	return mod, nil
}

// List 列出所有可用模组。
func (m *Manager) List() []*Module {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Module, 0, len(m.modules))
	for _, mod := range m.modules {
		result = append(result, mod)
	}
	return result
}

// Load 从文件加载模组。
func (m *Manager) Load(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// TODO: 解析模组文件 (JSON/YAML)
	_ = filepath.Base(path)
	return nil
}
