package agent

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// KPAgent is the AI host Agent.
// It uses trpc-agent-go's LLMAgent with DeepSeek model.
//
// 多层架构：Chat() 委托给 TurnEngine（规则指导 + 低频 Planner + Narrator）。
type KPAgent struct {
	config       *Config
	agent        agent.Agent
	runner       runner.Runner
	tools        []tool.Tool
	sessionMgr   *core.SessionManager
	turnEngine   *TurnEngine    // 回合引擎（可选，nil 时回退到单 agent 模式）
	personaStore *persona.Store // 全局默认人设存储（可为 nil）
}

// NewKPAgent creates the AI host agent.
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

	// 2. Create tools (仅 roll_dice)
	tools := []tool.Tool{NewRollDiceTool(sessionMgr)}

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
	r := runner.NewRunner("voyage-world-engine", a)

	kp := &KPAgent{
		config:     cfg,
		agent:      a,
		runner:     r,
		tools:      tools,
		sessionMgr: sessionMgr,
	}

	log.Printf("[KPAgent] 初始化完成, provider=%s, model=%s, base_url=%s, tools=%d",
		cfg.LLMProvider, cfg.LLMModel, cfg.LLMBaseURL, len(tools))

	return kp, nil
}

// SetTurnEngine 注入回合引擎（由 main.go 在装配阶段调用）。
func (a *KPAgent) SetTurnEngine(engine *TurnEngine) {
	a.turnEngine = engine
	log.Printf("[KPAgent] 回合引擎已注入")
}

// SetPersonaStore 注入全局人设存储（由 main.go 在装配阶段调用）。
func (a *KPAgent) SetPersonaStore(s *persona.Store) {
	a.personaStore = s
	log.Printf("[KPAgent] 玩家人设已接入")
}

// AgentID implements core.AgentHandler.
func (a *KPAgent) AgentID() string {
	return "kp"
}

// Chat implements core.AgentHandler.
// TurnEngine 是唯一正式回合入口；失败时必须向上返回，不能用无状态路径伪造成功。
func (a *KPAgent) Chat(ctx *core.MessageContext, session *core.Session) (string, error) {
	if a.turnEngine == nil {
		return a.chatSingleAgent(ctx)
	}
	reply, err := a.turnEngine.Run(ctx, session)
	if err != nil {
		return "", fmt.Errorf("回合引擎失败: %w", err)
	}
	log.Printf("[KPAgent] 会话 %s, 用户 %s: %s -> %s",
		ctx.SessionID, ctx.UserID, truncate(ctx.Content, 50), truncate(reply, 100))
	return reply, nil
}

// chatSingleAgent 是原有的单 agent 模式，作为 pipeline 的回退。
func (a *KPAgent) chatSingleAgent(ctx *core.MessageContext) (string, error) {
	// Build game context prompt
	gameContext := a.buildGameContext(ctx.SessionID)

	var userMessage string
	if gameContext != "" {
		userMessage = fmt.Sprintf("%s\n\n玩家: %s", gameContext, ctx.Content)
	} else {
		userMessage = ctx.Content
	}
	// 全局默认人设注入（回退路径无世界覆盖概念）
	if a.personaStore != nil {
		if p, err := a.personaStore.Get(ctx.UserID); err == nil {
			if block := buildPersonaBlock(p); block != "" {
				userMessage = block + "\n" + userMessage
			}
		}
	}
	// 会话级回复长度偏好注入（与 TurnEngine 路径同一机制）
	if hint := LengthHintFromSession(a.sessionMgr.GetSession(ctx.SessionID)); hint != "" {
		userMessage += "\n" + hint
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

	// 剥离 CoT 思考段（未启用 CoT 时是无害 no-op）
	reply := StripThinking(replyBuilder.String())
	if reply == "" {
		reply = "（世界静默了片刻……请重新尝试）"
	}

	log.Printf("[KPAgent] 会话 %s, 用户 %s: %s -> %s",
		ctx.SessionID, ctx.UserID, truncate(ctx.Content, 50), truncate(reply, 100))

	return reply, nil
}

// buildGameContext 构建会话级上下文（最近骰点记账）。
func (a *KPAgent) buildGameContext(sessionID string) string {
	if a.sessionMgr == nil {
		return ""
	}
	session := a.sessionMgr.GetSession(sessionID)
	lastRoll, ok := session.Get("last_dice_result")
	at, atOK := session.Get("last_dice_at")
	stamp, stampOK := at.(int64)
	if !ok || !atOK || !stampOK || time.Since(time.Unix(stamp, 0)) > 2*time.Minute {
		return ""
	}
	return fmt.Sprintf("【游戏上下文】\n最近骰点: %v\n", lastRoll)
}

// truncate truncates a string to maxLen runes.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
