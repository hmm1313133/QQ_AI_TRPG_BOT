// AI 补全的上下文摘要纯函数测试。
package assetparse

import (
	"encoding/json"
	"strings"
	"testing"
)

// buildCompleteDigest：包含 kind/name/summary 与 payload，超长 payload 截断。
func TestBuildCompleteDigest(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"name": "王婶", "personality": "热心"})
	drafts := []Draft{
		{Kind: "character", Name: "王婶", Summary: "小卖部老板娘", Payload: payload},
		{Kind: "lore", Name: "祭山神", Payload: json.RawMessage(`{"content":"` + strings.Repeat("长", 5000) + `"}`)},
	}
	digest := buildCompleteDigest(drafts)
	if !strings.Contains(digest, "【character】王婶：小卖部老板娘") {
		t.Fatalf("摘要应含 kind/name/summary: %q", digest)
	}
	if !strings.Contains(digest, "热心") {
		t.Fatalf("摘要应含 payload 内容: %q", digest)
	}
	// 超长 payload 被截断（5000 字 > 2000 上限）
	if strings.Contains(digest, strings.Repeat("长", 3000)) {
		t.Fatal("超长 payload 应截断")
	}
	if !strings.Contains(digest, "【lore】祭山神") {
		t.Fatalf("lore 草稿应入摘要: %q", digest)
	}
}
