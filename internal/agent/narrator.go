// Package agent - Narrator 叙事层。
//
// Narrator 是输出层，接收 DecisionDirective 作为约束，结合世界状态
// 上下文，使用工具（roll_dice）生成沉浸式叙事文本。
//
// 复用 KPAgent 的 Agent + Tools 模式：
//   - llmagent.New + WithModel + WithInstruction + WithTools + WithGenerationConfig
//   - 温度 0.7 兼顾创造性和可控性
package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Narrator 是叙事层 Agent。
type Narrator struct {
	config     *Config
	agent      agent.Agent
	runner     runner.Runner
	tools      []tool.Tool
	sessionMgr *core.SessionManager
}

// NewNarrator 创建叙事层 Agent。
func NewNarrator(
	cfg *Config,
	sessionMgr *core.SessionManager,
) (*Narrator, error) {
	narratorTemp := cfg.NarratorTemperature
	if narratorTemp == 0 {
		narratorTemp = 0.7
	}
	narratorMaxTokens := cfg.NarratorMaxTokens
	if narratorMaxTokens == 0 {
		narratorMaxTokens = 4096
	}

	// Set environment variables for trpc-agent-go's openai model
	if cfg.LLMAPIKey != "" {
		os.Setenv("OPENAI_API_KEY", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "" {
		os.Setenv("OPENAI_BASE_URL", cfg.LLMBaseURL)
	}

	// 1. Create model instance
	modelOpts := []openai.Option{
		openai.WithVariant(openai.VariantDeepSeek),
	}
	modelInstance := openai.New(cfg.LLMModel, modelOpts...)

	// 2. Create tools (仅 roll_dice)
	tools := []tool.Tool{NewRollDiceTool(sessionMgr)}

	// 3. Create LLMAgent with Narrator system prompt
	maxTokens := narratorMaxTokens
	temp := narratorTemp

	genConfig := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &temp,
		Stream:      false,
	}

	// 初始系统提示词
	a := llmagent.New("narrator",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(narratorSystemPromptBase),
		llmagent.WithTools(tools),
		llmagent.WithGenerationConfig(genConfig),
	)

	r := runner.NewRunner("voyage-narrator", a)

	n := &Narrator{
		config:     cfg,
		agent:      a,
		runner:     r,
		tools:      tools,
		sessionMgr: sessionMgr,
	}

	log.Printf("[Narrator] 初始化完成, model=%s, temp=%.1f, maxTokens=%d, tools=%d",
		cfg.LLMModel, temp, maxTokens, len(tools))

	return n, nil
}

// NarrateMessage 生成叙事文本（无状态调用）。
//
// 每轮使用唯一的 runner 会话 ID：跨轮一致性由 WorldState 与 ContextBuilder
// 组装的上下文包承载，不依赖框架会话历史（旧版会话历史无限累积，
// 是长团 token 爆炸的根因，见 AI多层架构分析.md 问题 6）。
func (n *Narrator) NarrateMessage(
	ctx context.Context,
	userMessage string,
	sessionID string,
	userID string,
) (string, error) {
	start := time.Now()

	// Inject sessionID and userID into context for FunctionTools
	agentCtx := withSessionID(ctx, sessionID)
	agentCtx = withUserID(agentCtx, userID)

	// 无状态会话：每次调用独立会话，历史不跨轮累积
	runSessionID := fmt.Sprintf("%s-%d", sessionID, time.Now().UnixNano())

	// Run AI agent
	events, err := n.runner.Run(agentCtx,
		userID,
		runSessionID,
		model.NewUserMessage(userMessage),
	)
	if err != nil {
		return "", fmt.Errorf("Narrator 执行失败: %w", err)
	}

	// Collect reply（剥离 CoT 思考段；未启用 CoT 时是无害 no-op）
	reply := StripThinking(collectAgentReply(events))
	if reply == "" {
		reply = "（主持人沉思中...未生成回复）"
	}

	elapsed := time.Since(start)
	log.Printf("[Narrator] 叙事完成 (%.1fs): %s -> %s",
		elapsed.Seconds(), truncate(userMessage, 50), truncate(reply, 100))

	return reply, nil
}

// buildGameContext 构建会话级上下文（最近骰点记账）。
func (n *Narrator) buildGameContext(sessionID string) string {
	if n.sessionMgr == nil {
		return ""
	}
	session := n.sessionMgr.GetSession(sessionID)
	lastRoll, ok := session.Get("last_dice_result")
	if !ok {
		return ""
	}
	return fmt.Sprintf("【游戏上下文】\n最近骰点: %v\n", lastRoll)
}
