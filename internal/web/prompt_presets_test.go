// 主提示词预设列表 API 测试（/api/chat/prompt-presets、/api/admin/prompt-presets）。
package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPromptPresetsEndpoints(t *testing.T) {
	srv, _, _ := newChatSessionTestServer(t)
	defer srv.Close()

	for _, path := range []string{"/api/chat/prompt-presets?token=t1", "/api/admin/prompt-presets"} {
		req, err := http.NewRequest("GET", srv.URL+path, nil)
		if err != nil {
			t.Fatalf("构造请求失败: %v", err)
		}
		if path == "/api/admin/prompt-presets" {
			req.Header.Set("Authorization", "Bearer tok")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("请求 %s 失败: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s 状态码应为 200，实际 %d", path, resp.StatusCode)
		}
		var presets []struct {
			Key     string `json:"key"`
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&presets); err != nil {
			resp.Body.Close()
			t.Fatalf("%s 响应解析失败: %v", path, err)
		}
		resp.Body.Close()
		if len(presets) < 2 || presets[0].Key != "default" || presets[0].Content == "" {
			t.Fatalf("%s 预设列表异常: %+v", path, presets)
		}
	}
}
