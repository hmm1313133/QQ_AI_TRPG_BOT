package handler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newPersonaTestRig 构建 persona 指令测试环境（SQLite 世界引擎 + 全局人设存储）。
func newPersonaTestRig(t *testing.T) (*PersonaHandler, *persona.Store) {
	t.Helper()
	db, err := world.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	repo, err := world.NewSQLiteRepository(db)
	if err != nil {
		t.Fatalf("NewSQLiteRepository: %v", err)
	}
	store, err := persona.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NewPersonaHandler(world.NewEngine(repo), store), store
}

// runPersonaCmd 执行一条 persona 指令并捕获回复文本。
func runPersonaCmd(t *testing.T, h *PersonaHandler, sessionID, userID, content string) string {
	t.Helper()
	var got string
	ctx := &core.MessageContext{
		Ctx:       context.Background(),
		SessionID: sessionID,
		UserID:    userID,
		Content:   content,
	}
	reply := func(_ context.Context, _, _, text string, _ bool) error {
		got = text
		return nil
	}
	if err := h.Execute(ctx, reply); err != nil {
		t.Fatalf("Execute(%q): %v", content, err)
	}
	return got
}

// persona 指令解析：set / set world / clear / clear global / 查看 / 非法输入。
func TestPersonaHandler_Commands(t *testing.T) {
	h, store := newPersonaTestRig(t)

	// 未设置时查看
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona"); !strings.Contains(got, "尚未设置") {
		t.Fatalf("未设置应提示: %q", got)
	}

	// 非法输入 -> 用法说明
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona foo"); !strings.Contains(got, "用法") {
		t.Fatalf("非法输入应给用法: %q", got)
	}
	// 缺描述 -> 报错
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona set 只有名字"); !strings.Contains(got, "名字和描述都不能为空") {
		t.Fatalf("缺 | 应报错: %q", got)
	}
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona set 名字|"); !strings.Contains(got, "名字和描述都不能为空") {
		t.Fatalf("空描述应报错: %q", got)
	}

	// set 全局（描述含空格）
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona set 林月|冷静果断的 私家侦探"); !strings.Contains(got, "全局默认人设已设置") {
		t.Fatalf("set 全局应成功: %q", got)
	}
	p, _ := store.Get("u1")
	if p == nil || p.Name != "林月" || p.Description != "冷静果断的 私家侦探" {
		t.Fatalf("全局人设写入错误: %+v", p)
	}

	// 无世界时 set world / clear -> 友好提示
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona set world 甲|乙"); !strings.Contains(got, "还没有进行中的世界") {
		t.Fatalf("无世界 set world 应提示: %q", got)
	}
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona clear"); !strings.Contains(got, "还没有进行中的世界") {
		t.Fatalf("无世界 clear 应提示: %q", got)
	}

	// 查看：标注全局默认
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona"); !strings.Contains(got, "全局默认") {
		t.Fatalf("查看应标注全局默认: %q", got)
	}

	// 有世界后 set world -> 覆盖生效
	ws := world.NewWorldState("s1", world.ModeSimRPG)
	if err := h.worldEngine.Save(ws); err != nil {
		t.Fatal(err)
	}
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona set world 侦探甲|只在本地出现"); !strings.Contains(got, "本世界人设已设置") {
		t.Fatalf("set world 应成功: %q", got)
	}
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona"); !strings.Contains(got, "本世界覆盖") || !strings.Contains(got, "侦探甲") {
		t.Fatalf("查看应标注本世界覆盖: %q", got)
	}
	// 其他会话不受影响（该用户在其他世界仍用全局默认）
	if got := runPersonaCmd(t, h, "s2", "u1", ".persona"); !strings.Contains(got, "全局默认") {
		t.Fatalf("其他会话应回退全局默认: %q", got)
	}

	// clear -> 回退全局
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona clear"); !strings.Contains(got, "已清除本世界人设覆盖") {
		t.Fatalf("clear 应成功: %q", got)
	}
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona"); !strings.Contains(got, "全局默认") {
		t.Fatalf("clear 后应回退全局默认: %q", got)
	}
	// 再 clear -> 提示无覆盖
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona clear"); !strings.Contains(got, "没有设置人设覆盖") {
		t.Fatalf("重复 clear 应提示: %q", got)
	}

	// clear global
	if got := runPersonaCmd(t, h, "s1", "u1", ".persona clear global"); !strings.Contains(got, "已清除全局默认人设") {
		t.Fatalf("clear global 应成功: %q", got)
	}
	if p, _ := store.Get("u1"); p != nil {
		t.Fatal("clear global 后不应有全局人设")
	}
}

// Match 只匹配 .persona 前缀指令。
func TestPersonaHandler_Match(t *testing.T) {
	h, _ := newPersonaTestRig(t)
	for content, want := range map[string]bool{
		".persona":          true,
		".persona set 甲|乙": true,
		".person":           false,
		".personality":      false,
	} {
		if got := h.Match(&core.MessageContext{Content: content}); got != want {
			t.Errorf("Match(%q) = %v, 期望 %v", content, got, want)
		}
	}
}
