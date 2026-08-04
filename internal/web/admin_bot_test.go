package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// fakeBotController 记录生命周期调用次数的 BotController 假实现。
type fakeBotController struct {
	status                  BotStatus
	err                     error
	starts, stops, restarts int
}

func (f *fakeBotController) Status() BotStatus { return f.status }
func (f *fakeBotController) Start() error      { f.starts++; return f.err }
func (f *fakeBotController) Stop() error       { f.stops++; return f.err }
func (f *fakeBotController) Restart() error    { f.restarts++; return f.err }

func newBotTestServer(t *testing.T, bot BotController) *httptest.Server {
	t.Helper()
	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions, nil)
	srv := NewServer(Config{UploadDir: t.TempDir()}, router, sessions)
	srv.SetAdmin(AdminDeps{Sessions: sessions, Bot: bot, StartTime: time.Now()}, "")
	return httptest.NewServer(srv.Handler())
}

func TestAdmin_BotStatus(t *testing.T) {
	fake := &fakeBotController{status: BotStatus{
		Running: true, Connected: true, AppID: "appid-1",
		Uptime: "1m0s", ReconnectCount: 2, RxCount: 10, TxCount: 8,
	}}
	ts := newBotTestServer(t, fake)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/admin/bot")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态接口应返回 200: %d", resp.StatusCode)
	}
	var st BotStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if !st.Running || !st.Connected || st.AppID != "appid-1" || st.RxCount != 10 || st.TxCount != 8 || st.ReconnectCount != 2 {
		t.Fatalf("状态字段不符: %+v", st)
	}
}

func TestAdmin_BotLifecycle(t *testing.T) {
	fake := &fakeBotController{}
	ts := newBotTestServer(t, fake)
	defer ts.Close()

	for _, ep := range []string{"start", "stop", "restart"} {
		resp, err := http.Post(ts.URL+"/api/admin/bot/"+ep, "", nil)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s 应返回 200: %d", ep, resp.StatusCode)
		}
	}
	if fake.starts != 1 || fake.stops != 1 || fake.restarts != 1 {
		t.Fatalf("生命周期调用次数不符: start=%d stop=%d restart=%d", fake.starts, fake.stops, fake.restarts)
	}
}

func TestAdmin_BotLifecycleError(t *testing.T) {
	fake := &fakeBotController{err: errors.New("QQ 凭证未配置")}
	ts := newBotTestServer(t, fake)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/admin/bot/start", "", nil)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("启动失败应返回 500: %d", resp.StatusCode)
	}
}

func TestAdmin_BotNotAttached(t *testing.T) {
	ts := newBotTestServer(t, nil)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/admin/bot")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("未接入机器人应返回 503: %d", resp.StatusCode)
	}

	resp, err = http.Post(ts.URL+"/api/admin/bot/restart", "", nil)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("未接入机器人时 restart 应返回 503: %d", resp.StatusCode)
	}
}
