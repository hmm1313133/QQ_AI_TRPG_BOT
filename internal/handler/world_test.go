package handler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newWorldTestRig 构建进入世界指令测试环境（SQLite 世界引擎 + 会话管理器）。
func newWorldTestRig(t *testing.T) (*WorldHandler, *world.Engine, *core.SessionManager) {
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
	engine := world.NewEngine(repo)
	sm := core.NewSessionManager()
	return NewWorldHandler(engine, sm), engine, sm
}

// runWorldCmd 执行一条 .world 指令并捕获回复文本。
func runWorldCmd(t *testing.T, h *WorldHandler, sessionID, content string) string {
	t.Helper()
	var got string
	ctx := &core.MessageContext{
		Ctx:       context.Background(),
		SessionID: sessionID,
		UserID:    "u1",
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

// 播种一个后台世界（world_xxx）与一个会话世界（s1）。
func seedWorlds(t *testing.T, engine *world.Engine) {
	t.Helper()
	tpl := world.NewWorldState("world_tpl", world.ModeRoleplay)
	tpl.Background = "雾都的私家侦探事务所，阴雨连绵的港口城市，暗藏玄机"
	tpl.RoundCount = 5
	tpl.Scene.NodeName = "事务所"
	tpl.Characters["侦探"] = &world.CharacterState{Name: "侦探", Disposition: "neutral"}
	if err := engine.Save(tpl); err != nil {
		t.Fatal(err)
	}
	own := world.NewWorldState("s1", world.ModeSimRPG)
	if err := engine.Save(own); err != nil {
		t.Fatal(err)
	}
}

// list 排除当前会话自己的世界；内容含元信息。
func TestWorldHandler_List(t *testing.T) {
	h, engine, _ := newWorldTestRig(t)
	seedWorlds(t, engine)

	got := runWorldCmd(t, h, "s1", ".world list")
	if !strings.Contains(got, "world_tpl") || !strings.Contains(got, "roleplay") {
		t.Fatalf("应列出后台世界: %q", got)
	}
	if strings.Contains(got, "  s1 ") {
		t.Fatalf("不应列出当前会话自己的世界: %q", got)
	}
	// 背景摘要（前 20 字 + 省略号）
	if !strings.Contains(got, "雾都的私家侦探事务所，阴雨连绵的港") {
		t.Fatalf("应含背景摘要: %q", got)
	}

	// 换一个会话视角：排除的是该会话自己的世界
	got2 := runWorldCmd(t, h, "world_tpl", ".world list")
	if strings.Contains(got2, "  world_tpl ") {
		t.Fatalf("不应列出当前会话自己的世界: %q", got2)
	}
	if !strings.Contains(got2, "  s1 ") {
		t.Fatalf("其他世界应正常列出: %q", got2)
	}

	// 全部排除后无世界时友好提示（新引擎只有自己的世界）
	h2, engine2, _ := newWorldTestRig(t)
	own := world.NewWorldState("s1", world.ModeSimRPG)
	if err := engine2.Save(own); err != nil {
		t.Fatal(err)
	}
	if got3 := runWorldCmd(t, h2, "s1", ".world list"); !strings.Contains(got3, "暂无可进入的世界") {
		t.Fatalf("排除自身后无世界应提示: %q", got3)
	}
}

// enter 实例化复制：源世界不变、会话世界 ID 正确、内容一致；自动切跑团模式。
func TestWorldHandler_Enter(t *testing.T) {
	h, engine, sm := newWorldTestRig(t)
	tpl := world.NewWorldState("world_tpl", world.ModeRoleplay)
	tpl.Background = "雾都"
	tpl.RoundCount = 5
	tpl.Scene.NodeName = "事务所"
	tpl.Characters["侦探"] = &world.CharacterState{Name: "侦探"}
	if err := engine.Save(tpl); err != nil {
		t.Fatal(err)
	}

	got := runWorldCmd(t, h, "s2", ".world enter world_tpl")
	if !strings.Contains(got, "已进入世界") || !strings.Contains(got, "轮次 5") {
		t.Fatalf("进入回复应含状态信息: %q", got)
	}

	// 会话世界已复制：ID 正确、内容一致
	cp := engine.LoadOrNil("s2")
	if cp == nil {
		t.Fatal("会话世界应已创建")
	}
	if cp.RoundCount != 5 || cp.Scene.NodeName != "事务所" || cp.Background != "雾都" || len(cp.Characters) != 1 {
		t.Fatalf("复制内容不一致: %+v", cp.Scene)
	}

	// 源世界不变（实例化语义，不是移动）
	src := engine.LoadOrNil("world_tpl")
	if src == nil || src.WorldID != "world_tpl" || src.RoundCount != 5 {
		t.Fatal("源世界应保持不变")
	}

	// 自动切跑团模式
	if sm.GetMode("s2") != core.ModeTRPG {
		t.Fatalf("进入后应自动切跑团模式: %v", sm.GetMode("s2"))
	}

	// 副本独立：改副本不影响源
	cp.RoundCount = 99
	if err := engine.Save(cp); err != nil {
		t.Fatal(err)
	}
	if engine.LoadOrNil("world_tpl").RoundCount != 5 {
		t.Fatal("副本修改不应影响源世界")
	}
}

// enter 边界：世界不存在 / 已有进行中的世界拒绝。
func TestWorldHandler_EnterReject(t *testing.T) {
	h, engine, _ := newWorldTestRig(t)
	seedWorlds(t, engine)

	if got := runWorldCmd(t, h, "s9", ".world enter world_none"); !strings.Contains(got, "世界不存在") {
		t.Fatalf("不存在应报错: %q", got)
	}

	// s1 已有进行中的世界 -> 拒绝
	got := runWorldCmd(t, h, "s1", ".world enter world_tpl")
	if !strings.Contains(got, "已有进行中的世界") {
		t.Fatalf("已有世界应拒绝: %q", got)
	}
	// 拒绝后 s1 世界仍是自己的（未被覆盖）
	if engine.LoadOrNil("s1").Mode != world.ModeSimRPG {
		t.Fatal("拒绝进入不应覆盖现有世界")
	}
}

// reset 删除当前会话世界；无世界时友好提示。
func TestWorldHandler_Reset(t *testing.T) {
	h, engine, _ := newWorldTestRig(t)
	seedWorlds(t, engine)

	if got := runWorldCmd(t, h, "s9", ".world reset"); !strings.Contains(got, "没有进行中的世界") {
		t.Fatalf("无世界应提示: %q", got)
	}

	got := runWorldCmd(t, h, "s1", ".world reset")
	if !strings.Contains(got, "已退出并删除") {
		t.Fatalf("reset 应成功: %q", got)
	}
	if engine.LoadOrNil("s1") != nil {
		t.Fatal("reset 后会话世界应已删除")
	}
	// 源世界不受影响
	if engine.LoadOrNil("world_tpl") == nil {
		t.Fatal("reset 不应影响其他世界")
	}

	// reset 后又能 enter
	if got := runWorldCmd(t, h, "s1", ".world enter world_tpl"); !strings.Contains(got, "已进入世界") {
		t.Fatalf("reset 后应能重新进入: %q", got)
	}
}

// Match 只匹配 .world 前缀指令。
func TestWorldHandler_Match(t *testing.T) {
	h, _, _ := newWorldTestRig(t)
	for content, want := range map[string]bool{
		".world":            true,
		".world enter w1":   true,
		".worldlist":        false,
		".worlds":           false,
	} {
		if got := h.Match(&core.MessageContext{Content: content}); got != want {
			t.Errorf("Match(%q) = %v, 期望 %v", content, got, want)
		}
	}
}
