package handler

import (
	"fmt"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// WorldHandler 处理"进入世界"指令（.world list / enter / reset）。
//
// 解决会话 ID（QQ=group:xxx、网页=web:<token>）与管理后台创建的世界 ID
// （world_xxx）对不上的问题：.world enter 把后台世界实例化复制到当前会话
// （整体快照，含轮次/状态原样带入），源世界不受影响。
type WorldHandler struct {
	worldEngine *world.Engine
	sessionMgr  *core.SessionManager
}

// NewWorldHandler 创建进入世界处理器。
func NewWorldHandler(engine *world.Engine, sm *core.SessionManager) *WorldHandler {
	return &WorldHandler{worldEngine: engine, sessionMgr: sm}
}

func (h *WorldHandler) Name() string { return "world" }

func (h *WorldHandler) Match(ctx *core.MessageContext) bool {
	return ctx.Content == ".world" || strings.HasPrefix(ctx.Content, ".world ")
}

// worldUsage 指令用法说明。
const worldUsage = "用法:\n" +
	"  .world list         列出可进入的世界\n" +
	"  .world enter <ID>   进入世界（实例化复制到当前会话）\n" +
	"  .world reset        退出并删除当前会话的世界"

func (h *WorldHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	rest := strings.TrimSpace(strings.TrimPrefix(ctx.Content, ".world"))
	switch {
	case rest == "list":
		return h.list(ctx, reply)
	case rest == "reset":
		return h.reset(ctx, reply)
	case strings.HasPrefix(rest, "enter "):
		return h.enter(ctx, reply, strings.TrimSpace(strings.TrimPrefix(rest, "enter ")))
	default:
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, worldUsage, ctx.IsGroup)
	}
}

// worldBrief 一行世界概况（list 用）。
func worldBrief(ws *world.WorldState) string {
	// 剧本名优先，否则背景摘要（前 20 字）
	desc := ws.ScriptName
	if desc == "" {
		desc = firstRunesH(strings.TrimSpace(ws.Background), 20)
	}
	if desc == "" {
		desc = "（无描述）"
	}
	return fmt.Sprintf("  %s [%s] %s（轮次 %d，角色 %d）",
		ws.WorldID, ws.Mode, desc, ws.RoundCount, len(ws.Characters))
}

// firstRunesH 取字符串前 n 个字符（超出追加省略号）。
func firstRunesH(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// list .world list：列出可进入的世界（排除当前会话自己的世界）。
func (h *WorldHandler) list(ctx *core.MessageContext, reply core.ReplyFunc) error {
	ids, err := h.worldEngine.ListWorlds()
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "查询失败: "+err.Error(), ctx.IsGroup)
	}
	var sb strings.Builder
	count := 0
	// 世界数量小，逐个加载取元信息可接受
	for _, id := range ids {
		if id == ctx.SessionID {
			continue // 排除当前会话自己的世界
		}
		ws := h.worldEngine.LoadOrNil(id)
		if ws == nil {
			continue
		}
		sb.WriteString(worldBrief(ws) + "\n")
		count++
	}
	if count == 0 {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"暂无可进入的世界（管理后台创建世界后可在此进入）", ctx.IsGroup)
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		"🌍 可进入的世界:\n"+sb.String()+"进入: .world enter <ID>", ctx.IsGroup)
}

// enter .world enter <id>：实例化复制源世界到当前会话，并自动切换到跑团模式。
func (h *WorldHandler) enter(ctx *core.MessageContext, reply core.ReplyFunc, id string) error {
	if id == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, worldUsage, ctx.IsGroup)
	}
	if id == ctx.SessionID {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "这就是当前会话的世界，无需进入", ctx.IsGroup)
	}

	h.worldEngine.Lock(ctx.SessionID)
	defer h.worldEngine.Unlock(ctx.SessionID)

	if h.worldEngine.LoadOrNil(ctx.SessionID) != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"当前会话已有进行中的世界，先 .world reset 退出后再进入", ctx.IsGroup)
	}

	src := h.worldEngine.LoadOrNil(id)
	if src == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("世界不存在: %s（.world list 查看可进入的世界）", id), ctx.IsGroup)
	}

	// 实例化复制：repo Load 返回的是新解码对象，直接改 ID 写回即为整体快照，
	// 轮次/场景/角色等状态原样带入，源世界不受影响。
	src.WorldID = ctx.SessionID
	if err := h.worldEngine.Save(src); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "进入失败: "+err.Error(), ctx.IsGroup)
	}

	// 进入后自动切跑团模式，发送消息即可开始
	h.sessionMgr.SetMode(ctx.SessionID, core.ModeTRPG)

	styleTag := "无"
	if src.Style != nil || strings.TrimSpace(src.ReplyStyle) != "" {
		styleTag = "有"
	}
	scene := src.Scene.NodeName
	if scene == "" {
		scene = "（未设置）"
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("✅ 已进入世界「%s」（实例化复制，带入世界当前状态，轮次 %d）\n模式: %s | 场景: %s | 在场角色: %d | 风格配置: %s\n已自动切换到跑团模式，发送消息即可开始。",
			id, src.RoundCount, src.Mode, scene, len(src.Characters), styleTag), ctx.IsGroup)
}

// reset .world reset：删除当前会话的世界（源世界模板与其存档不受影响）。
func (h *WorldHandler) reset(ctx *core.MessageContext, reply core.ReplyFunc) error {
	h.worldEngine.Lock(ctx.SessionID)
	defer h.worldEngine.Unlock(ctx.SessionID)

	if h.worldEngine.LoadOrNil(ctx.SessionID) == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "当前会话没有进行中的世界", ctx.IsGroup)
	}

	// 存档快照清点（级联删除前提示；repo 非 SQLite 时静默跳过）
	saveNote := ""
	if repo, ok := h.worldEngine.Repo().(*world.SQLiteRepository); ok {
		if saves, err := repo.ListSaves(ctx.SessionID); err == nil && len(saves) > 0 {
			saveNote = fmt.Sprintf("（本会话的 %d 个存档快照也一并删除）", len(saves))
		}
	}

	if err := h.worldEngine.Delete(ctx.SessionID); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "删除失败: "+err.Error(), ctx.IsGroup)
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		"✅ 已退出并删除当前会话的世界"+saveNote+"。源世界模板与其存档快照仍在，可 .world enter 重新进入。", ctx.IsGroup)
}

var _ core.Handler = (*WorldHandler)(nil)
