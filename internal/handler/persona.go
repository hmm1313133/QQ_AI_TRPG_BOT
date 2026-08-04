package handler

import (
	"fmt"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// PersonaHandler 处理玩家人设指令 (.persona)。
// 两级粒度：全局默认（persona.Store）+ 每世界覆盖（WorldState.Personas），
// 生效优先级：本世界覆盖 > 全局默认。
type PersonaHandler struct {
	worldEngine *world.Engine
	store       *persona.Store
}

// NewPersonaHandler 创建玩家人设处理器。
func NewPersonaHandler(engine *world.Engine, store *persona.Store) *PersonaHandler {
	return &PersonaHandler{worldEngine: engine, store: store}
}

func (h *PersonaHandler) Name() string { return "persona" }

func (h *PersonaHandler) Match(ctx *core.MessageContext) bool {
	return ctx.Content == ".persona" || strings.HasPrefix(ctx.Content, ".persona ")
}

// personaUsage 指令用法说明。
const personaUsage = "用法:\n" +
	"  .persona                  查看当前生效人设\n" +
	"  .persona set <名字>|<描述>       设全局默认人设\n" +
	"  .persona set world <名字>|<描述> 设本世界覆盖\n" +
	"  .persona clear            清本世界覆盖（回退全局）\n" +
	"  .persona clear global     清全局默认"

func (h *PersonaHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	rest := strings.TrimSpace(strings.TrimPrefix(ctx.Content, ".persona"))
	switch {
	case rest == "":
		return h.view(ctx, reply)
	case rest == "clear":
		return h.clearWorld(ctx, reply)
	case rest == "clear global":
		return h.clearGlobal(ctx, reply)
	case strings.HasPrefix(rest, "set world "):
		return h.setWorld(ctx, reply, strings.TrimPrefix(rest, "set world "))
	case strings.HasPrefix(rest, "set "):
		return h.setGlobal(ctx, reply, strings.TrimPrefix(rest, "set "))
	default:
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, personaUsage, ctx.IsGroup)
	}
}

// parseProfile 解析 "名字|描述"（描述可含空格，取 | 后剩余部分；任一段为空报错）。
func parseProfile(text string) (*persona.Profile, error) {
	name, desc, ok := strings.Cut(text, "|")
	p := &persona.Profile{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(desc),
	}
	if !ok || p.Name == "" || p.Description == "" {
		return nil, fmt.Errorf("名字和描述都不能为空（格式: 名字|描述）")
	}
	return p, nil
}

// view .persona：查看当前生效人设，标注来源。
func (h *PersonaHandler) view(ctx *core.MessageContext, reply core.ReplyFunc) error {
	var p *persona.Profile
	source := ""
	if ws := h.worldEngine.LoadOrNil(ctx.SessionID); ws != nil {
		if wp := ws.Personas[ctx.UserID]; !wp.Empty() {
			p, source = wp, "本世界覆盖"
		}
	}
	if p == nil && h.store != nil {
		if gp, err := h.store.Get(ctx.UserID); err == nil && !gp.Empty() {
			p, source = gp, "全局默认"
		}
	}
	if p == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"尚未设置玩家人设\n"+personaUsage, ctx.IsGroup)
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("当前生效人设（来源: %s）:\n%s: %s", source, p.Name, p.Description), ctx.IsGroup)
}

// setGlobal .persona set <名字>|<描述>：设全局默认。
func (h *PersonaHandler) setGlobal(ctx *core.MessageContext, reply core.ReplyFunc, text string) error {
	p, err := parseProfile(text)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error()+"\n"+personaUsage, ctx.IsGroup)
	}
	if err := h.store.Set(ctx.UserID, *p); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "保存失败: "+err.Error(), ctx.IsGroup)
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("✅ 全局默认人设已设置: %s: %s", p.Name, p.Description), ctx.IsGroup)
}

// setWorld .persona set world <名字>|<描述>：设本世界覆盖（需进行中的世界）。
func (h *PersonaHandler) setWorld(ctx *core.MessageContext, reply core.ReplyFunc, text string) error {
	p, err := parseProfile(text)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error()+"\n"+personaUsage, ctx.IsGroup)
	}
	h.worldEngine.Lock(ctx.SessionID)
	defer h.worldEngine.Unlock(ctx.SessionID)
	ws := h.worldEngine.LoadOrNil(ctx.SessionID)
	if ws == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"当前会话还没有进行中的世界（先加载剧本或创建世界）", ctx.IsGroup)
	}
	ws.Personas[ctx.UserID] = p
	if err := h.worldEngine.Save(ws); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "保存失败: "+err.Error(), ctx.IsGroup)
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("✅ 本世界人设已设置: %s: %s", p.Name, p.Description), ctx.IsGroup)
}

// clearWorld .persona clear：清本世界覆盖（回退全局默认）。
func (h *PersonaHandler) clearWorld(ctx *core.MessageContext, reply core.ReplyFunc) error {
	h.worldEngine.Lock(ctx.SessionID)
	defer h.worldEngine.Unlock(ctx.SessionID)
	ws := h.worldEngine.LoadOrNil(ctx.SessionID)
	if ws == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"当前会话还没有进行中的世界（先加载剧本或创建世界）", ctx.IsGroup)
	}
	if _, ok := ws.Personas[ctx.UserID]; !ok {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "本世界没有设置人设覆盖", ctx.IsGroup)
	}
	delete(ws.Personas, ctx.UserID)
	if err := h.worldEngine.Save(ws); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "保存失败: "+err.Error(), ctx.IsGroup)
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "✅ 已清除本世界人设覆盖，回退全局默认", ctx.IsGroup)
}

// clearGlobal .persona clear global：清全局默认。
func (h *PersonaHandler) clearGlobal(ctx *core.MessageContext, reply core.ReplyFunc) error {
	if err := h.store.Delete(ctx.UserID); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "删除失败: "+err.Error(), ctx.IsGroup)
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "✅ 已清除全局默认人设", ctx.IsGroup)
}

var _ core.Handler = (*PersonaHandler)(nil)
