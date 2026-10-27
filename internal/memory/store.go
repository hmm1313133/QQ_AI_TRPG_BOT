// Package memory 世界长期记忆的分块存储与检索。
//
// 设计取舍（与《世界设定库》lore 分离：lore 是静态设定，本包是"发生过的事"）：
//   - 一个 chunk = 一段可独立召回的记忆（阶段摘要 / 结构化事件）。
//   - 不进 WorldState JSON：避免每回合重写整份文档，也天然免疫整档 Revision 冲突。
//   - 关键词检索用 SQLite FTS5 trigram（中文子串可召回，已用 modernc 驱动验证）；
//     两字专名（如"老陈"）不进 trigram，走 subject/entities 精确路径。
//   - embedding 列预留但不启用：TRPG 的召回以专名与事件类型为主，关键词 + 实体
//   - 重要性 + 时间衰减已足够；确认召回不足后再补向量（纯 Go 暴力余弦或外部 API）。
package memory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// 记忆类型。
const (
	KindEpisode = "episode" // 阶段叙事摘要（LLM 生成，L2）
	KindEvent   = "event"   // 结构化事件（规则生成，L3）
)

// 层级：0=working（未落库），1=episode/event，2=arc（后续层级摘要）。
const (
	LevelChunk = 1
	LevelArc   = 2
)

// Chunk 一条长期记忆。
type Chunk struct {
	ID         int64    `json:"id"`
	WorldID    string   `json:"world_id"`
	Kind       string   `json:"kind"`
	Level      int      `json:"level"`
	Subject    string   `json:"subject,omitempty"` // 记忆归属实体（NPC 名/"player"），空=世界级
	RoundFrom  int      `json:"round_from,omitempty"`
	RoundTo    int      `json:"round_to,omitempty"`
	Scene      string   `json:"scene,omitempty"`
	Entities   []string `json:"entities,omitempty"`
	Text       string   `json:"text"`
	Importance int      `json:"importance"` // 规则化 0-10
	// ParentID 非 0 表示该块已被合并进某条上层摘要（arc），"剧情回顾"不再单列。
	ParentID int64 `json:"parent_id,omitempty"`
	// DedupeKey 非空时以 (world_id, dedupe_key) 唯一去重，供重试/重跑幂等。
	DedupeKey string `json:"-"`
	CreatedAt string `json:"created_at,omitempty"`
	// Score 检索时填充（不落库）。
	Score float64 `json:"-"`
}

// Query 一次记忆检索请求。
type Query struct {
	WorldID     string
	Text        string   // 当前情境文本（玩家消息 + 近期决策 + 场景实体）
	Entities    []string // 在场/相关实体名（NPC/场景/物品）
	Round       int      // 当前回合（时间衰减基准）
	Limit       int      // 返回条数上限（默认 8）
	BudgetChars int      // 字符预算（rune），<=0 不限
}

// Store 长期记忆存储（SQLite，与 world/config 共用 app.db）。
type Store struct {
	db *sql.DB
	mu sync.Mutex // 写串行化（db 本身也是单连接，这里保证批写原子性）
}

// NewStore 创建存储并建表（幂等）。
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("memory: db 不能为空")
	}
	s := &Store{db: db}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS world_memory (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			world_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			level INTEGER NOT NULL DEFAULT 1,
			subject TEXT,
			round_from INTEGER,
			round_to INTEGER,
			scene TEXT,
			entities TEXT,
			text TEXT NOT NULL,
			importance INTEGER NOT NULL DEFAULT 5,
			dedupe_key TEXT,
			created_at TEXT,
			parent_id INTEGER NOT NULL DEFAULT 0,
			embedding BLOB
		)`,
		`CREATE INDEX IF NOT EXISTS idx_world_memory_world ON world_memory(world_id, kind, round_to)`,
		// 部分唯一索引：仅对带 dedupe_key 的行去重（手动补录条目无 key）。
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_world_memory_dedupe
			ON world_memory(world_id, dedupe_key) WHERE dedupe_key IS NOT NULL AND dedupe_key <> ''`,
		// trigram 才能切中文；默认 unicode61 会把整句当一个 token。
		`CREATE VIRTUAL TABLE IF NOT EXISTS world_memory_fts USING fts5(
			text, content='world_memory', content_rowid='id', tokenize='trigram'
		)`,
		`CREATE TRIGGER IF NOT EXISTS world_memory_ai AFTER INSERT ON world_memory BEGIN
			INSERT INTO world_memory_fts(rowid, text) VALUES (new.id, new.text);
		END`,
		`CREATE TRIGGER IF NOT EXISTS world_memory_ad AFTER DELETE ON world_memory BEGIN
			INSERT INTO world_memory_fts(world_memory_fts, rowid, text) VALUES ('delete', old.id, old.text);
		END`,
		`CREATE TRIGGER IF NOT EXISTS world_memory_au AFTER UPDATE OF text ON world_memory BEGIN
			INSERT INTO world_memory_fts(world_memory_fts, rowid, text) VALUES ('delete', old.id, old.text);
			INSERT INTO world_memory_fts(rowid, text) VALUES (new.id, new.text);
		END`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("memory: 初始化表失败: %w", err)
		}
	}
	// 旧库（上一版已建 world_memory）补 parent_id 列。
	if err := s.ensureColumn("world_memory", "parent_id",
		`ALTER TABLE world_memory ADD COLUMN parent_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	return nil
}

// ensureColumn 检查列是否存在，缺失时执行 ddl（旧库升级用）。
func (s *Store) ensureColumn(table, column, ddl string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("memory: 检查表 %s 结构失败: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("memory: 读取表 %s 结构失败: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.Exec(ddl); err != nil {
		return fmt.Errorf("memory: 为表 %s 增加列 %s 失败: %w", table, column, err)
	}
	return nil
}

// Append 追加一条记忆；DedupeKey 冲突时忽略并返回已有 ID。
func (s *Store) Append(c Chunk) (int64, error) {
	if strings.TrimSpace(c.WorldID) == "" {
		return 0, errors.New("memory: world_id 不能为空")
	}
	if strings.TrimSpace(c.Text) == "" {
		return 0, errors.New("memory: text 不能为空")
	}
	if c.Kind == "" {
		c.Kind = KindEvent
	}
	if c.Level == 0 {
		c.Level = LevelChunk
	}
	if c.Importance <= 0 {
		c.Importance = 5
	}
	if c.CreatedAt == "" {
		c.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	entities, err := json.Marshal(c.Entities)
	if err != nil {
		return 0, fmt.Errorf("memory: 序列化 entities 失败: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`INSERT OR IGNORE INTO world_memory
		(world_id, kind, level, subject, round_from, round_to, scene, entities, text, importance, dedupe_key, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.WorldID, c.Kind, c.Level, c.Subject, c.RoundFrom, c.RoundTo, c.Scene,
		string(entities), c.Text, c.Importance, c.DedupeKey, c.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("memory: 写入失败: %w", err)
	}
	id, _ := res.LastInsertId()
	if id == 0 && c.DedupeKey != "" {
		_ = s.db.QueryRow(`SELECT id FROM world_memory WHERE world_id = ? AND dedupe_key = ?`,
			c.WorldID, c.DedupeKey).Scan(&id)
	}
	return id, nil
}

// AppendBatch 逐条追加（单条失败不阻塞其余条目，返回成功条数与首个错误；
// DedupeKey 保证重试幂等）。
func (s *Store) AppendBatch(chunks []Chunk) (int, error) {
	ok := 0
	var firstErr error
	for _, c := range chunks {
		if _, err := s.Append(c); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ok++
	}
	return ok, firstErr
}

// Episodes 返回阶段摘要（按时间升序）。limit <= 0 返回全部。
func (s *Store) Episodes(worldID string, limit int) ([]Chunk, error) {
	if strings.TrimSpace(worldID) == "" {
		return nil, nil
	}
	q := `SELECT ` + chunkColumns + ` FROM world_memory WHERE world_id = ? AND kind = ? ORDER BY round_to ASC, id ASC`
	args := []any{worldID, KindEpisode}
	if limit > 0 {
		q = `SELECT * FROM (SELECT ` + chunkColumns + ` FROM world_memory
			WHERE world_id = ? AND kind = ? ORDER BY round_to DESC, id DESC LIMIT ?)
			ORDER BY round_to ASC, id ASC`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: 查询阶段摘要失败: %w", err)
	}
	defer rows.Close()
	return scanChunks(rows)
}

// UncompactedEpisodes 返回尚未合并进 arc 的阶段摘要（按时间升序）。
func (s *Store) UncompactedEpisodes(worldID string, limit int) ([]Chunk, error) {
	if strings.TrimSpace(worldID) == "" {
		return nil, nil
	}
	q := `SELECT ` + chunkColumns + ` FROM world_memory
		WHERE world_id = ? AND kind = 'episode' AND level = 1
			AND (parent_id IS NULL OR parent_id = 0)
		ORDER BY round_to ASC, id ASC`
	args := []any{worldID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: 查询未归档摘要失败: %w", err)
	}
	defer rows.Close()
	return scanChunks(rows)
}

// InsertArc 写入一条上层篇章摘要（level=2）并把子摘要标记为已归档（parent_id）。
// 同一轮次范围重复写入时复用已有 arc（DedupeKey 幂等）。
func (s *Store) InsertArc(arc Chunk, childIDs []int64) (int64, error) {
	if strings.TrimSpace(arc.WorldID) == "" || strings.TrimSpace(arc.Text) == "" {
		return 0, errors.New("memory: arc 的 world_id/text 不能为空")
	}
	if arc.Kind == "" {
		arc.Kind = KindEpisode
	}
	arc.Level = LevelArc
	if arc.Importance <= 0 {
		arc.Importance = 8
	}
	if arc.CreatedAt == "" {
		arc.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	entities, err := json.Marshal(arc.Entities)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT OR IGNORE INTO world_memory
		(world_id, kind, level, subject, round_from, round_to, scene, entities, text, importance, dedupe_key, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		arc.WorldID, arc.Kind, arc.Level, arc.Subject, arc.RoundFrom, arc.RoundTo, arc.Scene,
		string(entities), arc.Text, arc.Importance, arc.DedupeKey, arc.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("memory: 写入 arc 失败: %w", err)
	}
	id, _ := res.LastInsertId()
	if id == 0 && arc.DedupeKey != "" {
		if err := tx.QueryRow(`SELECT id FROM world_memory WHERE world_id = ? AND dedupe_key = ?`,
			arc.WorldID, arc.DedupeKey).Scan(&id); err != nil {
			return 0, fmt.Errorf("memory: 查询已有 arc 失败: %w", err)
		}
	}
	if id == 0 {
		return 0, errors.New("memory: arc 写入后未取得 ID")
	}
	for _, child := range childIDs {
		if _, err := tx.Exec(`UPDATE world_memory SET parent_id = ? WHERE id = ? AND world_id = ?`,
			id, child, arc.WorldID); err != nil {
			return 0, fmt.Errorf("memory: 标记子摘要失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// Timeline 返回"剧情回顾"时间线：最近 arcLimit 条篇章摘要 + 最近 episodeLimit
// 条未归档阶段摘要，按时间升序合并（limit<=0 表示不限）。
func (s *Store) Timeline(worldID string, arcLimit, episodeLimit int) ([]Chunk, error) {
	if strings.TrimSpace(worldID) == "" {
		return nil, nil
	}
	arcs, err := s.tailEpisodes(worldID, `level >= 2`, arcLimit)
	if err != nil {
		return nil, err
	}
	eps, err := s.tailEpisodes(worldID, `level = 1 AND (parent_id IS NULL OR parent_id = 0)`, episodeLimit)
	if err != nil {
		return nil, err
	}
	out := append(arcs, eps...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RoundTo != out[j].RoundTo {
			return out[i].RoundTo < out[j].RoundTo
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Store) tailEpisodes(worldID, cond string, limit int) ([]Chunk, error) {
	q := `SELECT ` + chunkColumns + ` FROM world_memory WHERE world_id = ? AND kind = 'episode' AND ` + cond
	args := []any{worldID}
	if limit > 0 {
		q = `SELECT * FROM (` + q + ` ORDER BY round_to DESC, id DESC LIMIT ?) ORDER BY round_to ASC, id ASC`
		args = append(args, limit)
	} else {
		q += ` ORDER BY round_to ASC, id ASC`
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: 查询剧情时间线失败: %w", err)
	}
	defer rows.Close()
	return scanChunks(rows)
}

// SubjectMemories 返回归属于指定实体（NPC）的高重要性记忆。
func (s *Store) SubjectMemories(worldID, subject string, limit int) ([]Chunk, error) {
	if strings.TrimSpace(worldID) == "" || strings.TrimSpace(subject) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 2
	}
	rows, err := s.db.Query(`SELECT `+chunkColumns+` FROM world_memory
		WHERE world_id = ? AND subject = ?
		ORDER BY importance DESC, round_to DESC, id DESC LIMIT ?`, worldID, subject, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: 查询实体记忆失败: %w", err)
	}
	defer rows.Close()
	return scanChunks(rows)
}

// Retrieve 混合召回：实体精确命中 / FTS5 关键词 / 最近记忆，三路合并后打分取 top-K。
func (s *Store) Retrieve(q Query) ([]Chunk, error) {
	if strings.TrimSpace(q.WorldID) == "" {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}

	type cand struct {
		chunk     Chunk
		entityHit bool
		ftsRank   int // -1 = 未命中 FTS
		ftsTotal  int
		recent    bool
	}
	cands := make(map[int64]*cand)

	merge := func(c Chunk) *cand {
		if existing, ok := cands[c.ID]; ok {
			return existing
		}
		item := &cand{chunk: c, ftsRank: -1}
		cands[c.ID] = item
		return item
	}

	// 1. 实体路径（两字中文名也能命中；trigram 需要 ≥3 字）
	entities := sanitizeEntities(q.Entities, 8)
	if len(entities) > 0 {
		conds := make([]string, 0, len(entities)*2)
		args := []any{q.WorldID}
		for _, e := range entities {
			conds = append(conds, "subject = ?", "instr(entities, ?) > 0")
			args = append(args, e, e)
		}
		rows, err := s.db.Query(`SELECT `+chunkColumns+` FROM world_memory
			WHERE world_id = ? AND (`+strings.Join(conds, " OR ")+`)
			ORDER BY round_to DESC, id DESC LIMIT 80`, args...)
		if err == nil {
			chunks, _ := scanChunks(rows)
			rows.Close()
			for _, c := range chunks {
				merge(c).entityHit = true
			}
		}
	}

	// 2. FTS5 关键词路径（BM25 排序，取前 60 作为候选）
	if terms := ftsTerms(q.Text, 24); len(terms) > 0 {
		rows, err := s.db.Query(`SELECT `+prefixedColumns("m")+`, bm25(world_memory_fts) AS rank
			FROM world_memory_fts JOIN world_memory m ON m.id = world_memory_fts.rowid
			WHERE world_memory_fts MATCH ? AND m.world_id = ?
			ORDER BY rank LIMIT 60`, strings.Join(terms, " OR "), q.WorldID)
		if err != nil {
			log.Printf("[Memory] FTS 检索失败（忽略关键词路径）: %v", err)
		} else {
			total := 0
			for rows.Next() {
				c, err := scanChunkRow(rows, new(float64))
				if err != nil {
					break
				}
				item := merge(c)
				item.ftsRank = total
				total++
			}
			rows.Close()
			for _, item := range cands {
				if item.ftsRank >= 0 {
					item.ftsTotal = total
				}
			}
		}
	}

	// 3. 最近路径（保证"刚发生的事"始终有候选）
	if rows, err := s.db.Query(`SELECT `+chunkColumns+` FROM world_memory
		WHERE world_id = ? ORDER BY round_to DESC, id DESC LIMIT 30`, q.WorldID); err == nil {
		chunks, _ := scanChunks(rows)
		rows.Close()
		for _, c := range chunks {
			merge(c).recent = true
		}
	}

	// 打分：相关性 0.5 / 重要性 0.3 / 时间衰减 0.2（Generative Agents 权重）
	scored := make([]*cand, 0, len(cands))
	for _, item := range cands {
		rel := 0.2
		if item.entityHit {
			rel = 1.0
		} else if item.ftsRank >= 0 && item.ftsTotal > 0 {
			rel = 0.45 + 0.45*(1-float64(item.ftsRank)/float64(item.ftsTotal))
		}
		importance := float64(item.chunk.Importance)
		if importance <= 0 {
			importance = 5
		}
		importance /= 10
		gap := q.Round - item.chunk.RoundTo
		if gap < 0 {
			gap = 0
		}
		recency := 1 / (1 + float64(gap)/25)
		item.chunk.Score = 0.5*rel + 0.3*importance + 0.2*recency
		scored = append(scored, item)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].chunk.Score != scored[j].chunk.Score {
			return scored[i].chunk.Score > scored[j].chunk.Score
		}
		return scored[i].chunk.RoundTo > scored[j].chunk.RoundTo
	})

	out := make([]Chunk, 0, limit)
	used := 0
	for _, item := range scored {
		if len(out) >= limit {
			break
		}
		cost := len([]rune(item.chunk.Text)) + 8
		if q.BudgetChars > 0 && used+cost > q.BudgetChars {
			continue
		}
		used += cost
		out = append(out, item.chunk)
	}
	return out, nil
}

// UpdateText 修改一条记忆正文（玩家侧编辑摘要）。
func (s *Store) UpdateText(worldID string, id int64, text string) (bool, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return false, errors.New("memory: text 不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE world_memory SET text = ? WHERE world_id = ? AND id = ?`, text, worldID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Delete 删除一条记忆。
func (s *Store) Delete(worldID string, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM world_memory WHERE world_id = ? AND id = ?`, worldID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteRound 删除指定回合及之后的事件记忆（"回复重试"回滚后清理上一版结果，
// 避免同一回合的两版叙事事实同时存在于记忆里）。
func (s *Store) DeleteRound(worldID, kind string, fromRound int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM world_memory WHERE world_id = ? AND kind = ? AND round_from >= ?`,
		worldID, kind, fromRound)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Count 返回世界记忆条数（可观测性用）。
func (s *Store) Count(worldID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM world_memory WHERE world_id = ?`, worldID).Scan(&n)
	return n, err
}

const chunkColumns = `id, world_id, kind, level, subject, round_from, round_to, scene, entities, text, importance, created_at, parent_id`

func prefixedColumns(prefix string) string {
	parts := strings.Split(chunkColumns, ", ")
	for i, p := range parts {
		parts[i] = prefix + "." + p
	}
	return strings.Join(parts, ", ")
}

func scanChunks(rows *sql.Rows) ([]Chunk, error) {
	out := make([]Chunk, 0, 8)
	for rows.Next() {
		c, err := scanChunkRow(rows, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// scanChunkRow 扫描 chunkColumns；extra 非空时追加一列（如 bm25 rank）。
func scanChunkRow(rows *sql.Rows, extra *float64) (Chunk, error) {
	var c Chunk
	var entities string
	dest := []any{&c.ID, &c.WorldID, &c.Kind, &c.Level, &c.Subject, &c.RoundFrom, &c.RoundTo,
		&c.Scene, &entities, &c.Text, &c.Importance, &c.CreatedAt, &c.ParentID}
	if extra != nil {
		dest = append(dest, extra)
	}
	if err := rows.Scan(dest...); err != nil {
		return Chunk{}, err
	}
	if entities != "" && entities != "null" {
		_ = json.Unmarshal([]byte(entities), &c.Entities)
	}
	return c, nil
}

func sanitizeEntities(entities []string, max int) []string {
	seen := make(map[string]bool, len(entities))
	out := make([]string, 0, len(entities))
	for _, e := range entities {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
		if len(out) >= max {
			break
		}
	}
	return out
}

// ftsTerms 从文本生成 trigram 检索词（中文按 3 字滑窗，英文按词），最多 max 个。
// 两字词/两字专名不进 FTS（trigram 索引需要 ≥3 字），由实体路径负责。
func ftsTerms(text string, max int) []string {
	segs := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]bool, max)
	terms := make([]string, 0, max)
	for _, seg := range segs {
		runes := []rune(seg)
		if len(runes) < 3 {
			continue
		}
		for i := 0; i+3 <= len(runes); i++ {
			term := string(runes[i : i+3])
			if seen[term] {
				continue
			}
			seen[term] = true
			terms = append(terms, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
			if len(terms) >= max {
				return terms
			}
		}
	}
	return terms
}
