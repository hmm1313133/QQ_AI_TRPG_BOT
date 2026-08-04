// SQLite 人物卡存储后端 + 旧 JSON 迁移（《世界编辑器与素材联动设计.md》§9）。
// 混合存储：name/player/system 列供列表查询，card_json 存完整卡片文档。
package character

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NewSQLiteManager 创建 SQLite 后端的人物卡管理器（建表 character_cards）。
func NewSQLiteManager(db *sql.DB) (*Manager, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS character_cards (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		player TEXT,
		system TEXT,
		card_json TEXT NOT NULL,
		updated_at TEXT
	)`); err != nil {
		return nil, fmt.Errorf("建人物卡表失败: %w", err)
	}
	return newManagerWithBackend(&sqliteBackend{db: db})
}

// sqliteBackend SQLite 存储后端。
type sqliteBackend struct {
	db *sql.DB
}

func (b *sqliteBackend) loadAll() ([]*Card, error) {
	rows, err := b.db.Query(`SELECT card_json FROM character_cards`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Card
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var card Card
		if err := json.Unmarshal([]byte(raw), &card); err != nil {
			continue // skip invalid row
		}
		out = append(out, &card)
	}
	return out, rows.Err()
}

func (b *sqliteBackend) save(card *Card) error {
	data, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化角色卡失败: %w", err)
	}
	_, err = b.db.Exec(`INSERT INTO character_cards (id, name, player, system, card_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, player = excluded.player, system = excluded.system,
			card_json = excluded.card_json, updated_at = excluded.updated_at`,
		card.ID, card.Name, card.Player, card.System, string(data),
		time.Now().Format("2006-01-02 15:04:05"))
	return err
}

func (b *sqliteBackend) remove(card *Card) error {
	_, err := b.db.Exec(`DELETE FROM character_cards WHERE id = ?`, card.ID)
	return err
}

// MigrateDirToSQLite 把 dir 下的旧版人物卡 JSON 导入管理器（幂等：已有同 ID 跳过），
// 导入成功的原文件改名 .migrated 留底。返回导入数量。
func MigrateDirToSQLite(m *Manager, dir string, logf func(string, ...any)) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	migrated := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			logf("[迁移] 读取 %s 失败: %v（跳过）", name, err)
			continue
		}
		var card Card
		if err := json.Unmarshal(data, &card); err != nil || card.ID == "" {
			logf("[迁移] 解析 %s 失败（跳过）", name)
			continue
		}
		if _, err := m.Get(card.ID); err == nil {
			continue // 已存在，不覆盖（幂等）
		}
		if err := m.Create(&card); err != nil {
			logf("[迁移] 导入人物卡 %s 失败: %v（跳过）", card.ID, err)
			continue
		}
		if err := os.Rename(path, path+".migrated"); err != nil {
			logf("[迁移] 备份旧文件 %s 失败: %v", name, err)
		}
		migrated++
	}
	if migrated > 0 {
		logf("[迁移] 已导入 %d 张人物卡到数据库", migrated)
	}
	return migrated, nil
}
