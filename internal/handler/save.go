package handler

import (
	"fmt"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// SaveHandler 处理游玩存档指令（.save / .saves / .restore）。
// 会话即世界（WorldID == SessionID），QQ 侧只恢复世界状态，
// 不做对话历史回放（QQ 无服务端历史）。
type SaveHandler struct {
	worldEngine *world.Engine
}

// NewSaveHandler 创建游玩存档处理器。
func NewSaveHandler(engine *world.Engine) *SaveHandler {
	return &SaveHandler{worldEngine: engine}
}

func (h *SaveHandler) Name() string { return "save" }

func (h *SaveHandler) Match(ctx *core.MessageContext) bool {
	c := ctx.Content
	return c == ".save" || strings.HasPrefix(c, ".save ") ||
		c == ".saves" ||
		c == ".restore" || strings.HasPrefix(c, ".restore ")
}

func (h *SaveHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	repo, ok := h.worldEngine.Repo().(*world.SQLiteRepository)
	if !ok {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "存档功能需要 SQLite 存储", ctx.IsGroup)
	}

	switch {
	case ctx.Content == ".saves":
		return h.listSaves(ctx, reply, repo)
	case ctx.Content == ".restore" || strings.HasPrefix(ctx.Content, ".restore "):
		return h.restoreSave(ctx, reply)
	default:
		return h.createSave(ctx, reply, repo)
	}
}

// createSave .save [名字]：为当前世界创建存档快照（QQ 侧不带对话历史）。
func (h *SaveHandler) createSave(ctx *core.MessageContext, reply core.ReplyFunc, repo *world.SQLiteRepository) error {
	ws := h.worldEngine.LoadOrNil(ctx.SessionID)
	if ws == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"当前会话还没有进行中的世界（先 .script load 加载剧本）", ctx.IsGroup)
	}
	name := strings.TrimSpace(strings.TrimPrefix(ctx.Content, ".save"))
	if name == "" {
		name = "存档 " + time.Now().Format("01-02 15:04")
	}
	info, err := repo.CreateSave(ws, name, "", false)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "保存失败: "+err.Error(), ctx.IsGroup)
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("✅ 存档已创建: #%d「%s」（轮次 %d）", info.ID, info.Name, info.RoundCount), ctx.IsGroup)
}

// listSaves .saves：列出当前世界的全部存档。
func (h *SaveHandler) listSaves(ctx *core.MessageContext, reply core.ReplyFunc, repo *world.SQLiteRepository) error {
	list, err := repo.ListSaves(ctx.SessionID)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "查询失败: "+err.Error(), ctx.IsGroup)
	}
	if len(list) == 0 {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "当前世界暂无存档（.save [名字] 创建）", ctx.IsGroup)
	}
	var sb strings.Builder
	sb.WriteString("📦 当前世界存档:\n")
	for _, s := range list {
		autoTag := ""
		if s.Auto {
			autoTag = " [自动]"
		}
		sb.WriteString(fmt.Sprintf("  #%d %s%s（轮次 %d，%s）\n", s.ID, s.Name, autoTag, s.RoundCount, s.CreatedAt))
	}
	sb.WriteString("恢复: .restore <ID>")
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, strings.TrimRight(sb.String(), "\n"), ctx.IsGroup)
}

// restoreSave .restore <ID>：恢复存档（当前进度自动备份；QQ 侧不回放历史）。
func (h *SaveHandler) restoreSave(ctx *core.MessageContext, reply core.ReplyFunc) error {
	parts := strings.SplitN(ctx.Content, " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .restore <存档ID>（.saves 查看列表）", ctx.IsGroup)
	}
	var saveID int64
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &saveID); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "存档 ID 无效", ctx.IsGroup)
	}
	info, _, err := h.worldEngine.RestoreSave(ctx.SessionID, saveID, "player")
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "恢复失败: "+err.Error(), ctx.IsGroup)
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("✅ 已恢复到存档「%s」（轮次 %d），当前进度已自动备份", info.Name, info.RoundCount), ctx.IsGroup)
}

var _ core.Handler = (*SaveHandler)(nil)
