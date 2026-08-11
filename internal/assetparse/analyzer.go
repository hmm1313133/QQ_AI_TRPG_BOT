// 自由文本素材解析：单 Agent LLM 提取（设计 §11.4）。
//
// 大文本按段落分块逐块提取后合并（splitTextChunks/mergeOutputs），
// 避免单次调用把长文压缩概括导致细节丢失；非实体设定（仪式/时间线等）
// 提取为通用 lore 设定条目，content 尽量保留原文。
package assetparse

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

// Config LLM 解析器配置（与剧本 AnalyzerConfig 同款字段）。
type Config struct {
	LLMModel    string
	LLMAPIKey   string
	LLMBaseURL  string
	MaxTokens   int
	Temperature float64
}

// chunkRunes 分块提取的单块大小（按段落边界切分，大文本不再截断丢弃）。
const chunkRunes = 12000

// maxChunks 分块数上限（防极端超长输入打爆解析耗时；超出部分截断并记日志）。
const maxChunks = 20

// Parser 自由文本素材解析器（单 LLM Agent）。
type Parser struct {
	agent       *llmagent.LLMAgent
	run         runner.Runner
	completeRun runner.Runner // AI 补全（completePrompt instruction）
}

// NewParser 创建 LLM 素材解析器。
func NewParser(cfg *Config) (*Parser, error) {
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 8192
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.3
	}
	if cfg.LLMAPIKey != "" {
		_ = os.Setenv("OPENAI_API_KEY", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "" {
		_ = os.Setenv("OPENAI_BASE_URL", cfg.LLMBaseURL)
	}

	modelInstance := openai.New(cfg.LLMModel, openai.WithVariant(openai.VariantDeepSeek))
	maxTokens := cfg.MaxTokens
	temp := cfg.Temperature
	genConfig := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &temp,
		Stream:      false,
	}
	agent := llmagent.New("asset-parser",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(parserPrompt()),
		llmagent.WithGenerationConfig(genConfig),
	)
	// 补全用稍高的温度（要"想新点子"，不是提取）
	completeTemp := temp + 0.2
	completeAgent := llmagent.New("asset-completer",
		llmagent.WithModel(modelInstance),
		llmagent.WithInstruction(completePrompt()),
		llmagent.WithGenerationConfig(model.GenerationConfig{
			MaxTokens:   &maxTokens,
			Temperature: &completeTemp,
			Stream:      false,
		}),
	)
	return &Parser{
		agent:       agent,
		run:         runner.NewRunner("asset-parser", agent),
		completeRun: runner.NewRunner("asset-completer", completeAgent),
	}, nil
}

// extractedCharacter LLM 提取的单个角色（角色卡整理回写时按名字匹配）。
type extractedCharacter struct {
	Name          string   `json:"name"`
	Summary       string   `json:"summary"`
	Appearance    string   `json:"appearance"`
	Personality   string   `json:"personality"`
	Backstory     string   `json:"backstory"`
	Motivation    string   `json:"motivation"`
	DialogueStyle string   `json:"dialogue_style"`
	KeyDialogue   []string `json:"key_dialogue"`
	Skills        []string `json:"skills"`
	Tags          []string `json:"tags"`
}

// extractedWorldview LLM 提取的世界观。
type extractedWorldview struct {
	Name       string   `json:"name"`
	Setting    string   `json:"setting"`
	Era        string   `json:"era"`
	Location   string   `json:"location"`
	Atmosphere string   `json:"atmosphere"`
	Tone       string   `json:"tone"`
	Backstory  string   `json:"backstory"`
	Themes     []string `json:"themes"`
}

// extractedLocation LLM 提取的地点。
type extractedLocation struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Atmosphere  string   `json:"atmosphere"`
	Danger      string   `json:"danger"`
	Points      []string `json:"points"`
}

// extractedItem LLM 提取的物品。
type extractedItem struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// extractedFaction LLM 提取的势力。
type extractedFaction struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Goals       []string `json:"goals"`
	Leader      string   `json:"leader"`
}

// extractedStoryline LLM 提取的故事主线。
type extractedStoryline struct {
	Title   string `json:"title"`
	Premise string `json:"premise"`
	Acts    []struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"acts"`
}

// extractedLore LLM 提取的通用设定条目（仪式/时间线/习俗/规则等非实体设定）。
// 与 world.LoreEntry 同形（ID/Enabled 入库或播种时补），content 要求保留原文。
type extractedLore struct {
	Title    string   `json:"title"`
	Category string   `json:"category"`
	Keys     []string `json:"keys"`
	Content  string   `json:"content"`
	Position string   `json:"position"`
}

// extractionOutput LLM 提取输出的 JSON 结构。
type extractionOutput struct {
	Characters []extractedCharacter `json:"characters"`
	Worldview  *extractedWorldview  `json:"worldview"`
	Locations  []extractedLocation  `json:"locations"`
	Items      []extractedItem      `json:"items"`
	Factions   []extractedFaction   `json:"factions"`
	Storyline  *extractedStoryline  `json:"storyline"`
	LoreEntries []extractedLore     `json:"lore_entries"`
}

// ParseText 用 LLM 从自由文本中提取素材草稿。
// 大文本按段落分块逐块提取后合并（单次调用会丢失长文细节，见 §11.4 优化）。
func (p *Parser) ParseText(ctx context.Context, text string) (*Result, error) {
	chunks := splitTextChunks(text, chunkRunes)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("文本为空")
	}
	if len(chunks) > maxChunks {
		log.Printf("[AssetParser] 分块数 %d 超上限 %d，超出部分截断", len(chunks), maxChunks)
		chunks = chunks[:maxChunks]
	}

	outs := make([]*extractionOutput, 0, len(chunks))
	for i, chunk := range chunks {
		msg := "请从以下文本中提取创作素材，输出 JSON：\n\n---\n" + chunk + "\n---"
		if len(chunks) > 1 {
			msg = fmt.Sprintf("以下是一篇长文的第 %d/%d 段，只提取本段中出现的素材（其他段会另行提取），输出 JSON：\n\n---\n%s\n---",
				i+1, len(chunks), chunk)
		}
		out, err := p.parseStructured(ctx, msg)
		if err != nil {
			if len(chunks) == 1 {
				return nil, err
			}
			// 多块时单块失败不阻塞整体，记日志继续（宁缺勿丢整篇）
			log.Printf("[AssetParser] 第 %d/%d 段解析失败，跳过: %v", i+1, len(chunks), err)
			continue
		}
		outs = append(outs, out)
	}
	if len(outs) == 0 {
		return nil, fmt.Errorf("LLM 解析失败")
	}

	res := &Result{Parser: "llm"}
	res.Drafts = outputToDrafts(mergeOutputs(outs))
	if len(res.Drafts) == 0 {
		return nil, fmt.Errorf("未能从文本中识别出任何素材")
	}
	return res, nil
}

// splitTextChunks 按段落（空行）边界把长文本切成不超过 maxRunes 的块；
// 单段超长时按换行/句号就近硬切。返回空切片表示文本为空。
func splitTextChunks(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return nil
	}
	if len([]rune(text)) <= maxRunes {
		return []string{text}
	}
	var chunks []string
	var cur []rune
	flush := func() {
		if s := strings.TrimSpace(string(cur)); s != "" {
			chunks = append(chunks, s)
		}
		cur = nil
	}
	for _, para := range strings.Split(text, "\n\n") {
		pr := []rune(strings.TrimSpace(para))
		if len(pr) == 0 {
			continue
		}
		// 单段超长：先 flush 当前块，再把长段按 maxRunes 硬切（优先在换行/句号处断）
		if len(pr) > maxRunes {
			flush()
			for len(pr) > maxRunes {
				cut := maxRunes
				window := string(pr[:maxRunes])
				if idx := strings.LastIndexAny(window, "\n。！？!?"); idx > maxRunes/2 {
					cut = len([]rune(window[:idx])) + 1
				}
				chunks = append(chunks, strings.TrimSpace(string(pr[:cut])))
				pr = pr[cut:]
			}
			if s := strings.TrimSpace(string(pr)); s != "" {
				cur = append(cur, pr...)
			}
			continue
		}
		if len(cur)+len(pr)+2 > maxRunes && len(cur) > 0 {
			flush()
		}
		if len(cur) > 0 {
			cur = append(cur, '\n', '\n')
		}
		cur = append(cur, pr...)
	}
	flush()
	return chunks
}

// mergeOutputs 合并多次分块提取的结果：
//   - 实体按名称去重，标量字段保留更长者（保细节），列表字段取并集
//   - worldview 合并取更长字段；storyline 保留分幕更多的一条
//   - lore_entries 按标题去重，保留 content 更长者
func mergeOutputs(outs []*extractionOutput) *extractionOutput {
	merged := &extractionOutput{}
	longer := func(a, b string) string {
		if len([]rune(b)) > len([]rune(a)) {
			return b
		}
		return a
	}
	union := func(a, b []string, cap int) []string {
		seen := make(map[string]bool, len(a)+len(b))
		var out []string
		for _, s := range append(append([]string{}, a...), b...) {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
			if cap > 0 && len(out) >= cap {
				break
			}
		}
		return out
	}

	charIdx := map[string]int{}
	locIdx := map[string]int{}
	itemIdx := map[string]int{}
	factionIdx := map[string]int{}
	loreIdx := map[string]int{}

	for _, out := range outs {
		if out == nil {
			continue
		}
		for _, c := range out.Characters {
			if i, ok := charIdx[c.Name]; ok {
				m := &merged.Characters[i]
				m.Summary = longer(m.Summary, c.Summary)
				m.Appearance = longer(m.Appearance, c.Appearance)
				m.Personality = longer(m.Personality, c.Personality)
				m.Backstory = longer(m.Backstory, c.Backstory)
				m.Motivation = longer(m.Motivation, c.Motivation)
				m.DialogueStyle = longer(m.DialogueStyle, c.DialogueStyle)
				m.KeyDialogue = union(m.KeyDialogue, c.KeyDialogue, 3)
				m.Skills = union(m.Skills, c.Skills, 0)
				m.Tags = union(m.Tags, c.Tags, 0)
				continue
			}
			charIdx[c.Name] = len(merged.Characters)
			merged.Characters = append(merged.Characters, c)
		}
		for _, l := range out.Locations {
			if i, ok := locIdx[l.Name]; ok {
				m := &merged.Locations[i]
				m.Description = longer(m.Description, l.Description)
				m.Atmosphere = longer(m.Atmosphere, l.Atmosphere)
				m.Danger = longer(m.Danger, l.Danger)
				m.Points = union(m.Points, l.Points, 0)
				continue
			}
			locIdx[l.Name] = len(merged.Locations)
			merged.Locations = append(merged.Locations, l)
		}
		for _, it := range out.Items {
			if i, ok := itemIdx[it.Name]; ok {
				m := &merged.Items[i]
				m.Description = longer(m.Description, it.Description)
				if m.Type == "" {
					m.Type = it.Type
				}
				m.Tags = union(m.Tags, it.Tags, 0)
				continue
			}
			itemIdx[it.Name] = len(merged.Items)
			merged.Items = append(merged.Items, it)
		}
		for _, f := range out.Factions {
			if i, ok := factionIdx[f.Name]; ok {
				m := &merged.Factions[i]
				m.Description = longer(m.Description, f.Description)
				m.Goals = union(m.Goals, f.Goals, 0)
				m.Leader = longer(m.Leader, f.Leader)
				continue
			}
			factionIdx[f.Name] = len(merged.Factions)
			merged.Factions = append(merged.Factions, f)
		}
		for _, e := range out.LoreEntries {
			if i, ok := loreIdx[e.Title]; ok {
				m := &merged.LoreEntries[i]
				m.Content = longer(m.Content, e.Content)
				if m.Category == "" {
					m.Category = e.Category
				}
				m.Keys = union(m.Keys, e.Keys, 5)
				continue
			}
			loreIdx[e.Title] = len(merged.LoreEntries)
			merged.LoreEntries = append(merged.LoreEntries, e)
		}
		if out.Worldview != nil {
			w := out.Worldview
			if merged.Worldview == nil {
				cp := *w
				merged.Worldview = &cp
			} else {
				m := merged.Worldview
				if m.Name == "" {
					m.Name = w.Name
				}
				m.Setting = longer(m.Setting, w.Setting)
				m.Era = longer(m.Era, w.Era)
				m.Location = longer(m.Location, w.Location)
				m.Atmosphere = longer(m.Atmosphere, w.Atmosphere)
				m.Tone = longer(m.Tone, w.Tone)
				m.Backstory = longer(m.Backstory, w.Backstory)
				m.Themes = union(m.Themes, w.Themes, 0)
			}
		}
		if out.Storyline != nil && (merged.Storyline == nil || len(out.Storyline.Acts) > len(merged.Storyline.Acts)) {
			merged.Storyline = out.Storyline
		}
	}
	return merged
}

// parseStructured 单次 LLM 调用，把 userMessage 提取为结构化结果（不转草稿）。
// 角色卡混合解析（Parser.ParseCard）与自由文本分块提取（Parser.ParseText）共用此入口。
func (p *Parser) parseStructured(ctx context.Context, userMessage string) (*extractionOutput, error) {
	return p.parseStructuredWith(ctx, p.run, userMessage)
}

// parseStructuredWith 用指定 runner 执行单次提取/补全调用并解析 JSON 输出。
func (p *Parser) parseStructuredWith(ctx context.Context, run runner.Runner, userMessage string) (*extractionOutput, error) {
	if strings.TrimSpace(userMessage) == "" {
		return nil, fmt.Errorf("文本为空")
	}

	// 唯一会话 ID，避免 runner 内存会话串文本（同剧本分析的处理）
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	events, err := run.Run(ctx, "assetparse", "asset-parse-"+runID,
		model.NewUserMessage(userMessage),
	)
	if err != nil {
		return nil, fmt.Errorf("LLM 解析执行失败: %w", err)
	}

	var replyBuilder strings.Builder
	for ev := range events {
		if ev.Object == "chat.completion.chunk" {
			if len(ev.Response.Choices) > 0 {
				replyBuilder.WriteString(ev.Response.Choices[0].Delta.Content)
			}
		} else if ev.Object == "chat.completion" {
			if len(ev.Response.Choices) > 0 {
				replyBuilder.WriteString(ev.Response.Choices[0].Message.Content)
			}
		}
	}
	reply := replyBuilder.String()
	if reply == "" {
		return nil, fmt.Errorf("LLM 未生成回复")
	}

	jsonStr := extractJSONText(reply)
	if jsonStr == "" {
		return nil, fmt.Errorf("LLM 回复中未找到有效 JSON")
	}
	var out extractionOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return nil, fmt.Errorf("LLM 输出 JSON 解析失败: %w", err)
	}
	return &out, nil
}

// outputToDrafts 把 LLM 提取结果转换为素材草稿（payload 用世界实体结构序列化）。
func outputToDrafts(out *extractionOutput) []Draft {
	var drafts []Draft
	marshal := func(v any) json.RawMessage {
		data, _ := json.Marshal(v)
		return data
	}
	for _, c := range out.Characters {
		if c.Name == "" {
			continue
		}
		drafts = append(drafts, Draft{
			Kind: "character", Name: c.Name, Summary: c.Summary, Tags: c.Tags,
			Payload: marshal(map[string]any{
				"name": c.Name, "kind": "npc", "disposition": "neutral", "alive": true,
				"appearance": c.Appearance, "personality": c.Personality,
				"backstory": c.Backstory, "skills": c.Skills,
				"motivation": c.Motivation, "dialogue_style": c.DialogueStyle,
				"key_dialogue": c.KeyDialogue,
			}),
		})
	}
	if out.Worldview != nil {
		w := out.Worldview
		payload := marshal(map[string]any{
			"setting": w.Setting, "era": w.Era, "location": w.Location,
			"atmosphere": w.Atmosphere, "tone": w.Tone,
			"backstory": w.Backstory, "themes": w.Themes,
		})
		name := w.Name
		if name == "" {
			name = "导入的世界观"
		}
		drafts = append(drafts, Draft{
			Kind: "world", Name: name, Summary: w.Setting, Payload: payload,
		})
	}
	for _, l := range out.Locations {
		if l.Name == "" {
			continue
		}
		drafts = append(drafts, Draft{
			Kind: "location", Name: l.Name, Summary: l.Description,
			Payload: marshal(map[string]any{
				"name": l.Name, "description": l.Description, "atmosphere": l.Atmosphere,
				"danger": l.Danger, "points": l.Points,
			}),
		})
	}
	for _, it := range out.Items {
		if it.Name == "" {
			continue
		}
		typ := it.Type
		if typ == "" {
			typ = "other"
		}
		drafts = append(drafts, Draft{
			Kind: "item", Name: it.Name, Summary: it.Description, Tags: it.Tags,
			Payload: marshal(map[string]any{
				"name": it.Name, "type": typ, "description": it.Description, "tags": it.Tags,
			}),
		})
	}
	for _, f := range out.Factions {
		if f.Name == "" {
			continue
		}
		drafts = append(drafts, Draft{
			Kind: "faction", Name: f.Name, Summary: f.Description,
			Payload: marshal(map[string]any{
				"name": f.Name, "description": f.Description, "goals": f.Goals, "leader": f.Leader,
			}),
		})
	}
	if out.Storyline != nil && out.Storyline.Title != "" {
		acts := make([]map[string]any, 0, len(out.Storyline.Acts))
		for i, act := range out.Storyline.Acts {
			if act.Title == "" {
				continue
			}
			status := "pending"
			if len(acts) == 0 {
				status = "active"
			}
			acts = append(acts, map[string]any{
				"id": fmt.Sprintf("act_%d", i+1), "title": act.Title,
				"summary": act.Summary, "status": status,
			})
		}
		drafts = append(drafts, Draft{
			Kind: "storyline", Name: out.Storyline.Title, Summary: out.Storyline.Premise,
			Payload: marshal(map[string]any{
				"title": out.Storyline.Title, "premise": out.Storyline.Premise, "acts": acts,
			}),
		})
	}
	// 通用设定条目（仪式/时间线/习俗/规则等）：payload 与 world.LoreEntry 同形，
	// 入库为 lore 素材、生成世界/导入世界时落地为设定库条目（ID/Enabled 落地时补）。
	for _, e := range out.LoreEntries {
		if e.Title == "" || e.Content == "" {
			continue
		}
		category := e.Category
		if category == "" {
			category = "background"
		}
		position := e.Position
		if position == "" {
			position = "front"
		}
		summary := []rune(strings.TrimSpace(e.Content))
		if len(summary) > 80 {
			summary = summary[:80]
		}
		drafts = append(drafts, Draft{
			Kind: "lore", Name: e.Title, Summary: string(summary), Tags: e.Keys,
			Payload: marshal(map[string]any{
				"title": e.Title, "category": category, "keys": e.Keys,
				"content": e.Content, "position": position,
			}),
		})
	}
	return drafts
}

// extractJSONText 从可能包含 markdown 标记的回复中提取 JSON。
func extractJSONText(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "{") {
		return text
	}
	if idx := strings.Index(text, "```json"); idx >= 0 {
		start := idx + 7
		if end := strings.Index(text[start:], "```"); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	if idx := strings.Index(text, "```"); idx >= 0 {
		start := idx + 3
		if nl := strings.Index(text[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(text[start:], "```"); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	first := strings.Index(text, "{")
	last := strings.LastIndex(text, "}")
	if first >= 0 && last > first {
		return text[first : last+1]
	}
	return ""
}
