// SQLite 世界状态存储（《世界编辑器与素材联动设计.md》§9）。
//
// 混合存储：元数据列供列表/聚合查询，state_json 存完整 WorldState 文档，
// 不做关系拆表（运行时访问模式是整聚合读出→改→写回）。
// 与配置库共用 data/app.db（modernc.org/sqlite，零 CGO）。
package world

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite 驱动（零 CGO）
)

// OpenSQLite 打开共享数据库连接（世界状态/人物卡/素材库/存档共用）。
// 与配置库（config.Open）各自持有连接到同一文件，靠 busy_timeout 错开写入。
func OpenSQLite(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// SQLite 单写者，限制连接数避免 database is locked
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 busy_timeout 失败: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 WAL 失败: %w", err)
	}
	return db, nil
}

// SQLiteRepository 世界状态的 SQLite 实现（StateRepository 接口）。
type SQLiteRepository struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteRepository 创建 SQLite 世界存储（含 worlds / saves 建表）。
func NewSQLiteRepository(db *sql.DB) (*SQLiteRepository, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS worlds (
			id TEXT PRIMARY KEY,
			mode TEXT NOT NULL,
			script_id TEXT,
			script_name TEXT,
			round_count INTEGER DEFAULT 0,
			updated_at TEXT,
			state_json TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS saves (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			world_id TEXT NOT NULL,
			name TEXT NOT NULL,
			note TEXT,
			mode TEXT,
			round_count INTEGER,
			auto INTEGER DEFAULT 0,
			created_at TEXT,
			state_json TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_saves_world ON saves(world_id)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, fmt.Errorf("建表失败: %w", err)
		}
	}
	return &SQLiteRepository{db: db}, nil
}

// Load 加载世界状态。
func (r *SQLiteRepository) Load(worldID string) (*WorldState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var raw string
	err := r.db.QueryRow(`SELECT state_json FROM worlds WHERE id = ?`, worldID).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("读取 WorldState 失败: %w", err)
	}
	return decodeWorldState([]byte(raw))
}

func decodeWorldState(data []byte) (*WorldState, error) {
	var state WorldState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("解析 WorldState JSON 失败: %w", err)
	}
	state.ensureMaps()
	return &state, nil
}

// ensureMaps 空 map 经 JSON 往返（omitempty 省略空 map）会变 nil，统一补回。
func (ws *WorldState) ensureMaps() {
	if ws.Characters == nil {
		ws.Characters = make(map[string]*CharacterState)
	}
	if ws.Locations == nil {
		ws.Locations = make(map[string]*Location)
	}
	if ws.Factions == nil {
		ws.Factions = make(map[string]*Faction)
	}
	if ws.Items == nil {
		ws.Items = make(map[string]*Item)
	}
}

// Save 事务内写入文档并同步元数据列。
func (r *SQLiteRepository) Save(state *WorldState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state.WorldID == "" {
		return fmt.Errorf("WorldState WorldID 不能为空")
	}
	state.LastUpdate = time.Now().Format("2006-01-02 15:04:05")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 WorldState 失败: %w", err)
	}
	_, err = r.db.Exec(`INSERT INTO worlds (id, mode, script_id, script_name, round_count, updated_at, state_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			mode = excluded.mode, script_id = excluded.script_id, script_name = excluded.script_name,
			round_count = excluded.round_count, updated_at = excluded.updated_at, state_json = excluded.state_json`,
		state.WorldID, state.Mode, state.ScriptID, state.ScriptName,
		state.RoundCount, state.LastUpdate, string(data))
	if err != nil {
		return fmt.Errorf("写入 WorldState 失败: %w", err)
	}
	return nil
}

// Delete 删除世界状态及其全部存档。
func (r *SQLiteRepository) Delete(worldID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM worlds WHERE id = ?`, worldID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM saves WHERE world_id = ?`, worldID); err != nil {
		return err
	}
	return tx.Commit()
}

// List 列出所有世界 ID。
func (r *SQLiteRepository) List() ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows, err := r.db.Query(`SELECT id FROM worlds ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// WorldMeta 世界元数据（列表页用，不解析 state_json）。
type WorldMeta struct {
	ID         string `json:"id"`
	Mode       string `json:"mode"`
	ScriptID   string `json:"script_id,omitempty"`
	ScriptName string `json:"script_name,omitempty"`
	RoundCount int    `json:"round_count"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

// ListMeta 列出所有世界的元数据。
func (r *SQLiteRepository) ListMeta() ([]WorldMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows, err := r.db.Query(`SELECT id, mode, script_id, script_name, round_count, updated_at FROM worlds ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorldMeta
	for rows.Next() {
		var m WorldMeta
		if err := rows.Scan(&m.ID, &m.Mode, &m.ScriptID, &m.ScriptName, &m.RoundCount, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Archive 归档旧事件日志（与 JSONRepository 同一规则化截断语义）。
func (r *SQLiteRepository) Archive(worldID string, beforeRound int) error {
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

// ============================================================
// 游玩存档（设计 §9.4）：世界状态的命名快照
// ============================================================

// autoSaveKeep 每世界保留的自动备份档数量。
const autoSaveKeep = 10

// SaveInfo 存档元数据（不含 state_json）。
type SaveInfo struct {
	ID         int64  `json:"id"`
	WorldID    string `json:"world_id"`
	Name       string `json:"name"`
	Note       string `json:"note,omitempty"`
	Mode       string `json:"mode,omitempty"`
	RoundCount int    `json:"round_count"`
	Auto       bool   `json:"auto"`
	CreatedAt  string `json:"created_at"`
}

// CreateSave 为当前世界状态创建存档快照；auto=true 时滚动保留最近 autoSaveKeep 条。
func (r *SQLiteRepository) CreateSave(state *WorldState, name, note string, auto bool) (*SaveInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化 WorldState 失败: %w", err)
	}
	info := &SaveInfo{
		WorldID: state.WorldID, Name: name, Note: note,
		Mode: state.Mode, RoundCount: state.RoundCount, Auto: auto,
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	autoInt := 0
	if auto {
		autoInt = 1
	}
	res, err := r.db.Exec(`INSERT INTO saves (world_id, name, note, mode, round_count, auto, created_at, state_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		info.WorldID, info.Name, info.Note, info.Mode, info.RoundCount, autoInt, info.CreatedAt, string(data))
	if err != nil {
		return nil, fmt.Errorf("写入存档失败: %w", err)
	}
	info.ID, _ = res.LastInsertId()
	if auto {
		// 滚动清理：仅保留每世界最近 autoSaveKeep 条自动档
		_, _ = r.db.Exec(`DELETE FROM saves WHERE world_id = ? AND auto = 1 AND id NOT IN (
			SELECT id FROM saves WHERE world_id = ? AND auto = 1 ORDER BY id DESC LIMIT ?)`,
			state.WorldID, state.WorldID, autoSaveKeep)
	}
	return info, nil
}

// ListSaves 列出世界的全部存档（新的在前）。
func (r *SQLiteRepository) ListSaves(worldID string) ([]SaveInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows, err := r.db.Query(`SELECT id, world_id, name, note, mode, round_count, auto, created_at
		FROM saves WHERE world_id = ? ORDER BY id DESC`, worldID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SaveInfo{}
	for rows.Next() {
		var s SaveInfo
		var auto int
		var note, mode, createdAt sql.NullString
		if err := rows.Scan(&s.ID, &s.WorldID, &s.Name, &note, &mode, &s.RoundCount, &auto, &createdAt); err != nil {
			return nil, err
		}
		s.Note, s.Mode, s.CreatedAt = note.String, mode.String, createdAt.String
		s.Auto = auto == 1
		out = append(out, s)
	}
	return out, rows.Err()
}

// LoadSave 读取存档内容（恢复用）。
func (r *SQLiteRepository) LoadSave(id int64) (*SaveInfo, *WorldState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var (
		info       SaveInfo
		auto       int
		note, mode sql.NullString
		raw        string
	)
	err := r.db.QueryRow(`SELECT id, world_id, name, note, mode, round_count, auto, created_at, state_json
		FROM saves WHERE id = ?`, id).
		Scan(&info.ID, &info.WorldID, &info.Name, &note, &mode, &info.RoundCount, &auto, &info.CreatedAt, &raw)
	if err != nil {
		return nil, nil, fmt.Errorf("存档不存在: %w", err)
	}
	info.Note, info.Mode, info.Auto = note.String, mode.String, auto == 1
	state, err := decodeWorldState([]byte(raw))
	if err != nil {
		return nil, nil, err
	}
	return &info, state, nil
}

// DeleteSave 删除存档。
func (r *SQLiteRepository) DeleteSave(id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, err := r.db.Exec(`DELETE FROM saves WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("存档不存在")
	}
	return nil
}

// ============================================================
// 旧 JSON 文件迁移（设计 §9.5）：导入后原文件改名 .migrated 留底
// ============================================================

// MigrateJSONDir 把 dir 下的旧版世界 JSON 文件导入库（幂等：库中已有同 ID 跳过）。
// 返回导入数量。
func MigrateJSONDir(repo *SQLiteRepository, dir string, logf func(string, ...any)) (int, error) {
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
		if entry.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			logf("[迁移] 读取 %s 失败: %v（跳过）", name, err)
			continue
		}
		state, err := decodeWorldState(data)
		if err != nil || state.WorldID == "" {
			logf("[迁移] 解析 %s 失败: %v（跳过）", name, err)
			continue
		}
		if _, err := repo.Load(state.WorldID); err == nil {
			continue // 库中已存在，不覆盖（幂等）
		}
		if err := repo.Save(state); err != nil {
			logf("[迁移] 导入世界 %s 失败: %v（跳过）", state.WorldID, err)
			continue
		}
		if err := os.Rename(path, path+".migrated"); err != nil {
			logf("[迁移] 备份旧文件 %s 失败: %v", name, err)
		}
		migrated++
	}
	if migrated > 0 {
		logf("[迁移] 已导入 %d 个世界状态到数据库", migrated)
	}
	return migrated, nil
}
