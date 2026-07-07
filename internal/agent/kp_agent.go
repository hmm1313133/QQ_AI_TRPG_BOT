package agent

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// KPAgent 是跑团主持 AI Agent。
// 基于 trpc-agent-go 的 LLMAgent 实现，使用 DeepSeek 模型。
// 通过 Session 获取游戏上下文，通过 FunctionTool 调用骰子等工具。
type KPAgent struct {
	config     *Config
	agent      agent.Agent
	runner     runner.Runner
	tools      []tool.Tool
	sessionMgr *core.SessionManager
}

// NewKPAgent 创建 KP/DM 主持 Agent。
// 使用 trpc-agent-go 的 LLMAgent + DeepSeek 模型。
func NewKPAgent(cfg *Config, sessionMgr *core.SessionManager) (*KPAgent, error) {
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = DefaultKPPrompt()
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.8
	}
	if cfg.MemoryWindow == 0 {
		cfg.MemoryWindow = 20
	}

	// 设置环境变量（trpc-agent-go 的 openai 模型通过环境变量读取密钥）
	if cfg.LLMAPIKey != "" {
		os.Setenv("OPENAI_API_KEY", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "" {
		os.Setenv("OPENAI_BASE_URL", cfg.LLMBaseURL)
	}

	// 1. 创建 DeepSeek 模型实例
	// trpc-agent-go 的 openai 模块通过 VariantDeepSeek 支持 DeepSeek API
	modelOpts := []openai.Option{
		openai.WithVariant(openai.VariantDeepSeek),
	}
	modelInstance := openai.New(cfg.LLMModel, modelOpts...)

	// 2. 创建 TRPG 工具
	tools := []tool.Tool{
		NewRollDiceTool(sessionMgr),
	}

	// 3. 创建 LLMAgent
	genConfig := model.GenerationConfig{
		MaxTokens:   &cfg.MaxTokens,
		Temperature: &cfg.Temperature,
		Stream:      false, // 非流式，QQ 消息需要完整回复
	}

	a := llmagent.New("kp",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(cfg.SystemPrompt),
		llmagent.WithTools(tools),
		llmagent.WithGenerationConfig(genConfig),
	)

	// 4. 创建 Runner（管理会话和执行）
	r := runner.NewRunner("qq-ai-trpg-bot", a)

	kp := &KPAgent{
		config:     cfg,
		agent:      a,
		runner:     r,
		tools:      tools,
		sessionMgr: sessionMgr,
	}

	log.Printf("[KPAgent] 初始化完成, provider=%s, model=%s, base_url=%s",
		cfg.LLMProvider, cfg.LLMModel, cfg.LLMBaseURL)

	return kp, nil
}

// AgentID 实现 core.AgentHandler 接口。
func (a *KPAgent) AgentID() string {
	return "kp"
}

// Chat 实现 core.AgentHandler 接口。
// 处理玩家对话，通过 trpc-agent-go Runner 执行 AI 推理。
// trpc-agent-go 内部维护 session 级别的对话历史，无需手动管理。
func (a *KPAgent) Chat(ctx *core.MessageContext, session *core.Session) (string, error) {
	// 构建游戏上下文提示，拼接到用户消息前
	gameContext := a.buildGameContext(session)

	var userMessage string
	if gameContext != "" {
		userMessage = fmt.Sprintf("%s\n\n玩家: %s", gameContext, ctx.Content)
	} else {
		userMessage = ctx.Content
	}

	// 注入 sessionID 到 context，供 FunctionTool 读取
	agentCtx := withSessionID(ctx.Ctx, ctx.SessionID)

	// 调用 trpc-agent-go Runner 执行对话
	// runner 内部通过 user-id + session-id 维护对话历史
	events, err := a.runner.Run(agentCtx,
		ctx.UserID,    // user-id
		ctx.SessionID, // session-id
		model.NewUserMessage(userMessage),
	)
	if err != nil {
		return "", fmt.Errorf("Agent 执行失败: %w", err)
	}

	// 收集事件流中的回复内容
	var replyBuilder strings.Builder
	for event := range events {
		if event.Object == "chat.completion.chunk" {
			if len(event.Response.Choices) > 0 {
				replyBuilder.WriteString(event.Response.Choices[0].Delta.Content)
			}
		} else if event.Object == "chat.completion" {
			if len(event.Response.Choices) > 0 {
				replyBuilder.WriteString(event.Response.Choices[0].Message.Content)
			}
		}
	}

	reply := replyBuilder.String()
	if reply == "" {
		reply = "（KP 沉思中...未生成回复）"
	}

	log.Printf("[KPAgent] 会话 %s, 用户 %s: %s -> %s",
		ctx.SessionID, ctx.UserID, truncate(ctx.Content, 50), truncate(reply, 100))

	return reply, nil
}

// buildGameContext 从 Session 构建游戏上下文描述，注入到 AI 的 prompt 中。
// 这是 Agent 与 Handler 层联动的关键：Handler 层维护的游戏状态通过 Session 传递给 Agent。
func (a *KPAgent) buildGameContext(session *core.Session) string {
	session.RLock()
	defer session.RUnlock()

	var sb strings.Builder
	hasContext := false

	if module, ok := session.Get("current_module"); ok {
		sb.WriteString(fmt.Sprintf("当前模组: %v\n", module))
		hasContext = true
	}
	if scene, ok := session.Get("current_scene"); ok {
		sb.WriteString(fmt.Sprintf("当前场景: %v\n", scene))
		hasContext = true
	}
	if lastRoll, ok := session.Get("last_dice_result"); ok {
		sb.WriteString(fmt.Sprintf("最近骰点: %v\n", lastRoll))
		hasContext = true
	}
	if chars, ok := session.Get("characters"); ok {
		sb.WriteString(fmt.Sprintf("角色信息: %v\n", chars))
		hasContext = true
	}

	if !hasContext {
		return ""
	}

	return "【游戏上下文】\n" + sb.String()
}

// truncate 截断字符串到指定长度。
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
