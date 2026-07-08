// 剧本识别 Agent：使用 trpc-agent-go 的多阶段多 Agent 流水线能力。
//
// 三阶段流水线：
//   Phase 1: Planner Agent - 通读全文，输出提取计划 + 文本分段索引
//   Phase 2: 4个 Extractor Agent 并行 - 工具调用 Agent，自主读取段落提取模块
//   Phase 3: Integrator Agent - 整合 4 模块结果，交叉引用
//
// 与旧版的区别：
//   - 不再一次性提取所有模块，而是分阶段、分模块提取
//   - Extractor 是工具调用 Agent，通过文本访问工具自主读取相关段落
//   - 解决长文本注意力稀疏和细节丢失问题
package script

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// AnalyzerConfig 是剧本识别 Agent 的配置。
type AnalyzerConfig struct {
	LLMModel    string  // 模型名称
	LLMAPIKey   string  // API 密钥
	LLMBaseURL  string  // API 地址
	MaxTokens   int     // 最大 token 数
	Temperature float64 // 温度（建议 0.3，保证一致性）
}

// ProgressFunc 是分析进度回调函数。
// stage 为阶段标识，message 为人类可读的进度描述。
type ProgressFunc func(stage string, message string)

// ScriptAnalyzer 是剧本识别 Agent（多阶段多 Agent 流水线）。
type ScriptAnalyzer struct {
	config     *AnalyzerConfig
	planner    *llmagent.LLMAgent
	plannerRun runner.Runner

	bgExtractor    *llmagent.LLMAgent
	bgExtractorRun runner.Runner

	tlExtractor    *llmagent.LLMAgent
	tlExtractorRun runner.Runner

	chExtractor    *llmagent.LLMAgent
	chExtractorRun runner.Runner

	scExtractor    *llmagent.LLMAgent
	scExtractorRun runner.Runner

	integrator    *llmagent.LLMAgent
	integratorRun runner.Runner
}

// NewScriptAnalyzer 创建剧本识别 Agent（多阶段流水线）。
func NewScriptAnalyzer(cfg *AnalyzerConfig) (*ScriptAnalyzer, error) {
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 16384
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

	// 创建模型实例（DeepSeek 变体），所有 Agent 共享
	modelOpts := []openai.Option{
		openai.WithVariant(openai.VariantDeepSeek),
	}
	modelInstance := openai.New(cfg.LLMModel, modelOpts...)

	// 通用 MaxTokens
	maxTokens := cfg.MaxTokens

	// --- Phase 1: Planner Agent（无工具，纯文本->JSON） ---
	plannerTemp := 0.2
	plannerGenConfig := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &plannerTemp,
		Stream:      false,
	}
	planner := llmagent.New("script-planner",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(PlannerPrompt()),
		llmagent.WithGenerationConfig(plannerGenConfig),
	)
	plannerRun := runner.NewRunner("script-planner", planner)

	// --- Phase 2: 4 个 Extractor Agent（带文本访问工具） ---
	extractorTemp := 0.3
	extractorGenConfig := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &extractorTemp,
		Stream:      false,
	}
	textTools := NewTextAccessTools()

	// 每个_extractor 使用不同的 MaxToolIterations
	bgMaxIter := 15
	tlMaxIter := 20 // timeline 通常需要更多读取
	chMaxIter := 15
	scMaxIter := 15

	bgExtractor := llmagent.New("bg-extractor",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(BackgroundExtractorPrompt()),
		llmagent.WithGenerationConfig(extractorGenConfig),
		llmagent.WithTools(textTools),
		llmagent.WithMaxToolIterations(bgMaxIter),
	)
	bgRun := runner.NewRunner("bg-extractor", bgExtractor)

	tlExtractor := llmagent.New("tl-extractor",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(TimelineExtractorPrompt()),
		llmagent.WithGenerationConfig(extractorGenConfig),
		llmagent.WithTools(textTools),
		llmagent.WithMaxToolIterations(tlMaxIter),
	)
	tlRun := runner.NewRunner("tl-extractor", tlExtractor)

	chExtractor := llmagent.New("ch-extractor",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(CharactersExtractorPrompt()),
		llmagent.WithGenerationConfig(extractorGenConfig),
		llmagent.WithTools(textTools),
		llmagent.WithMaxToolIterations(chMaxIter),
	)
	chRun := runner.NewRunner("ch-extractor", chExtractor)

	scExtractor := llmagent.New("sc-extractor",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(ScenesExtractorPrompt()),
		llmagent.WithGenerationConfig(extractorGenConfig),
		llmagent.WithTools(textTools),
		llmagent.WithMaxToolIterations(scMaxIter),
	)
	scRun := runner.NewRunner("sc-extractor", scExtractor)

	// --- Phase 3: Integrator Agent（无工具，纯文本->JSON） ---
	integratorTemp := 0.2
	integratorGenConfig := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &integratorTemp,
		Stream:      false,
	}
	integrator := llmagent.New("script-integrator",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(IntegratorPrompt()),
		llmagent.WithGenerationConfig(integratorGenConfig),
	)
	integratorRun := runner.NewRunner("script-integrator", integrator)

	analyzer := &ScriptAnalyzer{
		config:         cfg,
		planner:        planner,
		plannerRun:     plannerRun,
		bgExtractor:    bgExtractor,
		bgExtractorRun: bgRun,
		tlExtractor:    tlExtractor,
		tlExtractorRun: tlRun,
		chExtractor:    chExtractor,
		chExtractorRun: chRun,
		scExtractor:    scExtractor,
		scExtractorRun: scRun,
		integrator:     integrator,
		integratorRun:  integratorRun,
	}

	log.Printf("[ScriptAnalyzer] 初始化完成（多阶段流水线）, model=%s", cfg.LLMModel)
	return analyzer, nil
}

// ============================================================
// 数据类型
// ============================================================

// ExtractionPlan 是 Phase 1 Planner Agent 的输出。
type ExtractionPlan struct {
	Title                string            `json:"title"`
	Name                 string            `json:"name"`
	System               string            `json:"system"`
	TextStructure        string            `json:"text_structure"`
	ExtractionHints      map[string]string `json:"extraction_hints"`
	KeyContentToPreserve []KeyContent      `json:"key_content_to_preserve"`
	SegmentMap           []TextSegment     `json:"segment_map"`
}

// KeyContent 标记需逐字保留的内容。
type KeyContent struct {
	Description string `json:"description"`
	Module      string `json:"module"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
}

// TextSegment 文本分段索引。
type TextSegment struct {
	Label           string   `json:"label"`
	StartLine       int      `json:"start_line"`
	EndLine         int      `json:"end_line"`
	RelevantModules []string `json:"relevant_modules"`
	Summary         string   `json:"summary"`
}

// ExtractionResults 收集 Phase 2 四个模块的提取结果。
type ExtractionResults struct {
	Background  StoryBackground   `json:"background"`
	Timeline    []TimelineNode    `json:"timeline"`
	Characters  []ScriptCharacter `json:"characters"`
	Scenes      []ScriptScene     `json:"scenes"`
}

// analyzerResult 是 Phase 3 Integrator Agent 返回的最终 JSON 结构。
type analyzerResult struct {
	Title      string            `json:"title"`
	Name       string            `json:"name"`
	System     string            `json:"system"`
	Background StoryBackground   `json:"background"`
	Timeline   []TimelineNode    `json:"timeline"`
	Characters []ScriptCharacter `json:"characters"`
	Scenes     []ScriptScene     `json:"scenes"`
}

// ============================================================
// Analyze 方法（三阶段流水线）
// ============================================================

// Analyze 接收剧本文本，返回识别后的 Script。
// progress 回调用于推送分析进度（可为 nil）。
func (a *ScriptAnalyzer) Analyze(ctx context.Context, text string, sourceFile string, progress ProgressFunc) (*Script, error) {
	if len(strings.TrimSpace(text)) == 0 {
		return nil, fmt.Errorf("剧本文本为空")
	}

	textLen := len([]rune(text))
	log.Printf("[ScriptAnalyzer] 开始分析: source=%s, 文本长度=%d 字符", sourceFile, textLen)
	a.notify(progress, "parse_done", fmt.Sprintf("文本提取完成，共 %d 字符，开始多阶段 AI 分析...", textLen))

	// 创建文本访问器
	provider := NewTextAccessProvider(text)

	// === Phase 1: Planner ===
	a.notify(progress, "planning", "Phase 1: AI 正在通读全文，制定提取计划...")
	planStart := time.Now()
	plan, err := a.runPlanner(ctx, text)
	if err != nil {
		log.Printf("[ScriptAnalyzer] Phase 1 规划失败: %v", err)
		return nil, fmt.Errorf("规划阶段失败: %w", err)
	}
	log.Printf("[ScriptAnalyzer] Phase 1 完成: %s, 规则集=%s, 分段=%d, 关键内容=%d, 耗时 %s",
		plan.Title, plan.System, len(plan.SegmentMap), len(plan.KeyContentToPreserve),
		time.Since(planStart).Round(time.Millisecond))
	a.notify(progress, "planning_done", fmt.Sprintf("提取计划完成：%s | 分段 %d | 关键内容 %d 项",
		plan.Title, len(plan.SegmentMap), len(plan.KeyContentToPreserve)))

	// === Phase 2: 并行模块提取 ===
	a.notify(progress, "extracting", "Phase 2: 4 个 AI Agent 并行提取模块...")
	extractStart := time.Now()
	results, extractErrors := a.runParallelExtraction(ctx, plan, provider, progress)
	log.Printf("[ScriptAnalyzer] Phase 2 完成: 耗时 %s, 错误数 %d",
		time.Since(extractStart).Round(time.Millisecond), len(extractErrors))
	for module, err := range extractErrors {
		log.Printf("[ScriptAnalyzer] 模块 %s 提取失败: %v", module, err)
	}
	a.notify(progress, "extracting_done", fmt.Sprintf("模块提取完成 | 节点 %d | 角色 %d | 场景 %d",
		len(results.Timeline), len(results.Characters), len(results.Scenes)))

	// === Phase 3: Integrator ===
	a.notify(progress, "integrating", "Phase 3: AI 正在整合各模块结果...")
	integrateStart := time.Now()
	result, err := a.runIntegrator(ctx, plan, results)
	if err != nil {
		log.Printf("[ScriptAnalyzer] Phase 3 整合失败: %v", err)
		return nil, fmt.Errorf("整合阶段失败: %w", err)
	}
	log.Printf("[ScriptAnalyzer] Phase 3 完成: 耗时 %s",
		time.Since(integrateStart).Round(time.Millisecond))

	// === 后处理 ===
	a.notify(progress, "parsing", "正在解析结构化数据...")

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
		scr.System = "coc7"
	}

	totalDuration := time.Since(planStart).Round(time.Millisecond)
	log.Printf("[ScriptAnalyzer] 识别完成: %s, 规则集=%s, 节点=%d, 角色=%d, 场景=%d, 总耗时 %s",
		scr.Title, scr.System, len(scr.Timeline), len(scr.Characters), len(scr.Scenes), totalDuration)

	a.notify(progress, "done", fmt.Sprintf("识别完成: %s | 节点 %d | 角色 %d | 场景 %d",
		scr.Title, len(scr.Timeline), len(scr.Characters), len(scr.Scenes)))

	return scr, nil
}

// ============================================================
// Phase 1: Planner
// ============================================================

// runPlanner 执行 Phase 1，让 AI 通读全文输出提取计划。
func (a *ScriptAnalyzer) runPlanner(ctx context.Context, text string) (*ExtractionPlan, error) {
	// 构建带行号的文本
	numberedText := buildNumberedText(text)
	userMessage := fmt.Sprintf("请分析以下 TRPG 剧本文本（带行号），输出提取计划：\n\n---\n%s\n---", numberedText)

	events, err := a.plannerRun.Run(ctx, "analyzer", "script-planning",
		model.NewUserMessage(userMessage),
	)
	if err != nil {
		return nil, fmt.Errorf("Planner Agent 执行失败: %w", err)
	}

	reply := collectReply(events)
	if reply == "" {
		return nil, fmt.Errorf("Planner Agent 未生成回复")
	}

	jsonStr := extractJSON(reply)
	if jsonStr == "" {
		return nil, fmt.Errorf("Planner 回复中未找到有效 JSON: %s", truncate(reply, 200))
	}

	var plan ExtractionPlan
	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return nil, fmt.Errorf("Planner JSON 解析失败: %w, 原始: %s", err, truncate(jsonStr, 200))
	}

	if plan.Title == "" && plan.Name == "" {
		return nil, fmt.Errorf("Planner JSON 缺少必要字段（title/name）")
	}

	return &plan, nil
}

// ============================================================
// Phase 2: 并行模块提取
// ============================================================

// extractModule 是单个 extractor 的提取任务描述。
type extractModule struct {
	name     string
	agent    *llmagent.LLMAgent
	runner   runner.Runner
	hintKey  string // extraction_hints 中的 key
	maxIter  int
}

// runParallelExtraction 并行执行 4 个模块提取。
func (a *ScriptAnalyzer) runParallelExtraction(
	ctx context.Context,
	plan *ExtractionPlan,
	provider *TextAccessProvider,
	progress ProgressFunc,
) (*ExtractionResults, map[string]error) {

	// 构建 segment_map + hints 的共享上下文文本
	segmentMapJSON, _ := json.MarshalIndent(plan.SegmentMap, "", "  ")
	hintsJSON, _ := json.MarshalIndent(plan.ExtractionHints, "", "  ")
	keyContentJSON, _ := json.MarshalIndent(plan.KeyContentToPreserve, "", "  ")

	sharedContext := fmt.Sprintf("## 文本分段索引（segment_map）\n%s\n\n## 提取要点（extraction_hints）\n%s\n\n## 需逐字保留的关键内容\n%s",
		string(segmentMapJSON), string(hintsJSON), string(keyContentJSON))

	// 4 个模块的提取任务
	modules := []extractModule{
		{name: "background", agent: a.bgExtractor, runner: a.bgExtractorRun, hintKey: "background", maxIter: 15},
		{name: "timeline", agent: a.tlExtractor, runner: a.tlExtractorRun, hintKey: "timeline", maxIter: 20},
		{name: "characters", agent: a.chExtractor, runner: a.chExtractorRun, hintKey: "characters", maxIter: 15},
		{name: "scenes", agent: a.scExtractor, runner: a.scExtractorRun, hintKey: "scenes", maxIter: 15},
	}

	results := &ExtractionResults{}
	errs := make(map[string]error)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, mod := range modules {
		wg.Add(1)
		go func(m extractModule) {
			defer wg.Done()

			moduleStart := time.Now()
			log.Printf("[ScriptAnalyzer] 模块 %s 开始提取", m.name)

			// 构建 user message
			userMessage := fmt.Sprintf("%s\n\n请根据以上信息，使用文本访问工具读取原文相关段落，提取 %s 模块的内容。"+
				"先用 segment_map 找到 relevant_modules 包含 \"%s\" 的段落，然后调用 read_text_segment 读取。"+
				"读取完所有相关段落后，输出 JSON。", sharedContext, m.name, m.name)

			// 注入 TextAccessProvider 到 context
			agentCtx := withTextAccessProvider(ctx, provider)

			events, err := m.runner.Run(agentCtx, "analyzer", fmt.Sprintf("extract-%s", m.name),
				model.NewUserMessage(userMessage),
			)
			if err != nil {
				mu.Lock()
				errs[m.name] = fmt.Errorf("Agent 执行失败: %w", err)
				mu.Unlock()
				log.Printf("[ScriptAnalyzer] 模块 %s Agent 执行失败: %v", m.name, err)
				return
			}

			reply := collectReply(events)
			if reply == "" {
				mu.Lock()
				errs[m.name] = fmt.Errorf("Agent 未生成回复")
				mu.Unlock()
				log.Printf("[ScriptAnalyzer] 模块 %s 未生成回复", m.name)
				return
			}

			// 解析 JSON
			jsonStr := extractJSON(reply)
			if jsonStr == "" {
				mu.Lock()
				errs[m.name] = fmt.Errorf("回复中未找到有效 JSON: %s", truncate(reply, 200))
				mu.Unlock()
				log.Printf("[ScriptAnalyzer] 模块 %s JSON 提取失败", m.name)
				return
			}

			// 写入对应模块
			mu.Lock()
			defer mu.Unlock()
			switch m.name {
			case "background":
				if err := json.Unmarshal([]byte(jsonStr), &results.Background); err != nil {
					errs[m.name] = fmt.Errorf("JSON 解析失败: %w", err)
					log.Printf("[ScriptAnalyzer] 模块 background JSON 解析失败: %v", err)
				}
			case "timeline":
				if err := json.Unmarshal([]byte(jsonStr), &results.Timeline); err != nil {
					errs[m.name] = fmt.Errorf("JSON 解析失败: %w", err)
					log.Printf("[ScriptAnalyzer] 模块 timeline JSON 解析失败: %v", err)
				}
			case "characters":
				if err := json.Unmarshal([]byte(jsonStr), &results.Characters); err != nil {
					errs[m.name] = fmt.Errorf("JSON 解析失败: %w", err)
					log.Printf("[ScriptAnalyzer] 模块 characters JSON 解析失败: %v", err)
				}
			case "scenes":
				if err := json.Unmarshal([]byte(jsonStr), &results.Scenes); err != nil {
					errs[m.name] = fmt.Errorf("JSON 解析失败: %w", err)
					log.Printf("[ScriptAnalyzer] 模块 scenes JSON 解析失败: %v", err)
				}
			}

			log.Printf("[ScriptAnalyzer] 模块 %s 提取完成, 耗时 %s",
				m.name, time.Since(moduleStart).Round(time.Millisecond))
		}(mod)
	}

	wg.Wait()

	// 推送每个模块的完成状态
	for _, mod := range modules {
		if _, hasErr := errs[mod.name]; hasErr {
			a.notify(progress, "extracting", fmt.Sprintf("模块 %s 提取失败（将尝试在整合阶段补充）", mod.name))
		} else {
			a.notify(progress, "extracting", fmt.Sprintf("模块 %s 提取完成", mod.name))
		}
	}

	return results, errs
}

// ============================================================
// Phase 3: Integrator
// ============================================================

// runIntegrator 执行 Phase 3，整合 4 个模块结果。
func (a *ScriptAnalyzer) runIntegrator(ctx context.Context, plan *ExtractionPlan, results *ExtractionResults) (*analyzerResult, error) {
	// 构建整合输入
	planJSON, _ := json.MarshalIndent(plan, "", "  ")
	resultsJSON, _ := json.MarshalIndent(results, "", "  ")

	userMessage := fmt.Sprintf("## 提取计划（ExtractionPlan）\n%s\n\n## 四个模块的提取结果\n%s\n\n请整合以上内容为最终的剧本 JSON。",
		string(planJSON), string(resultsJSON))

	events, err := a.integratorRun.Run(ctx, "analyzer", "script-integration",
		model.NewUserMessage(userMessage),
	)
	if err != nil {
		return nil, fmt.Errorf("Integrator Agent 执行失败: %w", err)
	}

	reply := collectReply(events)
	if reply == "" {
		return nil, fmt.Errorf("Integrator Agent 未生成回复")
	}

	// 解析 JSON
	jsonStr := extractJSON(reply)
	if jsonStr == "" {
		return nil, fmt.Errorf("Integrator 回复中未找到有效 JSON: %s", truncate(reply, 200))
	}

	var result analyzerResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("Integrator JSON 解析失败: %w, 原始: %s", err, truncate(jsonStr, 200))
	}

	if result.Title == "" && result.Name == "" {
		// 从 plan 补充
		result.Title = plan.Title
		result.Name = plan.Name
	}

	return &result, nil
}

// ============================================================
// 辅助函数
// ============================================================

// notify 安全调用进度回调（nil 时跳过）。
func (a *ScriptAnalyzer) notify(progress ProgressFunc, stage, message string) {
	if progress != nil {
		progress(stage, message)
	}
}

// buildNumberedText 将文本转换为带行号的格式。
func buildNumberedText(text string) string {
	lines := strings.Split(text, "\n")
	var sb strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&sb, "%d: %s\n", i+1, line)
	}
	return sb.String()
}

// collectReply 从事件流中收集 LLM 回复文本。
func collectReply(events <-chan *event.Event) string {
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
	return replyBuilder.String()
}

// extractJSON 从可能包含 markdown 标记的文本中提取 JSON 字符串。
func extractJSON(text string) string {
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

	// 尝试找到第一个 [ 和最后一个 ]
	firstBr := strings.Index(text, "[")
	lastBr := strings.LastIndex(text, "]")
	if firstBr >= 0 && lastBr > firstBr {
		return text[firstBr : lastBr+1]
	}

	return ""
}

// generateScriptID 根据名称生成剧本 ID。
func generateScriptID(name string) string {
	if name == "" {
		return fmt.Sprintf("script_%d", time.Now().Unix())
	}
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
