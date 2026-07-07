// Package store 提供数据持久化能力。
package store

import (
	"fmt"
	"sync"
)

// Store 数据存储接口。
type Store interface {
	Get(key string) (interface{}, error)
	Set(key string, value interface{}) error
	Delete(key string) error
	Close() error
}

// NewStore 根据类型创建存储实例。
func NewStore(storeType, dsn string) (Store, error) {
	switch storeType {
	case "sqlite":
		return NewSQLiteStore(dsn)
	case "memory":
		return NewMemoryStore(), nil
	default:
		return nil, fmt.Errorf("不支持的存储类型: %s", storeType)
	}
}

// MemoryStore 内存存储实现，用于开发测试。
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]interface{}
}

// NewMemoryStore 创建内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]interface{})}
}

func (s *MemoryStore) Get(key string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("key %s 不存在", key)
	}
	return v, nil
}

func (s *MemoryStore) Set(key string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func (s *MemoryStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *MemoryStore) Close() error { return nil }

// SQLiteStore SQLite 存储实现。
type SQLiteStore struct {
	dsn string
	// TODO: *sql.DB 连接
}

// NewSQLiteStore 创建 SQLite 存储。
func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	s := &SQLiteStore{dsn: dsn}
	// TODO: 打开 SQLite 数据库，初始化表结构
	return s, nil
}

func (s *SQLiteStore) Get(key string) (interface{}, error)         { return nil, fmt.Errorf("未实现") }
func (s *SQLiteStore) Set(key string, value interface{}) error     { return fmt.Errorf("未实现") }
func (s *SQLiteStore) Delete(key string) error                     { return fmt.Errorf("未实现") }
func (s *SQLiteStore) Close() error                                { return nil }
