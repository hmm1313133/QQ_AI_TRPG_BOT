package persona

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // SQLite 驱动（零 CGO）
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	// 直接开测试库：不能走 world.OpenSQLite（world 已 import 本包，测试内会成环）
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	st, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return st
}

// Store 的 Set/Get/Delete roundtrip 与无记录返回 nil。
func TestStore_Roundtrip(t *testing.T) {
	st := newTestStore(t)

	// 无记录返回 (nil, nil)
	p, err := st.Get("u1")
	if err != nil || p != nil {
		t.Fatalf("无记录应返回 nil,nil: %+v %v", p, err)
	}

	// Set + Get
	if err := st.Set("u1", Profile{Name: "林月", Description: "冷静果断的私家侦探"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	p, err = st.Get("u1")
	if err != nil || p == nil || p.Name != "林月" || p.Description != "冷静果断的私家侦探" {
		t.Fatalf("Get roundtrip 不一致: %+v %v", p, err)
	}

	// 覆盖写
	if err := st.Set("u1", Profile{Name: "林月", Description: "改了"}); err != nil {
		t.Fatalf("Set 覆盖: %v", err)
	}
	p, _ = st.Get("u1")
	if p.Description != "改了" {
		t.Fatalf("覆盖写未生效: %+v", p)
	}

	// 用户隔离
	p2, _ := st.Get("u2")
	if p2 != nil {
		t.Fatal("不同用户应隔离")
	}

	// Delete
	if err := st.Delete("u1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if p, _ := st.Get("u1"); p != nil {
		t.Fatal("删除后不应能 Get")
	}
	// 重复删除不报错
	if err := st.Delete("u1"); err != nil {
		t.Fatalf("重复删除不应报错: %v", err)
	}
}
