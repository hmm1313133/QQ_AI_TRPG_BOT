// 全局素材库存储（《世界编辑器与素材联动设计.md》§9.3）。
//
// 素材 = 不隶属于任何世界的可复用模板（角色/地点/物品/势力/主线），
// payload_json 存对应实体的完整 JSON，导入世界时复制为活实体。
// 混合存储：kind/name/tags/summary 列供检索，payload 不拆表。
package world

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Asset 素材库条目。
type Asset struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"` // character / location / item / faction / storyline / world
	Name      string          `json:"name"`
	Tags      []string        `json:"tags,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Source    string          `json:"source,omitempty"`    // 来源说明（手动创建 / 收藏自世界X / 剧本Y）
	Payload   json.RawMessage `json:"payload"`             // 实体完整 JSON
	CreatedAt string          `json:"created_at,omitempty"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

// 素材类型白名单。
var AssetKinds = map[string]bool{
	"character": true, "location": true, "item": true, "faction": true, "storyline": true,
	"world": true, // 世界观（payload 为 Worldview，设计 §11.3）
}

// AssetStore 素材库 SQLite 存储。
type AssetStore struct {
	db *sql.DB
}

// NewAssetStore 创建素材库存储（建表 asset_library）。
func NewAssetStore(db *sql.DB) (*AssetStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS asset_library (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			name TEXT NOT NULL,
			tags TEXT,
			summary TEXT,
			source TEXT,
			payload_json TEXT NOT NULL,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_assets_kind ON asset_library(kind)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, fmt.Errorf("建素材库表失败: %w", err)
		}
	}
	return &AssetStore{db: db}, nil
}

func nowString() string { return time.Now().Format("2006-01-02 15:04:05") }

// NewAssetID 生成素材 ID。
func NewAssetID() string { return fmt.Sprintf("ast_%d", time.Now().UnixNano()) }

func encodeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(tags)
	return string(data)
}

func decodeTags(raw sql.NullString) []string {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw.String), &tags); err != nil {
		return nil
	}
	return tags
}

// Create 新建素材。
func (s *AssetStore) Create(a *Asset) error {
	if !AssetKinds[a.Kind] {
		return fmt.Errorf("未知素材类型: %s", a.Kind)
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("素材名称不能为空")
	}
	if len(a.Payload) == 0 {
		return fmt.Errorf("素材 payload 不能为空")
	}
	if a.ID == "" {
		a.ID = NewAssetID()
	}
	a.CreatedAt = nowString()
	a.UpdatedAt = a.CreatedAt
	_, err := s.db.Exec(`INSERT INTO asset_library (id, kind, name, tags, summary, source, payload_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Kind, a.Name, encodeTags(a.Tags), a.Summary, a.Source, string(a.Payload), a.CreatedAt, a.UpdatedAt)
	return err
}

// Update 更新素材（全字段覆盖）。
func (s *AssetStore) Update(a *Asset) error {
	if !AssetKinds[a.Kind] {
		return fmt.Errorf("未知素材类型: %s", a.Kind)
	}
	a.UpdatedAt = nowString()
	res, err := s.db.Exec(`UPDATE asset_library SET kind=?, name=?, tags=?, summary=?, source=?, payload_json=?, updated_at=?
		WHERE id=?`,
		a.Kind, a.Name, encodeTags(a.Tags), a.Summary, a.Source, string(a.Payload), a.UpdatedAt, a.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("素材不存在: %s", a.ID)
	}
	return nil
}

// Get 读取素材详情（含 payload）。
func (s *AssetStore) Get(id string) (*Asset, error) {
	row := s.db.QueryRow(`SELECT id, kind, name, tags, summary, source, payload_json, created_at, updated_at
		FROM asset_library WHERE id = ?`, id)
	return scanAsset(row)
}

type assetRow interface {
	Scan(dest ...any) error
}

func scanAsset(row assetRow) (*Asset, error) {
	var a Asset
	var tags, summary, source, createdAt, updatedAt sql.NullString
	var payload string
	if err := row.Scan(&a.ID, &a.Kind, &a.Name, &tags, &summary, &source, &payload, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	a.Tags = decodeTags(tags)
	a.Summary, a.Source = summary.String, source.String
	a.CreatedAt, a.UpdatedAt = createdAt.String, updatedAt.String
	a.Payload = json.RawMessage(payload)
	return &a, nil
}

// List 列出素材（kind 过滤 + 名称/摘要模糊 + 标签过滤；不含 payload）。
func (s *AssetStore) List(kind, q, tag string) ([]Asset, error) {
	query := `SELECT id, kind, name, tags, summary, source, '' AS payload_json, created_at, updated_at FROM asset_library`
	var args []any
	var where []string
	if kind != "" {
		where = append(where, "kind = ?")
		args = append(args, kind)
	}
	if q != "" {
		where = append(where, "(name LIKE ? OR summary LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY kind, name"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		// 标签过滤在 Go 侧做（tags 是 JSON 数组，量小）
		if tag != "" && !hasTag(a.Tags, tag) {
			continue
		}
		a.Payload = nil
		out = append(out, *a)
	}
	return out, rows.Err()
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Delete 删除素材。
func (s *AssetStore) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM asset_library WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("素材不存在: %s", id)
	}
	return nil
}
