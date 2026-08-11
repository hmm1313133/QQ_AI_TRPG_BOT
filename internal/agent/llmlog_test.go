// LLM 交互调试日志测试。
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogLLMInteraction_WritesJSONL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LLM_DEBUG_DIR", dir)

	logLLMInteraction(llmLogRecord{
		Kind: "narrator", Session: "s1",
		SystemPrompt: "系统提示词<测试>", UserMessage: "玩家消息",
		Reply: "叙事回复", ElapsedMs: 42,
	})

	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("应生成一个 jsonl 文件: matches=%v, err=%v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	// 不转义 HTML/中文，方便直接阅读
	if !strings.Contains(string(data), "系统提示词<测试>") {
		t.Fatalf("日志应保留原文: %s", data)
	}
	var rec llmLogRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("记录应为合法 JSON: %v", err)
	}
	if rec.Kind != "narrator" || rec.Reply != "叙事回复" || rec.ElapsedMs != 42 {
		t.Fatalf("记录字段不符: %+v", rec)
	}
	if rec.Time == "" {
		t.Fatal("应自动补时间戳")
	}
	if _, err := time.Parse(time.RFC3339, rec.Time); err != nil {
		t.Fatalf("时间戳应为 RFC3339: %v", err)
	}
}
