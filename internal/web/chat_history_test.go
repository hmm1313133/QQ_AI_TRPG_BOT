// 聊天记录存储与历史 API 测试。
package web

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func newChatHistoryStore(t *testing.T) *ChatHistoryStore {
	t.Helper()
	db, err := world.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	st, err := NewChatHistoryStore(db)
	if err != nil {
		t.Fatalf("创建聊天记录存储失败: %v", err)
	}
	return st
}

func TestChatHistoryStore_AddList(t *testing.T) {
	st := newChatHistoryStore(t)

	st.Add("web:tok1", "user", "我四处看看")
	st.Add("web:tok1", "reply", "你看到一扇门")
	st.Add("web:tok1", "push", "时间推进")
	st.Add("web:tok2", "user", "另一个会话")
	st.Add("web:tok1", "", "空类型也应记录") // 不校验类型，留给实现方演进

	list, err := st.List("web:tok1", 0)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("tok1 应有 4 条: %d", len(list))
	}
	// 升序
	if list[0].Text != "我四处看看" || list[1].Text != "你看到一扇门" || list[3].Text != "空类型也应记录" {
		t.Fatalf("顺序或内容错误: %+v", list)
	}
	if list[0].ID == 0 || list[0].CreatedAt == "" {
		t.Fatalf("应带 ID 与时间戳: %+v", list[0])
	}

	// 会话隔离
	list2, _ := st.List("web:tok2", 0)
	if len(list2) != 1 {
		t.Fatalf("tok2 应只有 1 条: %d", len(list2))
	}

	// limit 生效：取最近 2 条
	limited, _ := st.List("web:tok1", 2)
	if len(limited) != 2 || limited[0].Text != "时间推进" || limited[1].Text != "空类型也应记录" {
		t.Fatalf("limit 应取最近 N 条: %+v", limited)
	}

	// 空文本不记录
	st.Add("web:tok1", "user", "")
	list3, _ := st.List("web:tok1", 0)
	if len(list3) != 4 {
		t.Fatalf("空文本不应记录: %d", len(list3))
	}
}

func TestChatHistoryStore_Retention(t *testing.T) {
	st := newChatHistoryStore(t)
	for i := 0; i < chatHistoryKeep+20; i++ {
		st.Add("web:cap", "user", "msg")
	}
	list, _ := st.List("web:cap", chatHistoryKeep+100)
	if len(list) > chatHistoryKeep {
		t.Fatalf("应滚动保留 %d 条，实际 %d", chatHistoryKeep, len(list))
	}
}

// 历史 API：鉴权 + 返回记录。
func TestAdmin_ChatHistoryAPI(t *testing.T) {
	ts, _ := newAssetTestServer(t)
	defer ts.Close()

	// 未配置存储时返回空数组（newAssetTestServer 未注入 history）
	req, _ := http.NewRequest("GET", ts.URL+"/api/chat/history?token=tok_x", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var list []ChatMessage
	readJSON(t, resp, &list)
	if len(list) != 0 {
		t.Fatalf("未配置存储应返回空: %+v", list)
	}

	// 缺少 token → 401
	req, _ = http.NewRequest("GET", ts.URL+"/api/chat/history", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("缺少 token 应 401: %d", resp.StatusCode)
	}
}
