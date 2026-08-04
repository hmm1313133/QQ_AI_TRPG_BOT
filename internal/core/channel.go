package core

// Channel 消息渠道接口（QQ / Web 等）。
// 渠道负责收发消息并转换为 MessageContext，路由统一走 Router。
type Channel interface {
	ID() string
	Start() error
	Stop() error
}
