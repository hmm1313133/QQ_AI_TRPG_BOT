// 测试用内存数据库：不落盘，避免 Windows 下 t.TempDir 清理与 SQLite 文件
// 删除的竞态导致偶发 "directory is not empty"。
package web

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
