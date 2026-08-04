// Package persona 玩家人设（persona）系统。
//
// 玩家自我设定（名字 + 自由描述），注入 Narrator 上下文让 KP 知晓"你是谁"。
// 两级粒度：全局默认（本包 SQLite 存储，按 UserID 隔离）+
// 每世界覆盖（存 WorldState.Personas，随世界文档持久化）。
// 生效优先级：本世界覆盖 > 全局默认。
package persona

import (
	"database/sql"
	"fmt"
	"time"
)

// Profile 一份玩家人设。
type Profile struct {
	Name        string `json:"name,omitempty"`        // 玩家角色名（可空，空则注入时省略名字段）
	Description string `json:"description,omitempty"` // 自由描述（性格/外貌/习惯等）
}

// Empty 判断人设是否全空。
func (p *Profile) Empty() bool {
	return p == nil || (p.Name == "" && p.Description == "")
}

// Store 全局默认人设的 SQLite 存储（表 personas）。
// 与配置库/世界库共用主流程打开的 app.db 连接。
type Store struct {
	db *sql.DB
}

// NewStore 创建全局人设存储（建表 personas）。
func NewStore(db *sql.DB) (*Store, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS personas (
			user_id TEXT PRIMARY KEY,
			name TEXT,
			description TEXT,
			updated_at TEXT
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, fmt.Errorf("建人设表失败: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// Get 读取用户的全局默认人设；无记录返回 (nil, nil)。
func (s *Store) Get(userID string) (*Profile, error) {
	var p Profile
	var name, desc sql.NullString
	err := s.db.QueryRow(`SELECT name, description FROM personas WHERE user_id = ?`, userID).
		Scan(&name, &desc)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Name, p.Description = name.String, desc.String
	return &p, nil
}

// Set 写入/覆盖用户的全局默认人设。
func (s *Store) Set(userID string, p Profile) error {
	_, err := s.db.Exec(`INSERT INTO personas (user_id, name, description, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET name = excluded.name, description = excluded.description, updated_at = excluded.updated_at`,
		userID, p.Name, p.Description, time.Now().Format("2006-01-02 15:04:05"))
	return err
}

// Delete 删除用户的全局默认人设（无记录也算成功）。
func (s *Store) Delete(userID string) error {
	_, err := s.db.Exec(`DELETE FROM personas WHERE user_id = ?`, userID)
	return err
}
