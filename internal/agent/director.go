// Package agent - Director 导演系统。
//
// Director 是决策层，负责读取 WorldState + 玩家输入，通过规则化指标评估
// + 低温度 LLM 调用，输出结构化决策指令（DecisionDirective）。
// （P2 起降级为低频 Planner 使用，不再跟随每条玩家消息。）
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// Director 是导演系统，负责决策层。
type Director struct {
	metrics  *MetricsEvaluator
	agent    *llmagent.LLMAgent
	runner   runner.Runner
}

// NewDirector 创建导演系统。
// modelInstance 和 openaiOpts 用于复用已有的 model 实例配置。
func NewDirector(
	cfg *Config,
	metrics *MetricsEvaluator,
) (*Director, error) {
	// Director 使用低温度保证决策一致性
	directorTemp := cfg.DirectorTemperature
	if directorTemp == 0 {
		directorTemp = 0.2
	}
	directorMaxTokens := cfg.DirectorMaxTokens
	if directorMaxTokens == 0 {
		directorMaxTokens = 2048
	}

	maxTokens := directorMaxTokens
	temp := directorTemp

	modelOpts := []openai.Option{
		openai.WithVariant(openai.VariantDeepSeek),
	}
	modelInstance := openai.New(cfg.LLMModel, modelOpts...)

	genConfig := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &temp,
		Stream:      false,
	}

	a := llmagent.New("director",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(directorSystemPrompt),
		llmagent.WithGenerationConfig(genConfig),
	)

	r := runner.NewRunner("trpg-director", a)

	log.Printf("[Director] 初始化完成, model=%s, temp=%.1f, maxTokens=%d",
		cfg.LLMModel, temp, maxTokens)

	return &Director{
		metrics: metrics,
		agent:   a,
		runner:  r,
	}, nil
}

// Decide 做出下一轮决策。
// 流程：预评估指标 -> LLM 调用 -> 解析 DecisionDirective
// LLM 失败时降级为规则化基础指令。
func (d *Director) Decide(
	ctx context.Context,
	state *world.WorldState,
	playerMessage string,
	scriptContext string,
	sessionID string,
) (*DecisionDirective, error) {
	start := time.Now()

	// 1. 规则化预评估
	state.Metrics = d.metrics.Evaluate(state, sessionID)
	log.Printf("[Director] 预评估: scene=%s, round=%d, metrics=T%d/C%d/A%d/P%d",
		state.Scene.NodeName, state.RoundCount,
		state.Metrics.TensionLevel, state.Metrics.ChaosLevel,
		state.Metrics.PlayerAgency, state.Metrics.ObjectiveProgress)

	// 2. LLM 调用（无状态会话：每次规划独立会话，历史不累积）
	userMsg := buildDirectorUserMessage(state, playerMessage, scriptContext)

	events, err := d.runner.Run(ctx,
		"director", // userID
		fmt.Sprintf("%s-plan-%d", sessionID, time.Now().UnixNano()),
		model.NewUserMessage(userMsg),
	)
	if err != nil {
		log.Printf("[Director] LLM 调用失败，降级为规则化指令: %v", err)
		return d.fallbackDirective(state, playerMessage), nil
	}

	reply := collectAgentReply(events)
	if reply == "" {
		log.Printf("[Director] LLM 返回空回复，降级为规则化指令")
		return d.fallbackDirective(state, playerMessage), nil
	}

	// 3. 解析 JSON
	jsonStr := extractAgentJSON(reply)
	if jsonStr == "" {
		log.Printf("[Director] LLM 输出无 JSON，降级为规则化指令")
		return d.fallbackDirective(state, playerMessage), nil
	}

	var directive DecisionDirective
	if err := json.Unmarshal([]byte(jsonStr), &directive); err != nil {
		log.Printf("[Director] JSON 解析失败: %v，降级为规则化指令", err)
		return d.fallbackDirective(state, playerMessage), nil
	}

	elapsed := time.Since(start)
	log.Printf("[Director] 决策完成 (%.1fs): actions=%d, updates=%d",
		elapsed.Seconds(), len(directive.Actions), len(directive.StateUpdates))

	return &directive, nil
}

// rawCompletion 一次无状态的纯文本补全（供 Reflector 等低频任务复用 runner）。
func (d *Director) rawCompletion(worldID, task, prompt string) (string, error) {
	events, err := d.runner.Run(context.Background(),
		"director",
		fmt.Sprintf("%s-%s-%d", worldID, task, time.Now().UnixNano()),
		model.NewUserMessage(prompt),
	)
	if err != nil {
		return "", err
	}
	reply := collectAgentReply(events)
	if reply == "" {
		return "", fmt.Errorf("LLM 返回空回复")
	}
	return reply, nil
}

// fallbackDirective 生成降级规则化指令（LLM 失败时使用）。
func (d *Director) fallbackDirective(state *world.WorldState, playerMessage string) *DecisionDirective {
	directive := &DecisionDirective{
		Assessment: SceneAssessment{
			TensionSummary:   fmt.Sprintf("张力 %d/100", state.Metrics.TensionLevel),
			ChaosSummary:     fmt.Sprintf("混乱 %d/100", state.Metrics.ChaosLevel),
			AgencySummary:    fmt.Sprintf("掌控权 %d/100", state.Metrics.PlayerAgency),
			ProgressSummary:  fmt.Sprintf("目标进度 %d/100", state.Metrics.ObjectiveProgress),
			OverallSituation: "降级模式：基于规则化指标的自动决策",
		},
		NarrationGuide: NarrationGuide{
			Tone:        d.recommendTone(state),
			Pacing:      d.recommendPacing(state),
			FocusPoints: "推进当前场景的叙事，响应玩家行动",
			NPCBehavior: "NPC 按既定性格行动",
			Constraints: "遵循剧本设定，不编造新元素",
		},
		Actions:      []DirectorAction{},
		StateUpdates: []StateUpdate{},
		Reasoning:    "降级模式：LLM 不可用，使用规则化指标生成基础指令",
	}

	// 目标全部完成时建议推进
	if state.Metrics.ObjectiveProgress >= 100 {
		directive.Actions = append(directive.Actions, DirectorAction{
			Type:        "advance_timeline",
			Description: "当前节点目标全部完成，建议推进到下一剧情节点",
		})
	}

	// 张力过高时降低难度
	if state.Metrics.TensionLevel > 80 {
		directive.NarrationGuide.Constraints += "；当前张力过高，适当给予玩家喘息空间"
	}

	// 张力过低时增加紧迫感
	if state.Metrics.TensionLevel < 20 {
		directive.Actions = append(directive.Actions, DirectorAction{
			Type:        "adjust_difficulty",
			Description: "当前张力过低，引入新的紧迫感或威胁",
		})
	}

	return directive
}

// recommendTone 根据指标推荐叙事基调。
func (d *Director) recommendTone(state *world.WorldState) string {
	if state.Metrics.TensionLevel > 70 {
		return "紧张"
	}
	if state.Metrics.ChaosLevel > 60 {
		return "混乱"
	}
	if state.Metrics.TensionLevel < 20 {
		return "平静"
	}
	return "正常"
}

// recommendPacing 根据指标推荐节奏。
func (d *Director) recommendPacing(state *world.WorldState) string {
	if state.Metrics.TensionLevel > 70 || state.Metrics.ChaosLevel > 60 {
		return "fast"
	}
	if state.Metrics.TensionLevel < 20 && state.Metrics.ObjectiveProgress < 30 {
		return "slow"
	}
	return "medium"
}

// --- 辅助函数（复用 analyzer.go 的模式，独立实现避免跨包依赖）---

// collectAgentReply 从 event 流收集 LLM 回复。
func collectAgentReply(events <-chan *event.Event) string {
	var sb strings.Builder
	for event := range events {
		if event.Object == "chat.completion.chunk" {
			if len(event.Response.Choices) > 0 {
				sb.WriteString(event.Response.Choices[0].Delta.Content)
			}
		} else if event.Object == "chat.completion" {
			if len(event.Response.Choices) > 0 {
				sb.WriteString(event.Response.Choices[0].Message.Content)
			}
		}
	}
	return sb.String()
}

// extractAgentJSON 从文本中提取 JSON 字符串。
func extractAgentJSON(text string) string {
	text = strings.TrimSpace(text)

	// 尝试直接解析
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		return text
	}

	// 尝试提取 ```json ... ``` 代码块
	if idx := strings.Index(text, "```json"); idx >= 0 {
		start := idx + 7
		end := strings.Index(text[start:], "```")
		if end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// 尝试提取 ``` ... ``` 代码块
	if idx := strings.Index(text, "```"); idx >= 0 {
		start := idx + 3
		if nl := strings.Index(text[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		end := strings.Index(text[start:], "```")
		if end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// 尝试找到第一个 { 和最后一个 }
	first := strings.Index(text, "{")
	last := strings.LastIndex(text, "}")
	if first >= 0 && last > first {
		return text[first : last+1]
	}

	return ""
}
