// TurnEngine 回合引擎测试。
package agent

import (
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func newResolveStateEnv(t *testing.T, withWorld bool) (*TurnEngine, *core.Session) {
	t.Helper()
	repo, err := world.NewJSONRepository(t.TempDir())
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}
	engine := world.NewEngine(repo)
	if withWorld {
		if _, err := engine.CreateWorld("s1", world.ModeSimRPG); err != nil {
			t.Fatalf("创建世界失败: %v", err)
		}
	}
	te := NewTurnEngine(nil, nil, nil, engine, nil, 0)
	return te, &core.Session{ID: "s1", Mode: core.ModeNormal, Data: map[string]interface{}{}}
}

// 进程重启后会话模式回落 normal，但世界状态仍在：应恢复为世界内扮演，
// 而不是走纯对话路径（世界观/人设/历史全部不注入）。
func TestResolveState_NormalModeWithWorld_RecoversFreechat(t *testing.T) {
	te, session := newResolveStateEnv(t, true)

	state := te.resolveState(session, "s1")
	if state == nil {
		t.Fatal("世界状态存在时应加载，而不是走纯对话路径")
	}
	if session.Mode != core.ModeFreeChat {
		t.Fatalf("应恢复为 freechat: %v", session.Mode)
	}
}

// normal 且无世界状态：纯对话路径。
func TestResolveState_NormalModeNoWorld(t *testing.T) {
	te, session := newResolveStateEnv(t, false)

	if state := te.resolveState(session, "s1"); state != nil {
		t.Fatal("无世界状态应返回 nil")
	}
	if session.Mode != core.ModeNormal {
		t.Fatalf("模式不应被改动: %v", session.Mode)
	}
}

// freechat 但世界状态已被删除（如管理端删除）：返回 nil 走纯对话兜底。
func TestResolveState_FreechatWorldGone(t *testing.T) {
	te, session := newResolveStateEnv(t, false)
	session.Mode = core.ModeFreeChat

	if state := te.resolveState(session, "s1"); state != nil {
		t.Fatal("世界已删除应返回 nil")
	}
	if session.Mode != core.ModeFreeChat {
		t.Fatalf("freechat 显式设置不应被改动: %v", session.Mode)
	}
}
