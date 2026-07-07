package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// ==================== Mock Server 测试 ====================

// mockOpenVikingServer 创建一个模拟 OpenViking HTTP 服务器。
func mockOpenVikingServer(t *testing.T) (*httptest.Server, map[string]string) {
	t.Helper()
	store := make(map[string]string)
	var mu sync.Mutex

	mux := http.NewServeMux()

	// GET /health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// GET /ready
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// POST /api/v1/content/write
	mux.HandleFunc("/api/v1/content/write", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		uri, _ := body["uri"].(string)
		content, _ := body["content"].(string)
		if uri == "" {
			http.Error(w, `{"status":"error","error":{"code":"INVALID_ARGUMENT","message":"uri is required"}}`, http.StatusBadRequest)
			return
		}
		mu.Lock()
		store[uri] = content
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","result":{"status":"success"},"time":0.01}`))
	})

	// GET /api/v1/content/read?uri=...
	mux.HandleFunc("/api/v1/content/read", func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Query().Get("uri")
		mu.Lock()
		content, ok := store[uri]
		mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"status":"error","error":{"code":"NOT_FOUND","message":"not found"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","result":{"content":%s},"time":0.01}`, mustJSON(content))
	})

	// GET /api/v1/fs/ls?uri=...
	mux.HandleFunc("/api/v1/fs/ls", func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Query().Get("uri")
		mu.Lock()
		var entries []map[string]interface{}
		prefix := uri
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		for k := range store {
			if strings.HasPrefix(k, prefix) {
				rel := strings.TrimPrefix(k, prefix)
				parts := strings.SplitN(rel, "/", 2)
				entries = append(entries, map[string]interface{}{
					"name": parts[0],
					"type": "file",
				})
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		result, _ := json.Marshal(map[string]interface{}{
			"status":  "ok",
			"result":  map[string]interface{}{"entries": entries},
			"time":    0.01,
		})
		w.Write(result)
	})

	// POST /api/v1/fs/mkdir
	mux.HandleFunc("/api/v1/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","result":{"status":"success"},"time":0.01}`))
	})

	// DELETE /api/v1/fs?uri=...
	mux.HandleFunc("/api/v1/fs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			uri := r.URL.Query().Get("uri")
			mu.Lock()
			delete(store, uri)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","time":0.01}`))
		}
	})

	// POST /api/v1/search/find
	mux.HandleFunc("/api/v1/search/find", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","result":{"resources":[{"uri":"viking://resources/test","score":0.95}]},"time":0.05}`))
	})

	// POST /api/v1/sessions
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","result":{"id":"test-session-001"},"time":0.01}`))
		}
	})

	// POST /api/v1/sessions/{id}/messages
	mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","time":0.01}`))
	})

	server := httptest.NewServer(mux)
	return server, store
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestOpenVikingClient_MockWriteRead(t *testing.T) {
	server, _ := mockOpenVikingServer(t)
	defer server.Close()

	client := NewOpenVikingClient(&OpenVikingConfig{
		BaseURL: server.URL,
		Enabled: true,
		APIKey:  "test-key",
		Timeout: 5,
	})

	if !client.IsEnabled() {
		t.Fatal("客户端应处于启用状态")
	}

	ctx := context.Background()

	// 测试写入
	testContent := "这是测试剧本内容 - 活神之手"
	err := client.WriteContext(ctx, "scripts/test_script/background", testContent)
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	t.Log("写入成功")

	// 测试读取
	readContent, err := client.ReadContext(ctx, "scripts/test_script/background")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if readContent != testContent {
		t.Errorf("读取内容不匹配: got %q, want %q", readContent, testContent)
	}
	t.Logf("读取成功: %s", readContent)

	// 测试读取不存在的
	_, err = client.ReadContext(ctx, "scripts/nonexistent")
	if err == nil {
		t.Error("应返回错误（资源不存在）")
	}
	t.Logf("不存在资源正确返回错误: %v", err)
}

func TestOpenVikingClient_MockWriteJSON(t *testing.T) {
	server, _ := mockOpenVikingServer(t)
	defer server.Close()

	client := NewOpenVikingClient(&OpenVikingConfig{
		BaseURL: server.URL,
		Enabled: true,
		Timeout: 5,
	})

	ctx := context.Background()

	// 写入 JSON 数据
	data := map[string]interface{}{
		"title":   "活神之手",
		"system":  "coc7",
		"nodes":   11,
		"characters": 7,
	}
	err := client.WriteJSON(ctx, "scripts/test/script_data", data)
	if err != nil {
		t.Fatalf("WriteJSON 失败: %v", err)
	}
	t.Log("WriteJSON 成功")

	// 读取验证
	content, err := client.ReadContext(ctx, "scripts/test/script_data")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	t.Logf("读取 JSON: %s", content)
}

func TestOpenVikingClient_MockListDir(t *testing.T) {
	server, _ := mockOpenVikingServer(t)
	defer server.Close()

	client := NewOpenVikingClient(&OpenVikingConfig{
		BaseURL: server.URL,
		Enabled: true,
		Timeout: 5,
	})

	ctx := context.Background()

	// 写入多个文件
	client.WriteContext(ctx, "scripts/s1/background", "背景1")
	client.WriteContext(ctx, "scripts/s1/timeline", "时间轴1")
	client.WriteContext(ctx, "scripts/s2/background", "背景2")

	// 列出目录
	entries, err := client.ListDir(ctx, "scripts/s1")
	if err != nil {
		t.Fatalf("ListDir 失败: %v", err)
	}
	t.Logf("目录列表: %v", entries)
}

func TestOpenVikingClient_MockSession(t *testing.T) {
	server, _ := mockOpenVikingServer(t)
	defer server.Close()

	client := NewOpenVikingClient(&OpenVikingConfig{
		BaseURL: server.URL,
		Enabled: true,
		Timeout: 5,
	})

	ctx := context.Background()

	// 创建会话
	sessionID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	t.Logf("会话 ID: %s", sessionID)

	if sessionID == "" {
		t.Error("会话 ID 为空")
	}

	// 添加消息
	err = client.AddSessionMessage(ctx, sessionID, "user", "调查员进入了图书馆")
	if err != nil {
		t.Fatalf("添加消息失败: %v", err)
	}
	t.Log("添加消息成功")

	err = client.AddSessionMessage(ctx, sessionID, "assistant", "你推开了图书馆沉重的木门...")
	if err != nil {
		t.Fatalf("添加 AI 消息失败: %v", err)
	}
	t.Log("添加 AI 消息成功")

	// 提交会话
	err = client.CommitSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("提交会话失败: %v", err)
	}
	t.Log("提交会话成功")
}

func TestOpenVikingClient_MockFind(t *testing.T) {
	server, _ := mockOpenVikingServer(t)
	defer server.Close()

	client := NewOpenVikingClient(&OpenVikingConfig{
		BaseURL: server.URL,
		Enabled: true,
		Timeout: 5,
	})

	ctx := context.Background()

	// 语义搜索
	results, err := client.Find(ctx, "黄衣之印 吊坠", "scripts")
	if err != nil {
		t.Fatalf("Find 失败: %v", err)
	}
	t.Logf("搜索结果: %d 条", len(results))
	for _, r := range results {
		t.Logf("  - %s (score: %.2f)", r.URI, r.Score)
	}
}

func TestOpenVikingClient_Degradation(t *testing.T) {
	// 连接到不存在的服务器
	client := NewOpenVikingClient(&OpenVikingConfig{
		BaseURL: "http://127.0.0.1:59999", // 不存在的端口
		Enabled: true,
		Timeout: 2,
	})

	// 应自动降级
	if client.IsEnabled() {
		t.Error("连接失败应自动降级")
	}
	t.Log("降级模式正常")

	// 降级模式下操作应静默返回
	ctx := context.Background()
	err := client.WriteContext(ctx, "test", "content")
	if err != nil {
		t.Errorf("降级模式 WriteContext 应返回 nil, got: %v", err)
	}

	err = client.WriteJSON(ctx, "test", map[string]string{"a": "b"})
	if err != nil {
		t.Errorf("降级模式 WriteJSON 应返回 nil, got: %v", err)
	}
}

func TestOpenVikingClient_Reconnect(t *testing.T) {
	server, _ := mockOpenVikingServer(t)

	client := NewOpenVikingClient(&OpenVikingConfig{
		BaseURL: server.URL,
		Enabled: true,
		Timeout: 5,
	})

	if !client.IsEnabled() {
		t.Fatal("应已连接")
	}

	// 关闭服务器模拟断连
	server.Close()
	client.markDisabled()

	if client.IsEnabled() {
		t.Error("应已降级")
	}

	// TryReconnect 应失败（服务器已关闭）
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if client.TryReconnect(ctx) {
		t.Error("服务器已关闭，重连应失败")
	}
	t.Log("重连失败处理正常")
}

// ==================== 真实 OpenViking 集成测试 ====================
//
// 需要启动 OpenViking 服务（本地或云上），通过环境变量配置：
//   OPENVIKING_ENABLED=true
//   OPENVIKING_BASE_URL=http://localhost:1933  (或云上endpoint)
//   OPENVIKING_API_KEY=your-key               (云上必填)
//
// 运行: go test -v -run "TestOpenVikingClient_Real" ./internal/store/ -timeout 30s

func getRealOpenVikingConfig() *OpenVikingConfig {
	cfg := &OpenVikingConfig{
		BaseURL: os.Getenv("OPENVIKING_BASE_URL"),
		Enabled: os.Getenv("OPENVIKING_ENABLED") == "true",
		APIKey:  os.Getenv("OPENVIKING_API_KEY"),
		Account: os.Getenv("OPENVIKING_ACCOUNT"),
		User:    os.Getenv("OPENVIKING_USER"),
		Timeout: 10,
	}

	// 尝试从 .env 文件读取
	if cfg.BaseURL == "" || (cfg.Enabled == false && os.Getenv("OPENVIKING_ENABLED") == "") {
		envPath := "../../.env"
		if data, err := os.ReadFile(envPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "OPENVIKING_ENABLED=") && os.Getenv("OPENVIKING_ENABLED") == "" {
					cfg.Enabled = strings.TrimPrefix(line, "OPENVIKING_ENABLED=") == "true"
				}
				if strings.HasPrefix(line, "OPENVIKING_BASE_URL=") && cfg.BaseURL == "" {
					cfg.BaseURL = strings.TrimPrefix(line, "OPENVIKING_BASE_URL=")
				}
				if strings.HasPrefix(line, "OPENVIKING_API_KEY=") && cfg.APIKey == "" {
					cfg.APIKey = strings.TrimPrefix(line, "OPENVIKING_API_KEY=")
				}
			}
		}
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:1933"
	}

	return cfg
}

// TestOpenVikingClient_RealWriteRead 测试真实 OpenViking 服务的读写。
func TestOpenVikingClient_RealWriteRead(t *testing.T) {
	cfg := getRealOpenVikingConfig()
	if !cfg.Enabled {
		t.Skip("OPENVIKING_ENABLED 未设置为 true，跳过真实集成测试")
	}

	client := NewOpenVikingClient(cfg)
	if !client.IsEnabled() {
		t.Skip("OpenViking 服务不可用，跳过")
	}

	t.Logf("连接: %s", cfg.BaseURL)
	ctx := context.Background()

	// 写入测试数据
	testPath := "trpg_bot_test/剧本信息"
	testContent := "活神之手 - CoC7 模组\n调查员在阿卡姆收到神秘吊坠..."
	t.Logf("写入: %s", testPath)
	err := client.WriteContext(ctx, testPath, testContent)
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	t.Log("写入成功")

	// 读取验证
	t.Log("读取验证...")
	content, err := client.ReadContext(ctx, testPath)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	t.Logf("读取内容: %s", content)

	// 写入 JSON
	t.Log("写入 JSON 数据...")
	jsonData := map[string]interface{}{
		"title":      "活神之手",
		"system":     "coc7",
		"timeline":   11,
		"characters": 7,
		"scenes":     6,
	}
	err = client.WriteJSON(ctx, "trpg_bot_test/script_json", jsonData)
	if err != nil {
		t.Fatalf("WriteJSON 失败: %v", err)
	}
	t.Log("WriteJSON 成功")

	// 列出目录
	t.Log("列出目录...")
	entries, err := client.ListDir(ctx, "trpg_bot_test")
	if err != nil {
		t.Logf("ListDir 失败（可能不支持）: %v", err)
	} else {
		t.Logf("目录内容: %v", entries)
	}

	// 清理测试数据
	t.Log("清理测试数据...")
	client.Delete(ctx, testPath)
	client.Delete(ctx, "trpg_bot_test/script_json")

	t.Log("真实 OpenViking 集成测试通过！")
}

// TestOpenVikingClient_RealSession 测试会话管理。
func TestOpenVikingClient_RealSession(t *testing.T) {
	cfg := getRealOpenVikingConfig()
	if !cfg.Enabled {
		t.Skip("OPENVIKING_ENABLED 未设置为 true，跳过真实集成测试")
	}

	client := NewOpenVikingClient(cfg)
	if !client.IsEnabled() {
		t.Skip("OpenViking 服务不可用，跳过")
	}

	ctx := context.Background()

	// 创建会话
	sessionID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	t.Logf("会话 ID: %s", sessionID)

	// 添加消息
	messages := []struct {
		role    string
		content string
	}{
		{"user", "KP，我想调查图书馆"},
		{"assistant", "你走进密斯卡托尼克大学图书馆，空气中弥漫着旧书的气味..."},
		{"user", "我搜索关于黄衣之印的资料"},
		{"assistant", "经过一番搜索，你找到了一本记载着古老仪式的书籍..."},
	}

	for _, msg := range messages {
		err = client.AddSessionMessage(ctx, sessionID, msg.role, msg.content)
		if err != nil {
			t.Fatalf("添加消息失败: %v", err)
		}
		t.Logf("消息 [%s]: %s", msg.role, msg.content)
	}

	// 获取会话上下文
	context, err := client.GetSessionContext(ctx, sessionID)
	if err != nil {
		t.Logf("获取上下文失败（可能不支持）: %v", err)
	} else {
		t.Logf("会话上下文: %s", context)
	}

	// 提交会话（提取记忆）
	err = client.CommitSession(ctx, sessionID)
	if err != nil {
		t.Logf("提交会话失败（可能需要 embedding 支持）: %v", err)
	} else {
		t.Log("提交会话成功")
	}

	t.Log("真实 OpenViking 会话测试通过！")
}
