package bot

import (
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

func newTestBot(t *testing.T, credFn func() (string, string)) *Bot {
	t.Helper()
	router := core.NewRouter(core.NewPluginManager(), core.NewSessionManager(), nil)
	b, err := NewBot(&Config{CredFn: credFn}, router)
	if err != nil {
		t.Fatalf("创建 Bot 失败: %v", err)
	}
	return b
}

func TestBot_StartWithoutCreds(t *testing.T) {
	b := newTestBot(t, func() (string, string) { return "", "" })
	if err := b.Start(); err == nil {
		t.Fatal("缺少凭证时 Start 应返回错误")
	}
	if st := b.Status(); st.Running {
		t.Fatal("启动失败后 Running 应为 false")
	}
}

func TestBot_LifecycleIdempotent(t *testing.T) {
	b := newTestBot(t, func() (string, string) { return "dummy-appid", "dummy-secret" })
	t.Cleanup(func() { _ = b.Stop() })

	// 未运行时 Stop 幂等
	if err := b.Stop(); err != nil {
		t.Fatalf("未运行时 Stop 应返回 nil: %v", err)
	}

	// 启动 + 重复启动幂等
	if err := b.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("重复 Start 应返回 nil: %v", err)
	}
	st := b.Status()
	if !st.Running {
		t.Fatal("启动后 Running 应为 true")
	}
	if st.AppID != "dummy-appid" {
		t.Fatalf("Status 应返回当前 AppID: %q", st.AppID)
	}

	// 重启（重读凭证）
	if err := b.Restart(); err != nil {
		t.Fatalf("Restart 失败: %v", err)
	}
	if !b.Status().Running {
		t.Fatal("重启后 Running 应为 true")
	}

	// 停止 + 重复停止幂等
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("重复 Stop 应返回 nil: %v", err)
	}
	if b.Status().Running {
		t.Fatal("停止后 Running 应为 false")
	}
}

func TestBot_RestartRereadsCreds(t *testing.T) {
	appID := "old-appid"
	b := newTestBot(t, func() (string, string) { return appID, "secret" })
	t.Cleanup(func() { _ = b.Stop() })

	if err := b.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	appID = "new-appid"
	if err := b.Restart(); err != nil {
		t.Fatalf("Restart 失败: %v", err)
	}
	if st := b.Status(); st.AppID != "new-appid" {
		t.Fatalf("重启后应使用新凭证: %q", st.AppID)
	}
}
