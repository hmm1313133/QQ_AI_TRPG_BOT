// Package agent - Narrator 叙事层。
//
// Narrator 是输出层，接收 DecisionDirective 作为约束，结合世界状态
// 上下文，使用工具（roll_dice）生成沉浸式叙事文本。
//
// 直接调用 model.GenerateContent：上下文以标准 messages 结构发送
// （system 主提示词 + 设定资料 user 消息 + 近期对话 user/assistant
// 消息对 + 本轮玩家 user 消息），工具调用由本层的小循环处理。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// maxToolCallRounds 单轮叙事允许的最大工具调用轮数（防死循环）。
const maxToolCallRounds = 4

// Narrator 是叙事层 Agent。
//
// 不持有构建好的 llmagent：主提示词按世界预设/自定义每轮不同，
// 每次 NarrateMessage 用当轮解析出的系统提示词现场构建 agent + runner
// （纯内存构建、无 I/O；Narrator 本来就是每轮独立的无状态会话）。
type Narrator struct {
	config     *Config
	model      model.Model
	tools      []tool.Tool
	genConfig  model.GenerationConfig
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

	// 3. 生成配置（系统提示词每轮动态解析，见 NarrateMessage）
	maxTokens := narratorMaxTokens
	temp := narratorTemp

	genConfig := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &temp,
		Stream:      false,
	}

	n := &Narrator{
		config:     cfg,
		model:      modelInstance,
		tools:      tools,
		genConfig:  genConfig,
		sessionMgr: sessionMgr,
	}

	log.Printf("[Narrator] 初始化完成, model=%s, temp=%.1f, maxTokens=%d, tools=%d",
		cfg.LLMModel, temp, maxTokens, len(tools))

	return n, nil
}

// NarrateMessage 生成叙事文本（无状态调用）。
//
// systemPrompt 为当轮解析出的系统提示词（ResolveMainPrompt：预设/自定义 +
// 技术契约段）；input 为 ContextBuilder 组装的结构化上下文。请求以标准
// messages 结构发送：system 主提示词 → 设定资料（单条 user）→ 近期对话
// （user/assistant 消息对）→ 本轮玩家消息（user，尾部拼接指令区）。
// 跨轮一致性由 WorldState 与 ContextBuilder 组装的上下文承载，不依赖
// 框架会话历史（旧版会话历史无限累积，是长团 token 爆炸的根因，
// 见 AI多层架构分析.md 问题 6）。
func (n *Narrator) NarrateMessage(
	ctx context.Context,
	systemPrompt string,
	input *NarratorInput,
	sessionID string,
	userID string,
) (string, error) {
	start := time.Now()

	// Inject sessionID and userID into context for FunctionTools
	agentCtx := withSessionID(ctx, sessionID)
	agentCtx = withUserID(agentCtx, userID)

	messages := buildNarratorMessages(systemPrompt, input)
	rawReply, messages, err := n.generate(agentCtx, messages)
	if err != nil {
		logLLMInteraction(llmLogRecord{
			Kind: "narrator", Session: sessionID,
			SystemPrompt: systemPrompt, Messages: llmLogMessages(messages),
			Error: err.Error(), ElapsedMs: time.Since(start).Milliseconds(),
		})
		return "", fmt.Errorf("Narrator 执行失败: %w", err)
	}

	// 剥离 CoT 思考段；未启用 CoT 时是无害 no-op
	reply := StripThinking(rawReply)
	if reply == "" {
		reply = "（世界静默了片刻……请重新尝试）"
	}

	elapsed := time.Since(start)
	dbgRec := llmLogRecord{
		Kind: "narrator", Session: sessionID,
		SystemPrompt: systemPrompt, Messages: llmLogMessages(messages),
		Reply: reply, ElapsedMs: elapsed.Milliseconds(),
	}
	if rawReply != reply {
		dbgRec.RawReply = rawReply
	}
	logLLMInteraction(dbgRec)

	log.Printf("[Narrator] 叙事完成 (%.1fs): %s -> %s",
		elapsed.Seconds(), truncate(input.PlayerMessage, 50), truncate(reply, 100))

	return reply, nil
}

// buildNarratorMessages 把结构化输入组装为标准 messages 列表。
func buildNarratorMessages(systemPrompt string, input *NarratorInput) []model.Message {
	msgs := make([]model.Message, 0, 4+2*len(input.History))
	msgs = append(msgs, model.Message{Role: model.RoleSystem, Content: systemPrompt})
	if strings.TrimSpace(input.ContextBlock) != "" {
		msgs = append(msgs, model.Message{Role: model.RoleUser, Content: input.ContextBlock})
	}
	// 近期对话：真实 user/assistant 消息对（玩家消息可能为空——如开场叙述轮，跳过）
	for _, turn := range input.History {
		if strings.TrimSpace(turn.Player) != "" {
			msgs = append(msgs, model.Message{Role: model.RoleUser, Content: turn.Player})
		}
		if strings.TrimSpace(turn.Narrator) != "" {
			msgs = append(msgs, model.Message{Role: model.RoleAssistant, Content: turn.Narrator})
		}
	}
	final := input.PlayerMessage
	if tail := strings.TrimSpace(input.Tail); tail != "" {
		final += "\n\n" + input.Tail
	}
	// 自动推进/开场轮可能出现空玩家消息；空 content 的 user 消息会被部分
	// OpenAI 兼容网关直接判 400，这里兜底为一句中性推进指令。
	if strings.TrimSpace(final) == "" {
		final = "（继续）"
	}
	msgs = append(msgs, model.Message{Role: model.RoleUser, Content: final})
	return msgs
}

// generate 调模型生成回复，含工具调用循环（roll_dice）。
// 返回最终文本与完整消息列表（含工具往返，供调试日志记录）。
func (n *Narrator) generate(ctx context.Context, messages []model.Message) (string, []model.Message, error) {
	toolSet := make(map[string]tool.Tool, len(n.tools))
	for _, t := range n.tools {
		toolSet[t.Declaration().Name] = t
	}

	for round := 0; ; round++ {
		req := &model.Request{
			Messages:         messages,
			GenerationConfig: n.genConfig,
			Tools:            toolSet,
		}
		ch, err := n.model.GenerateContent(ctx, req)
		if err != nil {
			return "", messages, err
		}
		msg, err := collectModelMessage(ch)
		if err != nil {
			return "", messages, err
		}
		if len(msg.ToolCalls) == 0 {
			return msg.Content, messages, nil
		}
		if round >= maxToolCallRounds {
			log.Printf("[Narrator] 工具调用超过 %d 轮，强制收尾", maxToolCallRounds)
			return msg.Content, messages, nil
		}
		log.Printf("[Narrator] 工具调用: %d 个（第 %d 轮）", len(msg.ToolCalls), round+1)
		messages = append(messages, model.Message{
			Role: model.RoleAssistant, Content: msg.Content, ToolCalls: msg.ToolCalls,
		})
		for _, tc := range msg.ToolCalls {
			messages = append(messages, n.callTool(ctx, toolSet, tc))
		}
	}
}

// callTool 执行一次工具调用，返回 tool 角色的结果消息（出错也以错误内容回传，让模型自行补救）。
func (n *Narrator) callTool(ctx context.Context, toolSet map[string]tool.Tool, tc model.ToolCall) model.Message {
	name := tc.Function.Name
	out := model.Message{Role: model.RoleTool, ToolID: tc.ID, ToolName: name}

	t, ok := toolSet[name]
	if !ok {
		out.Content = fmt.Sprintf(`{"error": "未知工具 %s"}`, name)
		return out
	}
	ct, ok := t.(tool.CallableTool)
	if !ok {
		out.Content = fmt.Sprintf(`{"error": "工具 %s 不可调用"}`, name)
		return out
	}
	result, err := ct.Call(ctx, tc.Function.Arguments)
	if err != nil {
		out.Content = fmt.Sprintf(`{"error": %q}`, err.Error())
		return out
	}
	data, err := json.Marshal(result)
	if err != nil {
		out.Content = fmt.Sprintf(`{"error": "工具结果序列化失败: %v"}`, err)
		return out
	}
	out.Content = string(data)
	return out
}

// collectModelMessage 从响应通道收集最终的 assistant 消息（非流式单条；流式则累积 delta）。
func collectModelMessage(ch <-chan *model.Response) (model.Message, error) {
	var full model.Message
	var content strings.Builder
	var retErr error
	for resp := range ch {
		if resp == nil {
			continue
		}
		if resp.Error != nil {
			retErr = fmt.Errorf("LLM API 错误: %s", resp.Error.Message)
			continue
		}
		if len(resp.Choices) == 0 {
			continue
		}
		c := resp.Choices[0]
		if resp.Object == "chat.completion" {
			full = c.Message
		} else {
			content.WriteString(c.Delta.Content)
		}
	}
	if retErr != nil {
		return model.Message{}, retErr
	}
	if full.Role == "" && content.Len() > 0 {
		full = model.Message{Role: model.RoleAssistant, Content: content.String()}
	}
	return full, nil
}

// buildGameContext 构建会话级上下文（最近骰点记账）。
func (n *Narrator) buildGameContext(sessionID string) string {
	if n.sessionMgr == nil {
		return ""
	}
	session := n.sessionMgr.GetSession(sessionID)
	lastRoll, ok := session.Get("last_dice_result")
	at, atOK := session.Get("last_dice_at")
	if !ok || !atOK {
		return ""
	}
	stamp, ok := at.(int64)
	if !ok || time.Since(time.Unix(stamp, 0)) > 2*time.Minute {
		return ""
	}
	return fmt.Sprintf("【游戏上下文】\n最近骰点: %v\n", lastRoll)
}
