// 自由文本素材解析：单 Agent LLM 提取（设计 §11.4）。
//
// 与剧本三阶段流水线的差异：素材文本（角色卡/设定集量级）远短于 TRPG 模组，
// 无需分段索引与逐字保留，单 Agent 一次提取即可。
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

// maxInputRunes 输入文本上限（超出截断，防止打爆上下文）。
const maxInputRunes = 60000

// Parser 自由文本素材解析器（单 LLM Agent）。
type Parser struct {
	agent *llmagent.LLMAgent
	run   runner.Runner
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
	return &Parser{
		agent: agent,
		run:   runner.NewRunner("asset-parser", agent),
	}, nil
}

// extractedCharacter LLM 提取的单个角色（角色卡整理回写时按名字匹配）。
type extractedCharacter struct {
	Name        string   `json:"name"`
	Summary     string   `json:"summary"`
	Appearance  string   `json:"appearance"`
	Personality string   `json:"personality"`
	Backstory   string   `json:"backstory"`
	Skills      []string `json:"skills"`
	Tags        []string `json:"tags"`
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

// extractionOutput LLM 提取输出的 JSON 结构。
type extractionOutput struct {
	Characters []extractedCharacter `json:"characters"`
	Worldview  *extractedWorldview  `json:"worldview"`
	Locations []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Atmosphere  string   `json:"atmosphere"`
		Danger      string   `json:"danger"`
		Points      []string `json:"points"`
	} `json:"locations"`
	Items []struct {
		Name        string   `json:"name"`
		Type        string   `json:"type"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	} `json:"items"`
	Factions []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Goals       []string `json:"goals"`
		Leader      string   `json:"leader"`
	} `json:"factions"`
	Storyline *struct {
		Title   string `json:"title"`
		Premise string `json:"premise"`
		Acts    []struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
		} `json:"acts"`
	} `json:"storyline"`
}

// ParseText 用 LLM 从自由文本中提取素材草稿。
func (p *Parser) ParseText(ctx context.Context, text string) (*Result, error) {
	out, err := p.parseStructured(ctx, text)
	if err != nil {
		return nil, err
	}
	res := &Result{Parser: "llm"}
	res.Drafts = outputToDrafts(out)
	if len(res.Drafts) == 0 {
		return nil, fmt.Errorf("未能从文本中识别出任何素材")
	}
	return res, nil
}

// parseStructured 单次 LLM 调用，把自由文本提取为结构化结果（不转草稿）。
// 角色卡混合解析（Parser.ParseCard）复用此入口。
func (p *Parser) parseStructured(ctx context.Context, text string) (*extractionOutput, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("文本为空")
	}
	if runes := []rune(text); len(runes) > maxInputRunes {
		text = string(runes[:maxInputRunes])
		log.Printf("[AssetParser] 输入超长，截断至 %d 字符", maxInputRunes)
	}

	// 唯一会话 ID，避免 runner 内存会话串文本（同剧本分析的处理）
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	events, err := p.run.Run(ctx, "assetparse", "asset-parse-"+runID,
		model.NewUserMessage("请从以下文本中提取创作素材，输出 JSON：\n\n---\n"+text+"\n---"),
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
