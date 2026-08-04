// Package character provides character card creation, storage, and management.
// Persistence is pluggable: JSON files (legacy) or SQLite (character_cards 表，
// 见《世界编辑器与素材联动设计.md》§9）。The Manager is thread-safe.
package character

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// cardBackend 持久化后端（文件 / SQLite）。
type cardBackend interface {
	loadAll() ([]*Card, error)
	save(c *Card) error
	remove(c *Card) error
}

// Manager manages all character cards.
// Cards are stored both in memory (map) and via the configured backend.
type Manager struct {
	mu      sync.RWMutex
	backend cardBackend
	cards   map[string]*Card // cardID -> character card
}

// NewManager creates a character card manager with JSON file storage (legacy).
func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建角色卡目录失败: %w", err)
	}
	return newManagerWithBackend(&fileBackend{dir: dir})
}

func newManagerWithBackend(b cardBackend) (*Manager, error) {
	m := &Manager{
		backend: b,
		cards:   make(map[string]*Card),
	}
	if err := m.loadAll(); err != nil {
		return nil, fmt.Errorf("加载角色卡失败: %w", err)
	}
	return m, nil
}

// Card represents a character card.
type Card struct {
	ID        string            `json:"id"`         // format: playerID:charName
	Name      string            `json:"name"`       // character name
	Player    string            `json:"player"`     // QQ userID (openid)
	System    string            `json:"system"`     // ruleset: "coc7" / "dnd5e"
	Attrs     map[string]int    `json:"attrs"`      // base attributes (力量/敏捷/智力 etc.)
	Skills    map[string]int    `json:"skills"`     // skill values
	Status    map[string]int    `json:"status"`     // status values (HP, SAN, MP, etc.)
	Backstory string            `json:"backstory"`  // background story
	// 创作字段（设计 §3.1）：roleplay 模式与导入世界时映射到 CharacterState。
	Appearance  string `json:"appearance,omitempty"`  // 外貌描写
	Personality string `json:"personality,omitempty"` // 性格描述
	FilePath  string            `json:"-"`          // file storage path (not serialized; 仅文件后端使用)
}

// MakeID generates a card ID from playerID and character name.
func MakeID(playerID, name string) string {
	return playerID + ":" + name
}

// Create creates a new character card.
// If card.ID is empty, it will be generated from Player and Name.
func (m *Manager) Create(card *Card) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if card.ID == "" {
		if card.Player == "" || card.Name == "" {
			return fmt.Errorf("角色卡需要 Player 和 Name 字段")
		}
		card.ID = MakeID(card.Player, card.Name)
	}
	if _, exists := m.cards[card.ID]; exists {
		return fmt.Errorf("角色卡 %s 已存在", card.Name)
	}

	m.cards[card.ID] = card
	return m.backend.save(card)
}

// Get retrieves a card by ID.
func (m *Manager) Get(cardID string) (*Card, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	card, ok := m.cards[cardID]
	if !ok {
		return nil, fmt.Errorf("角色卡 %s 不存在", cardID)
	}
	return card, nil
}

// GetByPlayerAndName retrieves a card by player ID and character name.
func (m *Manager) GetByPlayerAndName(playerID, name string) (*Card, error) {
	return m.Get(MakeID(playerID, name))
}

// GetByPlayer returns all cards belonging to a player.
func (m *Manager) GetByPlayer(playerID string) []*Card {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Card
	for _, c := range m.cards {
		if c.Player == playerID {
			result = append(result, c)
		}
	}
	return result
}

// ListByPlayer returns the names of all cards belonging to a player.
func (m *Manager) ListByPlayer(playerID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var names []string
	for _, c := range m.cards {
		if c.Player == playerID {
			names = append(names, c.Name)
		}
	}
	return names
}

// Update saves changes to an existing card.
func (m *Manager) Update(card *Card) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.cards[card.ID]; !exists {
		return fmt.Errorf("角色卡 %s 不存在", card.Name)
	}
	return m.backend.save(card)
}

// Delete removes a card by ID.
func (m *Manager) Delete(cardID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	card, ok := m.cards[cardID]
	if !ok {
		return fmt.Errorf("角色卡 %s 不存在", cardID)
	}
	delete(m.cards, cardID)
	return m.backend.remove(card)
}

// DeleteByPlayerAndName removes a card by player ID and character name.
func (m *Manager) DeleteByPlayerAndName(playerID, name string) error {
	return m.Delete(MakeID(playerID, name))
}

// SetAttr sets or updates an attribute on a card and persists it.
func (m *Manager) SetAttr(cardID, attr string, value int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	card, ok := m.cards[cardID]
	if !ok {
		return fmt.Errorf("角色卡 %s 不存在", cardID)
	}
	if card.Attrs == nil {
		card.Attrs = make(map[string]int)
	}
	card.Attrs[attr] = value
	return m.backend.save(card)
}

// SetSkill sets or updates a skill on a card and persists it.
func (m *Manager) SetSkill(cardID, skill string, value int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	card, ok := m.cards[cardID]
	if !ok {
		return fmt.Errorf("角色卡 %s 不存在", cardID)
	}
	if card.Skills == nil {
		card.Skills = make(map[string]int)
	}
	card.Skills[skill] = value
	return m.backend.save(card)
}

// SetStatus sets or updates a status value (HP/SAN/MP) on a card and persists it.
func (m *Manager) SetStatus(cardID, key string, value int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	card, ok := m.cards[cardID]
	if !ok {
		return fmt.Errorf("角色卡 %s 不存在", cardID)
	}
	if card.Status == nil {
		card.Status = make(map[string]int)
	}
	card.Status[key] = value
	return m.backend.save(card)
}

// --- internal: 文件后端（legacy） ---

// fileBackend JSON 文件存储后端。
type fileBackend struct {
	dir string
}

// cardFileName 由角色卡 ID 派生文件名：替换 Windows 文件名非法字符（如 ID 中的冒号）。
// 卡片内容里的 ID 保持不变，loadAll 从 JSON 恢复 ID，因此不影响按 ID 查找。
func cardFileName(cardID string) string {
	r := strings.NewReplacer(
		":", "_", "<", "_", ">", "_", "\"", "_",
		"/", "_", "\\", "_", "|", "_", "?", "_", "*", "_",
	)
	return r.Replace(cardID) + ".json"
}

// save serializes the card to JSON and writes it atomically.
func (b *fileBackend) save(card *Card) error {
	if card.FilePath == "" {
		card.FilePath = filepath.Join(b.dir, cardFileName(card.ID))
	}
	data, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化角色卡失败: %w", err)
	}
	// Atomic write: write to temp file, then rename
	tmpPath := card.FilePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入角色卡文件失败: %w", err)
	}
	// On Windows, need to remove target first if it exists
	_ = os.Remove(card.FilePath)
	if err := os.Rename(tmpPath, card.FilePath); err != nil {
		return fmt.Errorf("重命名角色卡文件失败: %w", err)
	}
	return nil
}

// remove 删除卡片文件（best-effort）。
func (b *fileBackend) remove(card *Card) error {
	if card.FilePath != "" {
		_ = os.Remove(card.FilePath)
	}
	return nil
}

// loadAll loads all character cards from the storage directory.
func (b *fileBackend) loadAll() ([]*Card, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return nil, nil // directory might not have any files yet
	}
	var out []*Card
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(b.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip unreadable files
		}
		var card Card
		if err := json.Unmarshal(data, &card); err != nil {
			continue // skip invalid JSON
		}
		card.FilePath = path
		out = append(out, &card)
	}
	return out, nil
}

// loadAll 从后端加载全部卡片到内存。
func (m *Manager) loadAll() error {
	cards, err := m.backend.loadAll()
	if err != nil {
		return err
	}
	for _, c := range cards {
		m.cards[c.ID] = c
	}
	return nil
}

// ListAll 返回所有角色卡（管理后台用）。
func (m *Manager) ListAll() []*Card {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Card, 0, len(m.cards))
	for _, c := range m.cards {
		result = append(result, c)
	}
	return result
}
