// Package config 提供 SQLite 支持的运行时配置存储。
//
// 设计见《多渠道改造计划.md》4.6：
//   - 配置持久化到 SQLite（data/app.db），KV 结构
//   - 热更新分级：HotKeys 中的键修改后立即生效（每回合读取），
//     其余键（模型/密钥/端口）重启生效
//   - 首次启动时用环境变量播种默认值
package config

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// 配置键定义。
const (
	KeyLLMModel         = "llm_model"
	KeyNarratorTemp     = "narrator_temp"
	KeyDirectorTemp     = "director_temp"
	KeyContextBudget    = "context_budget"
	KeyLoreBudget       = "lore_budget"
	KeyLoreScanRounds   = "lore_scan_rounds"
	KeyLoreRecursion    = "lore_recursion"
	KeyPlanInterval     = "plan_interval"
	KeyExtractorEnabled = "extractor_enabled"
	KeyQQAppID          = "qq_appid"
	KeyQQSecret         = "qq_secret"
	KeyLLMAPIKey        = "llm_api_key"
	KeyLLMBaseURL       = "llm_base_url"
	KeyWebChatToken     = "web_chat_token"
)

// 生效级别（修改后何时生效）。
const (
	ScopeHot            = "hot"             // 立即生效（下个回合读取）
	ScopeBotRestart     = "bot-restart"     // 重启 QQ 机器人后生效
	ScopeProcessRestart = "process-restart" // 重启进程后生效
)

// SecretMask 敏感配置的掩码值（GET 回包代替明文；PUT 原样传回视为不修改）。
const SecretMask = "********"

// KeyMeta 配置键注册元数据（管理后台据此动态渲染，消除前后端重复定义）。
type KeyMeta struct {
	Key    string `json:"key"`    // 配置键
	Label  string `json:"label"`  // 中文名
	Group  string `json:"group"`  // 分组（前端按组渲染）
	Type   string `json:"type"`   // string / number / bool
	Scope  string `json:"scope"`  // 生效级别：hot / bot-restart / process-restart
	Secret bool   `json:"secret"` // true：GET 掩码、PUT 空值或掩码原样=不修改
}

// KeyRegistry 配置键注册表（顺序即管理后台展示顺序）。
// 注意：未注册的键不允许经管理后台 PUT 写入（白名单校验）。
var KeyRegistry = []KeyMeta{
	{Key: KeyQQAppID, Label: "QQ 机器人 AppID", Group: "QQ 机器人", Type: "string", Scope: ScopeBotRestart},
	{Key: KeyQQSecret, Label: "QQ 机器人 Secret", Group: "QQ 机器人", Type: "string", Scope: ScopeBotRestart, Secret: true},
	{Key: KeyWebChatToken, Label: "Web 聊天访问 Token", Group: "Web 渠道", Type: "string", Scope: ScopeProcessRestart, Secret: true},
	{Key: KeyLLMModel, Label: "LLM 模型", Group: "LLM", Type: "string", Scope: ScopeProcessRestart},
	{Key: KeyLLMAPIKey, Label: "LLM API Key", Group: "LLM", Type: "string", Scope: ScopeProcessRestart, Secret: true},
	{Key: KeyLLMBaseURL, Label: "LLM Base URL", Group: "LLM", Type: "string", Scope: ScopeProcessRestart},
	{Key: KeyNarratorTemp, Label: "Narrator 温度", Group: "LLM", Type: "number", Scope: ScopeProcessRestart},
	{Key: KeyDirectorTemp, Label: "Planner 温度", Group: "LLM", Type: "number", Scope: ScopeProcessRestart},
	{Key: KeyContextBudget, Label: "上下文预算·字符（默认45000≈3万token）", Group: "上下文与记忆", Type: "number", Scope: ScopeHot},
	{Key: KeyLoreBudget, Label: "设定库预算·字符", Group: "上下文与记忆", Type: "number", Scope: ScopeHot},
	{Key: KeyLoreScanRounds, Label: "设定库扫描轮数", Group: "上下文与记忆", Type: "number", Scope: ScopeHot},
	{Key: KeyLoreRecursion, Label: "设定库递归扫描开关", Group: "上下文与记忆", Type: "bool", Scope: ScopeHot},
	{Key: KeyExtractorEnabled, Label: "对话记忆抽取开关", Group: "上下文与记忆", Type: "bool", Scope: ScopeHot},
	{Key: KeyPlanInterval, Label: "场景计划间隔·轮", Group: "时间轴", Type: "number", Scope: ScopeHot},
}

// Meta 查询键的注册元数据；未注册返回 false。
func Meta(key string) (KeyMeta, bool) {
	for _, m := range KeyRegistry {
		if m.Key == key {
			return m, true
		}
	}
	return KeyMeta{}, false
}

// Registered 判断键是否已注册（管理后台写入白名单）。
func Registered(key string) bool {
	_, ok := Meta(key)
	return ok
}

// HotKeys 立即生效的配置键（修改后下个回合即生效）。
var HotKeys = map[string]bool{
	KeyContextBudget:    true,
	KeyPlanInterval:     true,
	KeyExtractorEnabled: true,
	KeyLoreBudget:       true,
	KeyLoreScanRounds:   true,
	KeyLoreRecursion:    true,
}

// Store 是 SQLite 配置存储。
type Store struct {
	db    *sql.DB
	mu    sync.RWMutex
	cache map[string]string
}

// Open 打开（或创建）配置数据库。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开配置数据库失败: %w", err)
	}
	// SQLite 单写者，限制连接数避免 database is locked
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建配置表失败: %w", err)
	}

	s := &Store{db: db, cache: make(map[string]string)}
	if err := s.loadCache(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// loadCache 全量加载到内存缓存（配置量小，全量缓存换读取零 IO）。
func (s *Store) loadCache() error {
	rows, err := s.db.Query(`SELECT key, value FROM config`)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		s.cache[k] = v
	}
	return rows.Err()
}

// Close 关闭数据库。
func (s *Store) Close() error {
	return s.db.Close()
}

// Seed 在键不存在时写入默认值（环境变量播种）。
func (s *Store) Seed(key, value string) {
	if s.Get(key, "") == "" && value != "" {
		_ = s.Set(key, value)
	}
}

// Get 读取字符串配置。
func (s *Store) Get(key, def string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.cache[key]; ok {
		return v
	}
	return def
}

// GetInt 读取整数配置。
func (s *Store) GetInt(key string, def int) int {
	v := s.Get(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// GetFloat 读取浮点配置。
func (s *Store) GetFloat(key string, def float64) float64 {
	v := s.Get(key, "")
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// GetBool 读取布尔配置（"true"/"1" 为真）。
func (s *Store) GetBool(key string, def bool) bool {
	v := s.Get(key, "")
	if v == "" {
		return def
	}
	return v == "true" || v == "1"
}

// Set 写入配置（upsert + 更新缓存）。
func (s *Store) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`INSERT INTO config (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().Format("2006-01-02 15:04:05"))
	if err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	s.cache[key] = value
	return nil
}

// All 返回全部配置（管理后台用）。
func (s *Store) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string, len(s.cache))
	for k, v := range s.cache {
		result[k] = v
	}
	return result
}

// IsHot 判断配置键是否立即生效。
func IsHot(key string) bool {
	return HotKeys[key]
}
