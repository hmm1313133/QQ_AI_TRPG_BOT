package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// newConfigTestServer 启动带真实 ConfigStore 的管理后台测试服务。
func newConfigTestServer(t *testing.T) (*httptest.Server, *config.Store) {
	t.Helper()
	cfgStore, err := config.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开配置库失败: %v", err)
	}
	t.Cleanup(func() { cfgStore.Close() })

	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions, nil)
	srv := NewServer(Config{UploadDir: t.TempDir()}, router, sessions)
	srv.SetAdmin(AdminDeps{Sessions: sessions, ConfigStore: cfgStore, StartTime: time.Now()}, "tok")
	return httptest.NewServer(srv.Handler()), cfgStore
}

func adminReq(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	return resp
}

func TestAdmin_ConfigGetMasksSecrets(t *testing.T) {
	ts, cfgStore := newConfigTestServer(t)
	defer ts.Close()

	cfgStore.Set(config.KeyLLMModel, "deepseek-v4-flash")
	cfgStore.Set(config.KeyQQSecret, "real-secret-value")

	resp := adminReq(t, "GET", ts.URL+"/api/admin/config", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET 应返回 200: %d", resp.StatusCode)
	}
	var list []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(list) != len(config.KeyRegistry) {
		t.Fatalf("应返回注册表全部键: got %d, want %d", len(list), len(config.KeyRegistry))
	}
	byKey := make(map[string]map[string]interface{}, len(list))
	for _, item := range list {
		byKey[item["key"].(string)] = item
	}
	if got := byKey[config.KeyLLMModel]["value"]; got != "deepseek-v4-flash" {
		t.Fatalf("普通键应返回明文: %v", got)
	}
	if got := byKey[config.KeyQQSecret]["value"]; got != config.SecretMask {
		t.Fatalf("敏感键应掩码: %v", got)
	}
	// 未设置值的敏感键返回空串而非掩码
	if got := byKey[config.KeyLLMAPIKey]["value"]; got != "" {
		t.Fatalf("空敏感值应返回空串: %v", got)
	}
	// 元数据齐全
	if byKey[config.KeyQQSecret]["secret"] != true || byKey[config.KeyQQSecret]["scope"] != config.ScopeBotRestart {
		t.Fatalf("敏感键元数据缺失: %+v", byKey[config.KeyQQSecret])
	}
}

func TestAdmin_ConfigSetWhitelistAndSecretSkip(t *testing.T) {
	ts, cfgStore := newConfigTestServer(t)
	defer ts.Close()

	// 未注册键 → 400
	resp := adminReq(t, "PUT", ts.URL+"/api/admin/config", `{"evil_key":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("未注册键应返回 400: %d", resp.StatusCode)
	}

	// 普通键正常写入
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/config", `{"llm_model":"deepseek-reasoner"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("普通键写入应 200: %d", resp.StatusCode)
	}
	if got := cfgStore.Get(config.KeyLLMModel, ""); got != "deepseek-reasoner" {
		t.Fatalf("写入未生效: %s", got)
	}

	// 敏感键：掩码原样传回 → 跳过不修改
	cfgStore.Set(config.KeyQQSecret, "real-secret-value")
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/config", `{"qq_secret":"`+config.SecretMask+`"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("掩码跳过应 200: %d", resp.StatusCode)
	}
	if got := cfgStore.Get(config.KeyQQSecret, ""); got != "real-secret-value" {
		t.Fatalf("掩码原样传回应不修改: %s", got)
	}

	// 敏感键：空值 → 跳过不修改
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/config", `{"qq_secret":""}`)
	resp.Body.Close()
	if got := cfgStore.Get(config.KeyQQSecret, ""); got != "real-secret-value" {
		t.Fatalf("空值应不修改: %s", got)
	}

	// 敏感键：真实新值 → 覆盖
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/config", `{"qq_secret":"new-secret"}`)
	resp.Body.Close()
	if got := cfgStore.Get(config.KeyQQSecret, ""); got != "new-secret" {
		t.Fatalf("新值应覆盖: %s", got)
	}
}
