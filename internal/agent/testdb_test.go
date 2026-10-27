// 测试用内存数据库：不落盘，避免 Windows 下临时目录清理竞态。
package agent

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func newAgentTestEngine(t *testing.T) *world.Engine {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	repo, err := world.NewSQLiteRepository(db)
	if err != nil {
		t.Fatalf("创建世界仓库失败: %v", err)
	}
	return world.NewEngine(repo)
}
