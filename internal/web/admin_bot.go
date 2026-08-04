// QQ 机器人管理 API（设计见《管理后台扩展设计.md》2.1）。
//
// BotController 接口定义在 web 包以避免依赖环，由 internal/bot.Bot 实现。
// 状态接口只暴露 AppID，Secret 永不回包。
package web

import (
	"log"
	"net/http"
	"time"
)

// BotStatus QQ 机器人运行状态（GET /api/admin/bot 响应）。
type BotStatus struct {
	Running         bool      `json:"running"`          // 进程内是否已启动
	Connected       bool      `json:"connected"`        // WebSocket 是否已连接
	AppID           string    `json:"app_id,omitempty"` // 当前 AppID（Secret 不返回）
	StartedAt       string    `json:"started_at,omitempty"`
	Uptime          string    `json:"uptime,omitempty"`
	LastConnectedAt time.Time `json:"last_connected_at,omitempty"`
	ReconnectCount  uint64    `json:"reconnect_count"` // 断线重连次数
	RxCount         uint64    `json:"rx_count"`        // 累计接收消息数
	TxCount         uint64    `json:"tx_count"`        // 累计发送消息数
}

// BotController QQ 机器人生命周期控制接口（internal/bot.Bot 实现）。
// Start/Stop 幂等；Restart 重读凭证使新配置生效。
type BotController interface {
	Status() BotStatus
	Start() error
	Stop() error
	Restart() error
}

// handleBotStatus 返回机器人运行状态。
func (a *adminAPI) handleBotStatus(w http.ResponseWriter, r *http.Request) {
	if a.deps.Bot == nil {
		http.Error(w, "QQ 机器人未接入", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, a.deps.Bot.Status())
}

// handleBotStart 启动机器人（幂等）。
func (a *adminAPI) handleBotStart(w http.ResponseWriter, r *http.Request) {
	a.botLifecycle(w, "启动", func(c BotController) error { return c.Start() })
}

// handleBotStop 停止机器人（幂等）。
func (a *adminAPI) handleBotStop(w http.ResponseWriter, r *http.Request) {
	a.botLifecycle(w, "停止", func(c BotController) error { return c.Stop() })
}

// handleBotRestart 重启机器人（重读凭证）。
func (a *adminAPI) handleBotRestart(w http.ResponseWriter, r *http.Request) {
	a.botLifecycle(w, "重启", func(c BotController) error { return c.Restart() })
}

// botLifecycle 生命周期控制公共逻辑：nil 检查 + 错误处理 + 日志。
func (a *adminAPI) botLifecycle(w http.ResponseWriter, action string, fn func(BotController) error) {
	if a.deps.Bot == nil {
		http.Error(w, "QQ 机器人未接入", http.StatusServiceUnavailable)
		return
	}
	if err := fn(a.deps.Bot); err != nil {
		http.Error(w, action+"失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[Admin] 机器人%s", action)
	writeJSON(w, map[string]string{"message": "已" + action})
}
