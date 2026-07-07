package agent

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// KPAgent is the KP/DM AI Agent.
// It uses trpc-agent-go's LLMAgent with DeepSeek model.
// Game state is accessed through Service and shared via core.Session.
type KPAgent struct {
	config     *Config
	agent      agent.Agent
	runner     runner.Runner
	tools      []tool.Tool
	sessionMgr *core.SessionManager
	svc        *trpg.Service
}

// NewKPAgent creates a KP/DM agent.
func NewKPAgent(cfg *Config, sessionMgr *core.SessionManager, svc *trpg.Service) (*KPAgent, error) {
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

	// Set environment variables for trpc-agent-go's openai model
	if cfg.LLMAPIKey != "" {
		os.Setenv("OPENAI_API_KEY", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "" {
		os.Setenv("OPENAI_BASE_URL", cfg.LLMBaseURL)
	}

	// 1. Create DeepSeek model instance
	modelOpts := []openai.Option{
		openai.WithVariant(openai.VariantDeepSeek),
	}
	modelInstance := openai.New(cfg.LLMModel, modelOpts...)

	// 2. Create TRPG tools (KP-relevant only)
	tools := NewKPTools(sessionMgr, svc)

	// 3. Create LLMAgent
	genConfig := model.GenerationConfig{
		MaxTokens:   &cfg.MaxTokens,
		Temperature: &cfg.Temperature,
		Stream:      false,
	}

	a := llmagent.New("kp",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(cfg.SystemPrompt),
		llmagent.WithTools(tools),
		llmagent.WithGenerationConfig(genConfig),
	)

	// 4. Create Runner
	r := runner.NewRunner("qq-ai-trpg-bot", a)

	kp := &KPAgent{
		config:     cfg,
		agent:      a,
		runner:     r,
		tools:      tools,
		sessionMgr: sessionMgr,
		svc:        svc,
	}

	log.Printf("[KPAgent] 初始化完成, provider=%s, model=%s, base_url=%s, tools=%d",
		cfg.LLMProvider, cfg.LLMModel, cfg.LLMBaseURL, len(tools))

	return kp, nil
}

// AgentID implements core.AgentHandler.
func (a *KPAgent) AgentID() string {
	return "kp"
}

// Chat implements core.AgentHandler.
func (a *KPAgent) Chat(ctx *core.MessageContext, session *core.Session) (string, error) {
	// Build game context prompt
	gameContext := a.buildGameContext(ctx.SessionID, ctx.UserID)

	var userMessage string
	if gameContext != "" {
		userMessage = fmt.Sprintf("%s\n\n玩家: %s", gameContext, ctx.Content)
	} else {
		userMessage = ctx.Content
	}

	// Inject sessionID and userID into context for FunctionTools
	agentCtx := withSessionID(ctx.Ctx, ctx.SessionID)
	agentCtx = withUserID(agentCtx, ctx.UserID)

	// Run AI agent
	events, err := a.runner.Run(agentCtx,
		ctx.UserID,
		ctx.SessionID,
		model.NewUserMessage(userMessage),
	)
	if err != nil {
		return "", fmt.Errorf("Agent 执行失败: %w", err)
	}

	// Collect reply from event stream
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

// buildGameContext builds a game context description from the engine and session.
// This is injected into the AI's prompt to provide awareness of game state.
func (a *KPAgent) buildGameContext(sessionID, userID string) string {
	var sb strings.Builder
	hasContext := false

	// Ruleset
	rs := a.svc.GetRuleSet(sessionID)
	if rs != nil {
		rsLabel := "CoC 7版"
		if rs.Name() == "dnd5e" {
			rsLabel = "DnD 5e"
		}
		sb.WriteString(fmt.Sprintf("规则集: %s\n", rsLabel))
		hasContext = true
	}

	// Active character
	card := a.svc.GetActiveCharacter(sessionID, userID)
	if card != nil {
		sb.WriteString(fmt.Sprintf("玩家角色: %s (%s)\n", card.Name, card.System))
		// Show key stats
		if len(card.Attrs) > 0 {
			sb.WriteString("属性: ")
			first := true
			for k, v := range card.Attrs {
				if !first {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s=%d", k, v))
				first = false
			}
			sb.WriteString("\n")
		}
		if len(card.Skills) > 0 && rs != nil && rs.Name() == "coc7" {
			// Only show skills for CoC (too many to list otherwise)
			sb.WriteString("技能(前10): ")
			count := 0
			for k, v := range card.Skills {
				if count >= 10 {
					sb.WriteString("...")
					break
				}
				if count > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s=%d", k, v))
				count++
			}
			sb.WriteString("\n")
		}
		if len(card.Status) > 0 {
			sb.WriteString("状态: ")
			first := true
			for k, v := range card.Status {
				if !first {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s=%d", k, v))
				first = false
			}
			sb.WriteString("\n")
		}
		hasContext = true
	}

	// Last dice result from core.Session
	if a.sessionMgr != nil {
		session := a.sessionMgr.GetSession(sessionID)
		if lastRoll, ok := session.Get("last_dice_result"); ok {
			sb.WriteString(fmt.Sprintf("最近骰点: %v\n", lastRoll))
			hasContext = true
		}
		if lastCheck, ok := session.Get("last_check_result"); ok {
			sb.WriteString(fmt.Sprintf("最近检定: %v\n", lastCheck))
			hasContext = true
		}
	}

	// Initiative list (DnD)
	initList := a.svc.GetInitList(sessionID)
	if len(initList) > 0 {
		sb.WriteString("先攻列表: ")
		first := true
		for name, val := range initList {
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s(%d)", name, val))
			first = false
		}
		sb.WriteString("\n")
		hasContext = true
	}

	if !hasContext {
		return ""
	}

	return "【游戏上下文】\n" + sb.String()
}

// truncate truncates a string to maxLen runes.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
