package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

func newAdminTestServer(t *testing.T, adminToken string) *httptest.Server {
	t.Helper()
	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions, nil)
	srv := NewServer(Config{UploadDir: t.TempDir()}, router, sessions)
	srv.SetAdmin(AdminDeps{Sessions: sessions, StartTime: time.Now()}, adminToken)
	return httptest.NewServer(srv.Handler())
}

func TestAdmin_StatusWithToken(t *testing.T) {
	ts := newAdminTestServer(t, "secret-token")
	defer ts.Close()

	// 无 Authorization → 401
	resp, err := http.Get(ts.URL + "/api/admin/status")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无 token 应返回 401: %d", resp.StatusCode)
	}

	// 带正确 token → 200
	req, _ := http.NewRequest("GET", ts.URL+"/api/admin/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("带 token 应返回 200: %d", resp.StatusCode)
	}
	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if status["version"] == nil || status["uptime"] == nil {
		t.Fatalf("状态响应缺少字段: %v", status)
	}
}

func TestAdmin_NoTokenOnlyLocal(t *testing.T) {
	ts := newAdminTestServer(t, "")
	defer ts.Close()

	// 未配置 token 时本机（127.0.0.1）可访问
	resp, err := http.Get(ts.URL + "/api/admin/status")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("未配置 token 时本机应可访问: %d", resp.StatusCode)
	}

	// 非本机 RemoteAddr 应被拒绝（直接测试中间件）
	sessions := core.NewSessionManager()
	a := &adminAPI{deps: AdminDeps{Sessions: sessions, StartTime: time.Now()}}
	handler := a.wrap(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("GET", "/api/admin/status", nil) // RemoteAddr = 192.0.2.1
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("非本机应返回 403: %d", rec.Code)
	}
}
