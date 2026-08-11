// 素材 AI 补全：根据已有素材草稿，让 LLM 生成让世界更完整的补充素材。
//
// 与 ParseText 的差异：输入是已解析的结构化草稿（而非原始文本），
// 目标是"查漏补缺"（补对手/家人/关键地点/习俗规则/剧情钩子），不是提取。
// 输出复用 extractionOutput/outputToDrafts，草稿经预览确认后走原有入库/生成世界链路。
package assetparse

import (
	"context"
	"fmt"
	"strings"
)

// completeDigestRunes 单个草稿送入补全上下文的 payload 上限（防个别大素材挤占预算）。
const completeDigestRunes = 2000

// completeTotalRunes 已有素材摘要的总上限。
const completeTotalRunes = 20000

// CompleteDrafts 根据已有素材草稿生成补充素材草稿（不含与已有同名的条目）。
func (p *Parser) CompleteDrafts(ctx context.Context, drafts []Draft) ([]Draft, error) {
	if len(drafts) == 0 {
		return nil, fmt.Errorf("没有可参考的素材草稿")
	}
	if p.completeRun == nil {
		return nil, fmt.Errorf("AI 补全未配置")
	}
	digest := buildCompleteDigest(drafts)
	out, err := p.parseStructuredWith(ctx, p.completeRun,
		"以下是一个世界已有的素材清单。请按你的职责补全世界观，输出 JSON：\n\n---\n"+digest+"\n---")
	if err != nil {
		return nil, err
	}
	// 丢弃与已有素材同名的条目（LLM 可能复述已有内容）
	existing := make(map[string]bool, len(drafts))
	for _, d := range drafts {
		existing[d.Kind+"|"+strings.TrimSpace(d.Name)] = true
	}
	var completed []Draft
	for _, d := range outputToDrafts(out) {
		if existing[d.Kind+"|"+strings.TrimSpace(d.Name)] {
			continue
		}
		completed = append(completed, d)
	}
	if len(completed) == 0 {
		return nil, fmt.Errorf("AI 没有产出新的补充素材（可能素材已足够完整，可重试）")
	}
	return completed, nil
}

// buildCompleteDigest 把已有草稿序列化为补全上下文文本（kind/name/summary + 截断的 payload）。
func buildCompleteDigest(drafts []Draft) string {
	var sb strings.Builder
	total := 0
	for _, d := range drafts {
		line := fmt.Sprintf("【%s】%s", d.Kind, d.Name)
		if s := strings.TrimSpace(d.Summary); s != "" {
			line += "：" + s
		}
		if len(d.Payload) > 0 {
			pr := []rune(string(d.Payload))
			if len(pr) > completeDigestRunes {
				pr = pr[:completeDigestRunes]
			}
			line += "\n" + string(pr)
		}
		if total+len(line) > completeTotalRunes {
			break
		}
		sb.WriteString(line)
		sb.WriteString("\n\n")
		total += len(line)
	}
	return sb.String()
}
