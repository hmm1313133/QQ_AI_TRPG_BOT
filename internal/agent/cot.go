// Package agent - 思维链（CoT）输出层。
//
// 世界开启 Style.EnableCoT 后，每轮在回复风格分区之后注入输出格式要求，
// 让模型先输出 <think> 思考段再输出正文；回收回复时剥离思考段，
// 玩家只见正文。内置默认指导仅作兜底，可被世界配置的 CoTGuide 覆盖。
package agent

import "strings"

// DefaultCoTGuide 内置默认思维链指导（EnableCoT 且 CoTGuide 为空时使用）。
const DefaultCoTGuide = `【输出格式要求】先输出 <think> 思考段（①检查判断：理解玩家行动与当前状态 ②核心推演：NPC 心理与剧情走向 ③描写渲染：确定本段描写重点），再输出 </think> 和正文叙事。思考段不会展示给玩家，正文不要提及思考段的存在。`

// CoTGuideFor 返回世界生效的思维链指导：自定义非空则替换默认文本（保留【输出格式要求】头）。
func CoTGuideFor(custom string) string {
	if strings.TrimSpace(custom) == "" {
		return DefaultCoTGuide
	}
	return "【输出格式要求】\n" + strings.TrimSpace(custom)
}

// StripThinking 剥离回复中的 <think>...</think> 思考段。
// 成对标签存在则移除并 TrimSpace；未闭合或不含标签则原样返回
// （防模型忘闭合导致空回复）。未启用 CoT 时是无害 no-op。
func StripThinking(reply string) string {
	start := strings.Index(reply, "<think>")
	if start < 0 {
		return reply
	}
	end := strings.Index(reply, "</think>")
	if end < 0 {
		return reply // 未闭合：原样返回
	}
	return strings.TrimSpace(reply[:start] + reply[end+len("</think>"):])
}
