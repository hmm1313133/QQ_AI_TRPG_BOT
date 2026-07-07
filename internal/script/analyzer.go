// 剧本识别 Agent：使用 trpc-agent-go 的 LLMAgent 能力，
// 接收解析后的剧本文本，通过结构化提示词输出符合 Script JSON 结构的结果。
//
// 与 KPAgent 的区别：
//   - 使用独立 Agent 实例（不同 system prompt、低温度参数）
//   - 无需工具调用（纯文本输入 → JSON 输出）
//   - 一次性批处理任务，非持续对话
package script

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// AnalyzerConfig 是剧本识别 Agent 的配置。
type AnalyzerConfig struct {
	LLMModel   string  // 模型名称
	LLMAPIKey  string  // API 密钥
	LLMBaseURL string  // API 地址
	MaxTokens  int     // 最大 token 数
	Temperature float64 // 温度（建议 0.3，保证一致性）
}

// ProgressFunc 是分析进度回调函数。
// stage 为阶段标识，message 为人类可读的进度描述。
type ProgressFunc func(stage string, message string)

// ScriptAnalyzer 是剧本识别 Agent。
type ScriptAnalyzer struct {
	config   *AnalyzerConfig
	agent    *llmagent.LLMAgent
	runner   runner.Runner
}

// NewScriptAnalyzer 创建剧本识别 Agent。
func NewScriptAnalyzer(cfg *AnalyzerConfig) (*ScriptAnalyzer, error) {
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 8192
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.3
	}

	// 复用 KPAgent 的环境变量设置模式
	if cfg.LLMAPIKey != "" {
		_ = os.Setenv("OPENAI_API_KEY", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "" {
		_ = os.Setenv("OPENAI_BASE_URL", cfg.LLMBaseURL)
	}

	// 创建模型实例（DeepSeek 变体）
	modelOpts := []openai.Option{
		openai.WithVariant(openai.VariantDeepSeek),
	}
	modelInstance := openai.New(cfg.LLMModel, modelOpts...)

	// 创建 LLMAgent
	maxTokens := cfg.MaxTokens
	temp := cfg.Temperature
	genConfig := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &temp,
		Stream:      false,
	}

	a := llmagent.New("script-analyzer",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(ScriptAnalyzerSystemPrompt()),
		llmagent.WithGenerationConfig(genConfig),
	)

	r := runner.NewRunner("script-analyzer", a)

	analyzer := &ScriptAnalyzer{
		config: cfg,
		agent:  a,
		runner: r,
	}

	log.Printf("[ScriptAnalyzer] 初始化完成, model=%s, temperature=%.1f", cfg.LLMModel, cfg.Temperature)
	return analyzer, nil
}

// analyzerResult 是 AI 返回的 JSON 结构，用于解析为 Script。
type analyzerResult struct {
	Title      string            `json:"title"`
	Name       string            `json:"name"`
	System     string            `json:"system"`
	Background StoryBackground   `json:"background"`
	Timeline   []TimelineNode    `json:"timeline"`
	Characters []ScriptCharacter `json:"characters"`
	Scenes     []ScriptScene     `json:"scenes"`
}

// Analyze 接收剧本文本，返回识别后的 Script。
// progress 回调用于推送分析进度（可为 nil）。
func (a *ScriptAnalyzer) Analyze(ctx context.Context, text string, sourceFile string, progress ProgressFunc) (*Script, error) {
	if len(strings.TrimSpace(text)) == 0 {
		return nil, fmt.Errorf("剧本文本为空")
	}

	textLen := len([]rune(text))
	log.Printf("[ScriptAnalyzer] 开始分析: source=%s, 文本长度=%d 字符", sourceFile, textLen)
	a.notify(progress, "parse_done", fmt.Sprintf("文本提取完成，共 %d 字符，开始 AI 识别...", textLen))

	// 构建用户消息
	userMessage := fmt.Sprintf("请分析以下 TRPG 剧本文本，提取结构化信息并按指定 JSON 格式输出：\n\n---\n%s\n---", text)

	// 运行 Agent
	a.notify(progress, "ai_start", "AI 正在阅读剧本并提取结构化信息，请稍候...")
	log.Printf("[ScriptAnalyzer] 调用 LLM Agent, model=%s", a.config.LLMModel)
	aiStart := time.Now()

	events, err := a.runner.Run(ctx, "analyzer", "script-analysis",
		model.NewUserMessage(userMessage),
	)
	if err != nil {
		log.Printf("[ScriptAnalyzer] Agent 执行失败: %v", err)
		return nil, fmt.Errorf("剧本识别 Agent 执行失败: %w", err)
	}

	// 收集回复，同时跟踪流式进度
	var replyBuilder strings.Builder
	chunkCount := 0
	for event := range events {
		if event.Object == "chat.completion.chunk" {
			if len(event.Response.Choices) > 0 {
				replyBuilder.WriteString(event.Response.Choices[0].Delta.Content)
				chunkCount++
				// 每 50 个 chunk 推送一次进度
				if chunkCount%50 == 0 {
					currentLen := len([]rune(replyBuilder.String()))
					a.notify(progress, "ai_streaming", fmt.Sprintf("AI 正在生成分析结果... 已接收 %d 字符", currentLen))
				}
			}
		} else if event.Object == "chat.completion" {
			if len(event.Response.Choices) > 0 {
				replyBuilder.WriteString(event.Response.Choices[0].Message.Content)
			}
		}
	}

	aiDuration := time.Since(aiStart)
	reply := replyBuilder.String()
	replyLen := len([]rune(reply))
	log.Printf("[ScriptAnalyzer] LLM 回复完成: %d chunks, %d 字符, 耗时 %s",
		chunkCount, replyLen, aiDuration.Round(time.Millisecond))

	if reply == "" {
		return nil, fmt.Errorf("剧本识别 Agent 未生成回复")
	}

	// 解析 JSON 回复
	a.notify(progress, "parsing", "AI 回复完成，正在解析结构化数据...")
	log.Printf("[ScriptAnalyzer] 解析 JSON 回复")
	result, err := parseAnalyzerResponse(reply)
	if err != nil {
		log.Printf("[ScriptAnalyzer] JSON 解析失败: %v", err)
		return nil, fmt.Errorf("解析剧本识别结果失败: %w", err)
	}

	// 转换为 Script
	scr := &Script{
		ID:         generateScriptID(result.Name),
		Name:       result.Name,
		Title:      result.Title,
		System:     result.System,
		Background: result.Background,
		Timeline:   result.Timeline,
		Characters: result.Characters,
		Scenes:     result.Scenes,
		CreatedAt:  time.Now().Format("2006-01-02 15:04:05"),
		SourceFile: sourceFile,
	}

	// 补充角色 ID
	for i := range scr.Characters {
		if scr.Characters[i].ID == "" {
			scr.Characters[i].ID = fmt.Sprintf("char_%d", i+1)
		}
	}

	// 补充场景 ID
	for i := range scr.Scenes {
		if scr.Scenes[i].ID == "" {
			scr.Scenes[i].ID = fmt.Sprintf("scene_%d", i+1)
		}
	}

	// 补充时间轴节点 ID 和 Order
	for i := range scr.Timeline {
		if scr.Timeline[i].ID == "" {
			scr.Timeline[i].ID = fmt.Sprintf("node_%d", i+1)
		}
		if scr.Timeline[i].Order == 0 {
			scr.Timeline[i].Order = i + 1
		}
	}

	// 确保规则集有效
	if scr.System != "coc7" && scr.System != "dnd5e" {
		scr.System = "coc7" // 默认 CoC7
	}

	log.Printf("[ScriptAnalyzer] 识别完成: %s, 规则集=%s, 节点=%d, 角色=%d, 场景=%d, 总耗时 %s",
		scr.Title, scr.System, len(scr.Timeline), len(scr.Characters), len(scr.Scenes),
		time.Since(aiStart).Round(time.Millisecond))

	a.notify(progress, "done", fmt.Sprintf("识别完成: %s | 节点 %d | 角色 %d | 场景 %d",
		scr.Title, len(scr.Timeline), len(scr.Characters), len(scr.Scenes)))

	return scr, nil
}

// notify 安全调用进度回调（nil 时跳过）。
func (a *ScriptAnalyzer) notify(progress ProgressFunc, stage, message string) {
	if progress != nil {
		progress(stage, message)
	}
}

// parseAnalyzerResponse 从 AI 回复中提取 JSON。
// 处理可能的 markdown 代码块包裹和多余文本。
func parseAnalyzerResponse(reply string) (*analyzerResult, error) {
	jsonStr := extractJSON(reply)
	if jsonStr == "" {
		return nil, fmt.Errorf("回复中未找到有效 JSON: %s", truncate(reply, 200))
	}

	var result analyzerResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w, 原始内容: %s", err, truncate(jsonStr, 200))
	}

	if result.Title == "" && result.Name == "" {
		return nil, fmt.Errorf("JSON 缺少必要字段（title/name）")
	}

	return &result, nil
}

// extractJSON 从可能包含 markdown 标记的文本中提取 JSON 字符串。
func extractJSON(text string) string {
	text = strings.TrimSpace(text)

	// 尝试直接解析
	if strings.HasPrefix(text, "{") {
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
		// 跳过可能的语言标识行
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

// generateScriptID 根据名称生成剧本 ID。
func generateScriptID(name string) string {
	if name == "" {
		return fmt.Sprintf("script_%d", time.Now().Unix())
	}
	// 简单清理：空格替换为下划线
	return strings.ReplaceAll(strings.TrimSpace(name), " ", "_")
}

// truncate 截断字符串到指定长度。
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
