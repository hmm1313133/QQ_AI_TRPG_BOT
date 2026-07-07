// Package character 提供角色卡的创建、存储和管理功能。
package character

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Manager 管理所有角色卡。
type Manager struct {
	mu      sync.RWMutex
	dir     string                // 存储目录
	cards   map[string]*Card      // cardID -> 角色卡
}

// NewManager 创建角色卡管理器。
func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建角色卡目录失败: %w", err)
	}
	return &Manager{
		dir:   dir,
		cards: make(map[string]*Card),
	}, nil
}

// Card 角色卡数据结构。
type Card struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Player   string            `json:"player"`    // QQ userID
	System   string            `json:"system"`    // 规则系统: coc7 / dnd5e
	Attrs    map[string]int    `json:"attrs"`     // 基础属性
	Skills   map[string]int    `json:"skills"`    // 技能值
	Status   map[string]int    `json:"status"`    // 状态值 (HP, SAN, MP 等)
	Backstory string           `json:"backstory"` // 背景故事
	FilePath string            `json:"-"`         // 存储路径
}

// Create 创建一张新角色卡。
func (m *Manager) Create(card *Card) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if card.ID == "" {
		return fmt.Errorf("角色卡 ID 不能为空")
	}
	if _, exists := m.cards[card.ID]; exists {
		return fmt.Errorf("角色卡 %s 已存在", card.ID)
	}

	card.FilePath = filepath.Join(m.dir, card.ID+".json")
	m.cards[card.ID] = card
	return m.save(card)
}

// Get 获取角色卡。
func (m *Manager) Get(cardID string) (*Card, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	card, ok := m.cards[cardID]
	if !ok {
		return nil, fmt.Errorf("角色卡 %s 不存在", cardID)
	}
	return card, nil
}

// GetByPlayer 获取某玩家的角色卡。
func (m *Manager) GetByPlayer(playerID string) ([]*Card, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Card
	for _, c := range m.cards {
		if c.Player == playerID {
			result = append(result, c)
		}
	}
	return result, nil
}

// Update 更新角色卡。
func (m *Manager) Update(card *Card) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.cards[card.ID]; !exists {
		return fmt.Errorf("角色卡 %s 不存在", card.ID)
	}
	return m.save(card)
}

// Delete 删除角色卡。
func (m *Manager) Delete(cardID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	card, ok := m.cards[cardID]
	if !ok {
		return fmt.Errorf("角色卡 %s 不存在", cardID)
	}
	delete(m.cards, cardID)
	return os.Remove(card.FilePath)
}

// save 将角色卡保存到文件。
func (m *Manager) save(card *Card) error {
	// TODO: 使用 JSON 序列化保存到 card.FilePath
	return nil
}
