// Package agent - LLM 交互调试日志。
//
// 开发期把每次 LLM 调用的完整输入（系统提示词 + 用户消息）与输出落盘为
// JSONL，便于本地排查提示词与注入问题：
//   - 默认写入 data/llm_debug/<日期>.jsonl（data/ 已被 .gitignore 忽略）
//   - 环境变量 LLM_DEBUG_LOG=0 关闭；LLM_DEBUG_DIR 自定义目录
//   - 单行一条记录：time/kind/session/system_prompt/user_message/reply/error/elapsed_ms
package agent

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// llmLogMessage 一条对话消息的快照（调试日志用）。
type llmLogMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// llmLogMessages 把 model.Message 列表转换为日志快照（含工具往返）。
func llmLogMessages(msgs []model.Message) []llmLogMessage {
	out := make([]llmLogMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == model.RoleSystem {
			continue // system 由 SystemPrompt 字段单独记录
		}
		content := m.Content
		if len(m.ToolCalls) > 0 {
			var names []string
			for _, tc := range m.ToolCalls {
				names = append(names, tc.Function.Name+"("+string(tc.Function.Arguments)+")")
			}
			content += "\n[tool_calls] " + strings.Join(names, ", ")
		}
		out = append(out, llmLogMessage{Role: string(m.Role), Content: content})
	}
	return out
}

// llmLogRecord 一次 LLM 调用的调试记录。
type llmLogRecord struct {
	Time         string          `json:"time"`
	Kind         string          `json:"kind"` // narrator / planner / 低频任务名（如 summary）
	Session      string          `json:"session,omitempty"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
	Messages     []llmLogMessage `json:"messages,omitempty"`     // narrator：完整消息列表（含历史与工具往返）
	UserMessage  string          `json:"user_message,omitempty"` // planner/低频任务：单条用户消息
	Reply        string          `json:"reply"`
	RawReply     string          `json:"raw_reply,omitempty"` // 剥离 CoT 思考段之前的原始输出
	Error        string          `json:"error,omitempty"`
	ElapsedMs    int64           `json:"elapsed_ms"`
}

var (
	llmLogMu  sync.Mutex
	llmLogOff = os.Getenv("LLM_DEBUG_LOG") == "0"
)

// logLLMInteraction 追加一条 LLM 交互记录（失败仅告警，不影响主流程）。
func cleanupLLMLogs(dir string, now time.Time, keepDays int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -keepDays)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func logLLMInteraction(rec llmLogRecord) {
	if llmLogOff {
		return
	}
	rec.Time = time.Now().Format(time.RFC3339)

	llmLogMu.Lock()
	defer llmLogMu.Unlock()

	dir := os.Getenv("LLM_DEBUG_DIR")
	if dir == "" {
		dir = filepath.Join("data", "llm_debug")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[LLMLog] 创建目录失败: %v", err)
		return
	}
	now := time.Now()
	cleanupLLMLogs(dir, now, 14)
	base := now.Format("2006-01-02")
	path := filepath.Join(dir, base+".jsonl")
	if info, statErr := os.Stat(path); statErr == nil && info.Size() >= 20*1024*1024 {
		path = filepath.Join(dir, base+"."+strconv.FormatInt(now.UnixNano(), 10)+".jsonl")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("[LLMLog] 打开日志失败: %v", err)
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false) // 保留中文与符号原文，方便直接阅读
	if err := enc.Encode(rec); err != nil {
		log.Printf("[LLMLog] 写入失败: %v", err)
	}
}
