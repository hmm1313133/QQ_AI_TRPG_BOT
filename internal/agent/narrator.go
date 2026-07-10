// Package agent - Narrator 叙事层。
//
// Narrator 是输出层，接收 DecisionDirective 作为约束，结合 GameState
// 和剧本上下文，使用工具（骰子/检定/NPC查询等）生成沉浸式叙事文本。
//
// 复用现有 KPAgent 的 Agent + Tools 模式：
//   - llmagent.New + WithModel + WithInstruction + WithTools + WithGenerationConfig
//   - 保留所有现有工具（roll_dice, skill_check, san_check, get_character, ...）
//   - 温度 0.7 兼顾创造性和可控性
package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"

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
	svc        *trpg.Service
	scriptDeps *ScriptDeps
}

// NewNarrator 创建叙事层 Agent。
func NewNarrator(
	cfg *Config,
	sessionMgr *core.SessionManager,
	svc *trpg.Service,
	scriptDeps *ScriptDeps,
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

	// 2. Create tools (KP tools + script tools)
	tools := NewKPTools(sessionMgr, svc)
	if scriptDeps != nil {
		tools = append(tools, NewScriptTools(scriptDeps)...)
	}

	// 3. Create LLMAgent with Narrator system prompt
	maxTokens := narratorMaxTokens
	temp := narratorTemp

	genConfig := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &temp,
		Stream:      false,
	}

	// 初始系统提示词（后续每轮会根据 GameState 动态更新）
	a := llmagent.New("narrator",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(narratorSystemPromptBase),
		llmagent.WithTools(tools),
		llmagent.WithGenerationConfig(genConfig),
	)

	r := runner.NewRunner("trpg-narrator", a)

	n := &Narrator{
		config:     cfg,
		agent:      a,
		runner:     r,
		tools:      tools,
		sessionMgr: sessionMgr,
		svc:        svc,
		scriptDeps: scriptDeps,
	}

	log.Printf("[Narrator] 初始化完成, model=%s, temp=%.1f, maxTokens=%d, tools=%d",
		cfg.LLMModel, temp, maxTokens, len(tools))

	return n, nil
}

// Narrate 生成叙事文本。
// 注入 DecisionDirective 到用户消息，使用工具进行骰子检定等操作。
func (n *Narrator) Narrate(
	ctx context.Context,
	state *GameState,
	directive *DecisionDirective,
	gameContext string,
	playerMessage string,
	sessionID string,
	userID string,
) (string, error) {
	start := time.Now()

	// 构建用户消息：导演指令 + 游戏上下文 + 玩家消息
	userMessage := buildNarratorUserMessage(directive, gameContext, playerMessage)

	// Inject sessionID and userID into context for FunctionTools
	agentCtx := withSessionID(ctx, sessionID)
	agentCtx = withUserID(agentCtx, userID)

	// Run AI agent
	events, err := n.runner.Run(agentCtx,
		userID,
		sessionID,
		model.NewUserMessage(userMessage),
	)
	if err != nil {
		return "", fmt.Errorf("Narrator 执行失败: %w", err)
	}

	// Collect reply
	reply := collectAgentReply(events)
	if reply == "" {
		reply = "（KP 沉思中...未生成回复）"
	}

	elapsed := time.Since(start)
	log.Printf("[Narrator] 叙事完成 (%.1fs): %s -> %s",
		elapsed.Seconds(), truncate(playerMessage, 50), truncate(reply, 100))

	return reply, nil
}

// buildGameContext 构建游戏上下文（复用现有 KPAgent 的逻辑）。
func (n *Narrator) buildGameContext(sessionID, userID string) string {
	var sb strings.Builder
	hasContext := false

	// Ruleset
	rs := n.svc.GetRuleSet(sessionID)
	if rs != nil {
		rsLabel := "CoC 7版"
		if rs.Name() == "dnd5e" {
			rsLabel = "DnD 5e"
		}
		sb.WriteString(fmt.Sprintf("规则集: %s\n", rsLabel))
		hasContext = true
	}

	// Active character
	card := n.svc.GetActiveCharacter(sessionID, userID)
	if card != nil {
		sb.WriteString(fmt.Sprintf("玩家角色: %s (%s)\n", card.Name, card.System))
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

	// Last dice result
	if n.sessionMgr != nil {
		session := n.sessionMgr.GetSession(sessionID)
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
	initList := n.svc.GetInitList(sessionID)
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

	// Script context (ProgressTracker)
	if n.scriptDeps != nil && n.scriptDeps.ProgressTracker != nil {
		scriptCtx := n.scriptDeps.ProgressTracker.GetContextForKP(sessionID)
		if scriptCtx != "" {
			sb.WriteString(scriptCtx)
			hasContext = true
		}

		if n.sessionMgr != nil {
			session := n.sessionMgr.GetSession(sessionID)
			if prompt, ok := session.Get("timeline_prompt"); ok {
				sb.WriteString(fmt.Sprintf("\n【时间轴提示】%v\n", prompt))
				session.Set("timeline_prompt", nil)
			}
		}
	}

	if !hasContext {
		return ""
	}

	return "【游戏上下文】\n" + sb.String()
}
