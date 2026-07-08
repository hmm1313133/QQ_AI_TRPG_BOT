// 剧本分析文本访问工具：供 Phase 2 的 Extractor Agent 使用。
//
// 每个 Extractor 是工具调用 Agent，通过这 3 个 FunctionTool 自主读取
// 原文的相关段落，而非一次性接收全文，解决注意力稀疏问题。
//
// 工具通过 context 传递 TextAccessProvider（复用 internal/agent/tools.go 的 context key 模式）。
package script

import (
	"context"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const maxReadLines = 500 // read_text_segment 单次最多读取行数

// textAccessProviderKey 是 context key 类型，用于传递 TextAccessProvider。
type textAccessProviderKey struct{}

// withTextAccessProvider 将 TextAccessProvider 注入 context。
func withTextAccessProvider(ctx context.Context, provider *TextAccessProvider) context.Context {
	return context.WithValue(ctx, textAccessProviderKey{}, provider)
}

// getTextAccessProvider 从 context 中获取 TextAccessProvider。
func getTextAccessProvider(ctx context.Context) (*TextAccessProvider, error) {
	provider, ok := ctx.Value(textAccessProviderKey{}).(*TextAccessProvider)
	if !ok || provider == nil {
		return nil, fmt.Errorf("文本访问器未初始化")
	}
	return provider, nil
}

// TextAccessProvider 持有原文文本（按行分割），供工具函数访问。
type TextAccessProvider struct {
	lines []string // 原文按行分割（index 0 = 第1行）
	total int      // 总行数
}

// NewTextAccessProvider 创建文本访问器，按行分割原文。
func NewTextAccessProvider(text string) *TextAccessProvider {
	lines := strings.Split(text, "\n")
	return &TextAccessProvider{
		lines: lines,
		total: len(lines),
	}
}

// ReadLines 返回 [start, end] 行号范围（1-based）的文本。
func (p *TextAccessProvider) ReadLines(start, end int) (string, error) {
	if start < 1 {
		start = 1
	}
	if end > p.total {
		end = p.total
	}
	if start > end || start > p.total {
		return "", fmt.Errorf("无效的行号范围: %d-%d（总行数: %d）", start, end, p.total)
	}
	// 限制单次最多读取 maxReadLines 行
	if end-start+1 > maxReadLines {
		end = start + maxReadLines - 1
		if end > p.total {
			end = p.total
		}
	}
	selected := p.lines[start-1 : end]
	var sb strings.Builder
	for i, line := range selected {
		fmt.Fprintf(&sb, "%d: %s\n", start+i, line)
	}
	return sb.String(), nil
}

// SearchLines 搜索关键词，返回匹配行号+上下文（前后各2行）。
func (p *TextAccessProvider) SearchLines(keyword string) string {
	if keyword == "" {
		return "关键词为空"
	}
	lowerKeyword := strings.ToLower(keyword)
	var results []string
	matchCount := 0
	maxResults := 20 // 最多返回20个匹配结果
	for i, line := range p.lines {
		if strings.Contains(strings.ToLower(line), lowerKeyword) {
			matchCount++
			if len(results) >= maxResults {
				continue
			}
			lineNum := i + 1
			// 前后各2行上下文
			ctxStart := lineNum - 2
			if ctxStart < 1 {
				ctxStart = 1
			}
			ctxEnd := lineNum + 2
			if ctxEnd > p.total {
				ctxEnd = p.total
			}
			var sb strings.Builder
			for j := ctxStart; j <= ctxEnd; j++ {
				marker := "  "
				if j == lineNum {
					marker = ">>"
				}
				fmt.Fprintf(&sb, "%s %d: %s\n", marker, j, p.lines[j-1])
			}
			results = append(results, sb.String())
		}
	}
	if matchCount == 0 {
		return fmt.Sprintf("未找到关键词 \"%s\" 的匹配", keyword)
	}
	header := fmt.Sprintf("找到 %d 个匹配（显示前 %d 个）：\n\n", matchCount, len(results))
	return header + strings.Join(results, "\n---\n")
}

// Overview 返回文本整体结构概览。
func (p *TextAccessProvider) Overview() string {
	previewLines := 30
	if p.total < previewLines {
		previewLines = p.total
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "总行数: %d\n\n前 %d 行预览:\n", p.total, previewLines)
	for i := 0; i < previewLines; i++ {
		fmt.Fprintf(&sb, "%d: %s\n", i+1, p.lines[i])
	}
	if p.total > previewLines {
		fmt.Fprintf(&sb, "\n... (还有 %d 行)", p.total-previewLines)
	}
	return sb.String()
}

// --- read_text_segment tool ---

// ReadTextSegmentReq 是 read_text_segment 工具的请求。
type ReadTextSegmentReq struct {
	StartLine int `json:"start_line" jsonschema:"description=起始行号（从1开始），required"`
	EndLine   int `json:"end_line" jsonschema:"description=结束行号，required"`
}

// ReadTextSegmentRsp 是 read_text_segment 工具的响应。
type ReadTextSegmentRsp struct {
	Content   string `json:"content"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Truncated bool   `json:"truncated"`
}

// NewReadTextSegmentTool 创建文本段落读取工具。
func NewReadTextSegmentTool() tool.Tool {
	fn := func(ctx context.Context, req ReadTextSegmentReq) (ReadTextSegmentRsp, error) {
		provider, err := getTextAccessProvider(ctx)
		if err != nil {
			return ReadTextSegmentRsp{}, err
		}

		content, err := provider.ReadLines(req.StartLine, req.EndLine)
		if err != nil {
			return ReadTextSegmentRsp{}, err
		}

		// 检查是否被截断
		actualEnd := req.EndLine
		truncated := false
		if req.EndLine-req.StartLine+1 > maxReadLines {
			actualEnd = req.StartLine + maxReadLines - 1
			if actualEnd > provider.total {
				actualEnd = provider.total
			}
			truncated = true
		}
		if req.EndLine > provider.total {
			actualEnd = provider.total
		}

		return ReadTextSegmentRsp{
			Content:   content,
			StartLine: req.StartLine,
			EndLine:   actualEnd,
			Truncated: truncated,
		}, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("read_text_segment"),
		function.WithDescription(
			"按行号范围读取原文段落。行号从1开始，与 segment_map 中的行号一致。"+
				"单次最多读取500行。返回带行号的原文文本。"+
				"参数: start_line 是起始行号，end_line 是结束行号。"+
				"如果请求范围超过500行，结果会被截断并标记 truncated=true。"),
	)
}

// --- search_text tool ---

// SearchTextReq 是 search_text 工具的请求。
type SearchTextReq struct {
	Keyword string `json:"keyword" jsonschema:"description=搜索关键词，required"`
}

// SearchTextRsp 是 search_text 工具的响应。
type SearchTextRsp struct {
	Results   string `json:"results"`
	MatchCount int   `json:"match_count"`
}

// NewSearchTextTool 创建文本搜索工具。
func NewSearchTextTool() tool.Tool {
	fn := func(ctx context.Context, req SearchTextReq) (SearchTextRsp, error) {
		provider, err := getTextAccessProvider(ctx)
		if err != nil {
			return SearchTextRsp{}, err
		}

		results := provider.SearchLines(req.Keyword)
		return SearchTextRsp{
			Results:    results,
			MatchCount: 0, // 值在 results 文本中已包含
		}, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("search_text"),
		function.WithDescription(
			"在原文中搜索关键词，返回匹配行号和上下文（前后各2行）。"+
				"用于快速定位特定内容，如角色名、地点名、信件关键词等。"+
				"最多返回20个匹配结果。参数: keyword 是要搜索的关键词。"),
	)
}

// --- get_text_overview tool ---

// GetTextOverviewReq 是 get_text_overview 工具的请求。
type GetTextOverviewReq struct{}

// GetTextOverviewRsp 是 get_text_overview 工具的响应。
type GetTextOverviewRsp struct {
	Overview string `json:"overview"`
}

// NewGetTextOverviewTool 创建文本概览工具。
func NewGetTextOverviewTool() tool.Tool {
	fn := func(ctx context.Context, req GetTextOverviewReq) (GetTextOverviewRsp, error) {
		provider, err := getTextAccessProvider(ctx)
		if err != nil {
			return GetTextOverviewRsp{}, err
		}

		return GetTextOverviewRsp{
			Overview: provider.Overview(),
		}, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("get_text_overview"),
		function.WithDescription(
			"获取原文的整体结构概览，包括总行数和前30行预览。"+
				"用于了解文本的整体结构和开头内容。无参数。"),
	)
}

// NewTextAccessTools 创建所有文本访问工具。
// 返回的切片适合传给 llmagent.WithTools()。
func NewTextAccessTools() []tool.Tool {
	return []tool.Tool{
		NewReadTextSegmentTool(),
		NewSearchTextTool(),
		NewGetTextOverviewTool(),
	}
}
