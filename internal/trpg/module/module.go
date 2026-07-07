// Package module 提供 TRPG 剧本/模组的加载和管理功能。
//
// Module 是运行时使用的模组数据结构，可由 AI 识别的 script.Script 转换而来，
// 也可手动编写 JSON 文件加载。Manager 负责从磁盘加载和管理所有模组。
package module

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Manager 管理可用的 TRPG 模组。
type Manager struct {
	mu      sync.RWMutex
	dir     string
	modules map[string]*Module
}

// NewManager 创建模组管理器并扫描目录加载所有已有模组。
func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建模组目录失败: %w", err)
	}
	m := &Manager{
		dir:     dir,
		modules: make(map[string]*Module),
	}
	if err := m.loadAll(); err != nil {
		return nil, fmt.Errorf("加载模组失败: %w", err)
	}
	return m, nil
}

// Module 表示一个 TRPG 模组/剧本。
// 扩展自原始结构，增加 Background 和 Timeline 字段以支持 AI 剧本识别。
type Module struct {
	ID          string         `json:"id"`           // 唯一标识
	Name        string         `json:"name"`         // 简短名称（用于指令引用）
	Title       string         `json:"title"`        // 完整标题
	Description string         `json:"description"`  // 简要描述
	System      string         `json:"system"`       // 适用规则: coc7 / dnd5e
	MinPlayers  int            `json:"min_players"`  // 最少玩家数
	MaxPlayers  int            `json:"max_players"`  // 最多玩家数
	Background  *Background    `json:"background"`   // 故事背景与世界观
	Timeline    []TimelineNode `json:"timeline"`     // 剧情时间轴
	Scenes      []Scene        `json:"scenes"`       // 场景列表
	NPCs        map[string]*NPC `json:"npcs"`        // NPC 定义
	Items       map[string]*Item `json:"items"`      // 道具定义
	FilePath    string         `json:"-"`
	CreatedAt   string         `json:"created_at"`   // 创建时间
	SourceFile  string         `json:"source_file"`  // 原始文件名
}

// Background 故事背景与世界观设定。
type Background struct {
	Setting           string   `json:"setting"`             // 时代/地点/世界观概述
	Era               string   `json:"era"`                 // 具体时代
	Location          string   `json:"location"`            // 主要地点
	Atmosphere        string   `json:"atmosphere"`          // 氛围描述
	MainTheme         string   `json:"main_theme"`          // 主题
	Synopsis          string   `json:"synopsis"`            // 故事梗概
	KeyOrganizations  []string `json:"key_organizations"`   // 关键组织/势力
}

// TimelineNode 剧情时间轴节点。
type TimelineNode struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Type         string   `json:"type"`           // "act" / "scene" / "event"
	Order        int      `json:"order"`
	Triggers     []string `json:"triggers"`       // 触发条件
	Consequences []string `json:"consequences"`   // 可能后果
	IsKeyNode    bool     `json:"is_key_node"`    // 关键节点
	NPCs         []string `json:"npcs,omitempty"` // 涉及NPC
}

// Scene 模组中的一个场景。
type Scene struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	OnEnter     string   `json:"on_enter"`    // 进入场景时的触发脚本
	Exits       []Exit   `json:"exits"`       // 可前往的场景
	Atmosphere  string   `json:"atmosphere"`  // 场景氛围
}

// Exit 场景出口。
type Exit struct {
	To        string `json:"to"`          // 目标场景 ID
	Condition string `json:"condition"`   // 触发条件描述
}

// NPC 非玩家角色定义。
type NPC struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Attrs       map[string]int `json:"attrs"`
	Skills      map[string]int `json:"skills"`       // 关键技能
	Personality string         `json:"personality"`  // 性格描述，供 AI 扮演参考
	Background  string         `json:"background"`   // 背景故事
	Role        string         `json:"role"`         // protagonist/antagonist/npc
	Notes       string         `json:"notes"`        // 备注
}

// Item 道具定义。
type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Usable      bool   `json:"usable"`
}

// --- 时间轴辅助方法 ---

// GetNodeByID 根据ID查找时间轴节点。
func (mod *Module) GetNodeByID(nodeID string) (*TimelineNode, error) {
	for i := range mod.Timeline {
		if mod.Timeline[i].ID == nodeID {
			return &mod.Timeline[i], nil
		}
	}
	return nil, fmt.Errorf("节点 %s 不存在", nodeID)
}

// GetNextNode 返回当前节点的下一个时间轴节点。
func (mod *Module) GetNextNode(currentNodeID string) (*TimelineNode, error) {
	current, err := mod.GetNodeByID(currentNodeID)
	if err != nil {
		return nil, err
	}
	for i := range mod.Timeline {
		if mod.Timeline[i].Order > current.Order {
			return &mod.Timeline[i], nil
		}
	}
	return nil, nil
}

// GetFirstNode 返回时间轴的第一个节点。
func (mod *Module) GetFirstNode() *TimelineNode {
	if len(mod.Timeline) == 0 {
		return nil
	}
	return &mod.Timeline[0]
}

// IsLastNode 判断是否为最后一个时间轴节点。
func (mod *Module) IsLastNode(nodeID string) bool {
	if len(mod.Timeline) == 0 {
		return true
	}
	return mod.Timeline[len(mod.Timeline)-1].ID == nodeID
}

// --- Manager 方法 ---

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

// Load 从 JSON 文件加载模组。
func (m *Manager) Load(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取模组文件失败: %w", err)
	}

	var mod Module
	if err := json.Unmarshal(data, &mod); err != nil {
		return fmt.Errorf("解析模组 JSON 失败: %w", err)
	}

	mod.FilePath = path
	if mod.Name == "" {
		// 使用文件名（不含扩展名）作为名称
		base := filepath.Base(path)
		mod.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}

	m.modules[mod.Name] = &mod
	return nil
}

// Save 将模组保存为 JSON 文件。
func (m *Manager) Save(mod *Module) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if mod.Name == "" {
		return fmt.Errorf("模组名称不能为空")
	}

	mod.FilePath = filepath.Join(m.dir, mod.Name+".json")
	data, err := json.MarshalIndent(mod, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化模组失败: %w", err)
	}

	// 原子写入：先写临时文件，再重命名
	tmpPath := mod.FilePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入模组文件失败: %w", err)
	}
	_ = os.Remove(mod.FilePath) // Windows 需要先删除目标
	if err := os.Rename(tmpPath, mod.FilePath); err != nil {
		return fmt.Errorf("重命名模组文件失败: %w", err)
	}

	m.modules[mod.Name] = mod
	return nil
}

// Remove 删除模组。
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mod, ok := m.modules[name]
	if !ok {
		return fmt.Errorf("模组 %s 不存在", name)
	}

	if mod.FilePath != "" {
		_ = os.Remove(mod.FilePath)
	}
	delete(m.modules, name)
	return nil
}

// loadAll 扫描目录加载所有 JSON 模组文件。
func (m *Manager) loadAll() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil // 目录可能还没有文件
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(m.dir, entry.Name())
		if err := m.Load(path); err != nil {
			continue // 跳过无效文件
		}
	}
	return nil
}
