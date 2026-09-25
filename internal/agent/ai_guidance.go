// Package agent - AI 叙事指导器（AIGuidance）。
//
// 替代规则化 RuleGuidance 的每轮指导：用低成本 LLM 基于真实叙事上下文
// （场景/NPC/任务/最近对话/玩家最新行动）生成下一回合的叙事指导，
// 并可输出 state_updates 回写世界状态，让规则指标能感知叙事变化（闭环）。
//
// 容错套路与 Director 一致：超时 / 调用失败 / 解析失败一律降级为
// BuildGuidance 规则版，不阻塞回合。
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

const (
	// guidanceTimeout 单次指导调用的超时（超时后降级规则版，不阻塞叙事）。
	// 注意 DeepSeek 响应延迟波动大（实测 guidance 成功约 3.5-4s、narrator
	// 常态 5-9s），5s 超时曾导致约六成调用被掐断，故放宽到 10s。
	guidanceTimeout = 10 * time.Second
	// guidanceMaxTokens 指导输出很小（JSON），压低 token 上限控制成本。
	guidanceMaxTokens = 800
	// guidanceTemperature 低温度保证指导稳定。
	guidanceTemperature = 0.3
	// guidanceRecentTurns 注入的最近对话轮数。
	guidanceRecentTurns = 4
	// guidanceTurnChars 每轮对话截断字符数。
	guidanceTurnChars = 200
	// guidanceSummaryEntries 注入的最近阶段摘要条数（补长期记忆，防指导"近视"）。
	guidanceSummaryEntries = 2
	// guidanceSummaryChars 每条摘要截断字符数。
	guidanceSummaryChars = 300
	// guidanceMaxUpdates 单轮状态更新条数上限（防幻觉批量改状态）。
	guidanceMaxUpdates = 5
)

// guidanceAllowedUpdateTypes AI 指导允许输出的状态更新类型（与 Planner 同一套约束）。
var guidanceAllowedUpdateTypes = map[string]bool{
	"npc_disposition":     true,
	"hidden_discovered":   true,
	"event_triggered":     true,
	"objective_completed": true,
}

// GuidanceOutcome 一轮 AI 指导的结果（含可视化元数据）。
type GuidanceOutcome struct {
	Guidance     *RuleGuidance // 注入 Narrator 的指导（AI 或规则兜底）
	StateUpdates []StateUpdate // 已通过校验的状态更新（由 TurnEngine 回写）
	Source       string        // "ai" / "fallback"
	ElapsedMs    int64
	Err          string // 降级原因（AI 成功时为空）
}

// AIGuidance 是每轮的 AI 叙事指导器（无状态会话，历史不累积）。
type AIGuidance struct {
	agent  *llmagent.LLMAgent
	runner runner.Runner
}

// NewAIGuidance 创建 AI 叙事指导器（复用主 LLM 配置，低温度小输出）。
func NewAIGuidance(cfg *Config) (*AIGuidance, error) {
	maxTokens := guidanceMaxTokens
	temp := guidanceTemperature

	modelInstance := openai.New(cfg.LLMModel,
		openai.WithVariant(openai.VariantDeepSeek),
		// JSON 模式：指导输出固定为 JSON（DeepSeek 支持 response_format=json_object）。
		openai.WithExtraFields(map[string]any{
			"response_format": map[string]any{"type": "json_object"},
		}),
	)

	a := llmagent.New("guidance",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(guidanceSystemPrompt),
		llmagent.WithGenerationConfig(model.GenerationConfig{
			MaxTokens:   &maxTokens,
			Temperature: &temp,
			Stream:      false,
		}),
	)

	log.Printf("[AIGuidance] 初始化完成, model=%s, temp=%.1f, maxTokens=%d",
		cfg.LLMModel, temp, maxTokens)

	return &AIGuidance{
		agent:  a,
		runner: runner.NewRunner("voyage-guidance", a),
	}, nil
}

// Guide 生成下一轮叙事指导。任何失败都降级为规则版，永不返回 nil。
func (g *AIGuidance) Guide(
	ctx context.Context,
	state *world.WorldState,
	playerMessage string,
	sessionID string,
) *GuidanceOutcome {
	start := time.Now()
	out := &GuidanceOutcome{Source: "ai"}

	userMsg := buildGuidanceUserMessage(state, playerMessage)

	callCtx, cancel := context.WithTimeout(ctx, guidanceTimeout)
	defer cancel()

	events, err := g.runner.Run(callCtx,
		"guidance",
		fmt.Sprintf("%s-guide-%d", sessionID, time.Now().UnixNano()),
		model.NewUserMessage(userMsg),
	)
	if err != nil {
		out.Err = fmt.Sprintf("LLM 调用失败: %v", err)
		logLLMInteraction(llmLogRecord{
			Kind: "guidance", Session: sessionID,
			SystemPrompt: guidanceSystemPrompt, UserMessage: userMsg,
			Error: err.Error(), ElapsedMs: time.Since(start).Milliseconds(),
		})
		return g.fallback(state, out, start)
	}

	reply := collectAgentReply(events)
	logLLMInteraction(llmLogRecord{
		Kind: "guidance", Session: sessionID,
		SystemPrompt: guidanceSystemPrompt, UserMessage: userMsg,
		Reply: reply, ElapsedMs: time.Since(start).Milliseconds(),
	})
	if reply == "" {
		// runner 在 context 取消时不返回 error，仅关闭事件通道——
		// 需显式区分超时与真正的空回复，否则日志误导排查
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			out.Err = fmt.Sprintf("LLM 调用超时（>%s）", guidanceTimeout)
		} else {
			out.Err = "LLM 返回空回复"
		}
		return g.fallback(state, out, start)
	}

	jsonStr := extractAgentJSON(reply)
	if jsonStr == "" {
		out.Err = "LLM 输出无 JSON"
		return g.fallback(state, out, start)
	}

	var parsed aiGuidanceReply
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		out.Err = fmt.Sprintf("JSON 解析失败: %v", err)
		return g.fallback(state, out, start)
	}

	out.Guidance = parsed.toRuleGuidance()
	out.StateUpdates = sanitizeGuidanceUpdates(parsed.StateUpdates)
	out.ElapsedMs = time.Since(start).Milliseconds()
	return out
}

// fallback 降级为规则化指导（保留降级原因供可视化）。
func (g *AIGuidance) fallback(state *world.WorldState, out *GuidanceOutcome, start time.Time) *GuidanceOutcome {
	log.Printf("[AIGuidance] %s，降级为规则指导", out.Err)
	out.Guidance = BuildGuidance(state)
	out.StateUpdates = nil
	out.Source = "fallback"
	out.ElapsedMs = time.Since(start).Milliseconds()
	return out
}

// aiGuidanceReply 是 AI 指导的 JSON 输出结构。
type aiGuidanceReply struct {
	Tone         string        `json:"tone"`
	Pacing       string        `json:"pacing"`
	Focus        []string      `json:"focus"`
	Constraints  []string      `json:"constraints"`
	StateUpdates []StateUpdate `json:"state_updates"`
}

// toRuleGuidance 转换为注入 Narrator 的 RuleGuidance（字段清洗+默认值）。
func (r *aiGuidanceReply) toRuleGuidance() *RuleGuidance {
	g := &RuleGuidance{
		Tone:        strings.TrimSpace(r.Tone),
		Pacing:      strings.TrimSpace(r.Pacing),
		Focus:       cleanStrings(r.Focus),
		Constraints: cleanStrings(r.Constraints),
	}
	if g.Tone == "" {
		g.Tone = "正常"
	}
	switch g.Pacing {
	case "slow", "medium", "fast":
	default:
		g.Pacing = "medium"
	}
	return g
}

// sanitizeGuidanceUpdates 校验状态更新：类型白名单 + target 非空 + 数量上限。
func sanitizeGuidanceUpdates(updates []StateUpdate) []StateUpdate {
	out := make([]StateUpdate, 0, len(updates))
	for _, u := range updates {
		if !guidanceAllowedUpdateTypes[u.Type] {
			continue
		}
		u.Target = strings.TrimSpace(u.Target)
		if u.Target == "" {
			continue
		}
		out = append(out, u)
		if len(out) >= guidanceMaxUpdates {
			break
		}
	}
	return out
}

// cleanStrings 去除空白条目。
func cleanStrings(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// buildGuidanceUserMessage 组装指导器的用户消息：让世界状态与近期叙事
// 同时可见，使指导能针对玩家最新行动做出反应（规则版做不到的叙事感知）。
func buildGuidanceUserMessage(state *world.WorldState, playerMessage string) string {
	var sb strings.Builder

	sb.WriteString("【场景】\n")
	sb.WriteString(fmt.Sprintf("节点: %s（%s）| 危险等级: %s\n",
		state.Scene.NodeName, state.Scene.NodeType, state.Scene.DangerLevel))
	if state.Scene.Description != "" {
		sb.WriteString("描述: " + truncate(state.Scene.Description, 200) + "\n")
	}

	m := state.Metrics
	sb.WriteString(fmt.Sprintf("\n【规则指标】张力 %d | 混乱 %d | 掌控权 %d | 目标进度 %d | 轮次 %d\n",
		m.TensionLevel, m.ChaosLevel, m.PlayerAgency, m.ObjectiveProgress, state.RoundCount))

	if len(state.Characters) > 0 {
		sb.WriteString("\n【在场 NPC】\n")
		for _, npc := range state.Characters {
			alive := "存活"
			if !npc.Alive {
				alive = "死亡"
			}
			sb.WriteString(fmt.Sprintf("- %s（态度 %s，%s）\n", npc.Name, npc.Disposition, alive))
		}
	}

	if len(state.Quests) > 0 {
		sb.WriteString("\n【任务目标】\n")
		for _, q := range state.Quests {
			mark := "[ ]"
			if q.Completed {
				mark = "[x]"
			}
			sb.WriteString(fmt.Sprintf("- %s %s\n", mark, q.Description))
		}
	}

	pending := make([]string, 0)
	for _, ev := range state.EventQueue {
		if !ev.Triggered && !ev.Resolved {
			pending = append(pending, ev.Description)
		}
	}
	if len(pending) > 0 {
		sb.WriteString("\n【待触发事件】\n")
		for _, d := range pending {
			sb.WriteString("- " + d + "\n")
		}
	}

	hidden := make([]string, 0)
	for _, h := range state.HiddenInfo {
		if !h.Discovered {
			hidden = append(hidden, h.Description)
		}
	}
	if len(hidden) > 0 {
		sb.WriteString("\n【未发现线索】\n")
		for _, d := range hidden {
			sb.WriteString("- " + d + "\n")
		}
	}

	if state.ScenePlan != "" {
		sb.WriteString("\n【当前场景计划】\n" + truncate(state.ScenePlan, 500) + "\n")
	}

	// 剧情前情：注入最近阶段摘要，使指导具备长期记忆（避免只盯最近几轮）。
	if entries := state.SummaryEntries(); len(entries) > 0 {
		from := len(entries) - guidanceSummaryEntries
		if from < 0 {
			from = 0
		}
		sb.WriteString("\n【剧情前情】\n")
		for _, e := range entries[from:] {
			sb.WriteString("- " + truncate(e.Text, guidanceSummaryChars) + "\n")
		}
	}

	if n := len(state.RecentTurns); n > 0 {
		from := n - guidanceRecentTurns
		if from < 0 {
			from = 0
		}
		sb.WriteString("\n【最近对话】\n")
		for _, turn := range state.RecentTurns[from:] {
			sb.WriteString("玩家: " + truncate(turn.Player, guidanceTurnChars) + "\n")
			sb.WriteString("世界: " + truncate(turn.Narrator, guidanceTurnChars) + "\n")
		}
	}

	sb.WriteString("\n【玩家最新行动】\n" + playerMessage + "\n")
	return sb.String()
}
