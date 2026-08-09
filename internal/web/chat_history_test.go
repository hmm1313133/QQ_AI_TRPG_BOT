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

// ---- AI 回复多版本（重试/编辑）----

func TestChatHistoryStore_ReplyVersions(t *testing.T) {
	st := newChatHistoryStore(t)

	st.Add("web:v1", "user", "我推开门")
	replyID := st.Add("web:v1", "reply", "门后是一条走廊")
	if replyID == 0 {
		t.Fatal("Add 应返回新行 id")
	}

	// 无版本时聚合字段为 0
	list, _ := st.List("web:v1", 0)
	if len(list) != 2 || list[1].VersionNo != 0 || list[1].VersionCount != 0 {
		t.Fatalf("无版本时 version 字段应为 0: %+v", list)
	}

	// 首次重试：现有文本落 v1，新文本落 v2 并激活，chat_messages 镜像
	vno, count, err := st.AddReplyVersion(replyID, "门后是一堵墙")
	if err != nil || vno != 2 || count != 2 {
		t.Fatalf("首次加版本应得 (2,2): (%d,%d,%v)", vno, count, err)
	}
	msg, err := st.GetMessage("web:v1", replyID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg.Text != "门后是一堵墙" || msg.VersionNo != 2 || msg.VersionCount != 2 {
		t.Fatalf("镜像/聚合错误: %+v", msg)
	}

	// 再次重试：v3
	vno, count, err = st.AddReplyVersion(replyID, "门后是一座大厅")
	if err != nil || vno != 3 || count != 3 {
		t.Fatalf("再次加版本应得 (3,3): (%d,%d,%v)", vno, count, err)
	}

	// 切回 v1：返回激活文本并镜像
	text, count, err := st.SetActiveVersion(replyID, 1)
	if err != nil || text != "门后是一条走廊" || count != 3 {
		t.Fatalf("切回 v1 错误: (%q,%d,%v)", text, count, err)
	}
	msg, _ = st.GetMessage("web:v1", replyID)
	if msg.Text != "门后是一条走廊" || msg.VersionNo != 1 {
		t.Fatalf("v1 应已激活并镜像: %+v", msg)
	}

	// 编辑当前激活版本（v1）
	if err := st.UpdateActiveText(replyID, "门后是一条长长的走廊"); err != nil {
		t.Fatalf("UpdateActiveText: %v", err)
	}
	msg, _ = st.GetMessage("web:v1", replyID)
	if msg.Text != "门后是一条长长的走廊" || msg.VersionNo != 1 || msg.VersionCount != 3 {
		t.Fatalf("编辑后镜像/聚合错误: %+v", msg)
	}
	// 切到 v2 再切回 v1，确认编辑改的是 v1 版本行
	if _, _, err := st.SetActiveVersion(replyID, 2); err != nil {
		t.Fatal(err)
	}
	text, _, err = st.SetActiveVersion(replyID, 1)
	if err != nil || text != "门后是一条长长的走廊" {
		t.Fatalf("编辑应同步到激活版本行: (%q,%v)", text, err)
	}

	// 不存在的版本 → 错误
	if _, _, err := st.SetActiveVersion(replyID, 9); err == nil {
		t.Fatal("不存在的版本应报错")
	}
	// 不存在的消息 → 错误
	if _, _, err := st.AddReplyVersion(99999, "x"); err == nil {
		t.Fatal("不存在的消息加版本应报错")
	}
	// 其他会话 → GetMessage 报错
	if _, err := st.GetMessage("web:other", replyID); err == nil {
		t.Fatal("跨会话 GetMessage 应报错")
	}
}

func TestChatHistoryStore_RegenHelpers(t *testing.T) {
	st := newChatHistoryStore(t)

	if last, err := st.LastReplyOf("web:rh"); err != nil || last != nil {
		t.Fatalf("空会话 LastReplyOf 应为 nil: %+v, %v", last, err)
	}

	st.Add("web:rh", "user", "第一句")
	r1 := st.Add("web:rh", "reply", "回复一")
	st.Add("web:rh", "user", "第二句")
	r2 := st.Add("web:rh", "reply", "回复二")

	last, err := st.LastReplyOf("web:rh")
	if err != nil || last == nil || last.ID != r2 {
		t.Fatalf("LastReplyOf 应为最新 reply: %+v, %v", last, err)
	}

	// r1 之后有更新的 user 消息；r2 之后没有
	if newer, _ := st.HasNewerUserAfter("web:rh", r1); !newer {
		t.Fatal("r1 之后应有更新的 user 消息")
	}
	if newer, _ := st.HasNewerUserAfter("web:rh", r2); newer {
		t.Fatal("r2 之后不应有更新的 user 消息")
	}

	// 目标 reply 前最近一条 user 消息
	if text, ok := st.UserTextBefore("web:rh", r2); !ok || text != "第二句" {
		t.Fatalf("UserTextBefore 应为「第二句」: (%q,%v)", text, ok)
	}
	if _, ok := st.UserTextBefore("web:rh", 1); ok {
		t.Fatal("首条消息之前不应有 user 消息")
	}
}

// 滚动清理应级联删除被清消息的版本行。
func TestChatHistoryStore_RetentionCascadesVersions(t *testing.T) {
	st := newChatHistoryStore(t)

	victim := st.Add("web:cap2", "reply", "将被滚动清掉的回复")
	if _, _, err := st.AddReplyVersion(victim, "新版本"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < chatHistoryKeep+20; i++ {
		st.Add("web:cap2", "user", "msg")
	}
	// 原消息已被清理
	list, _ := st.List("web:cap2", chatHistoryKeep+100)
	if len(list) > chatHistoryKeep {
		t.Fatalf("应滚动保留 %d 条，实际 %d", chatHistoryKeep, len(list))
	}
	// 版本行也应被清理（对孤立 message_id 加版本应报"消息不存在"）
	if _, _, err := st.AddReplyVersion(victim, "x"); err == nil {
		t.Fatal("被清理消息的版本行应已级联删除（消息不存在应报错）")
	}
}

// Replace（存档恢复回放）应清空该会话全部版本行。
func TestChatHistoryStore_ReplaceClearsVersions(t *testing.T) {
	st := newChatHistoryStore(t)

	st.Add("web:rp", "user", "你好")
	rid := st.Add("web:rp", "reply", "你好呀")
	if _, _, err := st.AddReplyVersion(rid, "你好呀（重试）"); err != nil {
		t.Fatal(err)
	}

	if err := st.Replace("web:rp", []ChatMessage{
		{Type: "user", Text: "存档里的用户消息"},
		{Type: "reply", Text: "存档里的回复"},
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	list, _ := st.List("web:rp", 0)
	if len(list) != 2 || list[1].Text != "存档里的回复" || list[1].VersionCount != 0 {
		t.Fatalf("Replace 后应为存档内容且无版本: %+v", list)
	}
	// 旧消息 id 的版本行已随消息一起清除
	if _, err := st.GetMessage("web:rp", rid); err == nil {
		t.Fatal("Replace 后旧消息应不存在")
	}
}
