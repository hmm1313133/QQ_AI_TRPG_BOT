package web

import (
	"fmt"
	"log"
	"os"

	"trpc.group/trpc-go/trpc-go"
	thttp "trpc.group/trpc-go/trpc-go/http"
)

// trpcServiceName Web 渠道在 trpc 框架内的命名服务名（与 trpc_go.yaml 中一致）。
const trpcServiceName = "trpc.trpg.web.Admin"

// StartTrpc 以 trpc-go 泛 HTTP 标准服务托管 Web 渠道（标准化接入）。
// 监听地址/端口/超时等由 trpc_go.yaml 的 server.service 段注入
//（confPath 传入，等价于命令行 -conf flag）。
// 底层是标准库 net/http.Server，REST / SPA / WebSocket 全链路可用
//（链路验证见 trpc_spike_test.go）。
func (s *Server) StartTrpc(confPath string) (err error) {
	if err := os.MkdirAll(s.cfg.UploadDir, 0755); err != nil {
		return err
	}

	// trpc.NewServer 在配置错误时按框架约定 panic，转成 error 避免拖垮整个进程
	defer func() {
		if r := recover(); r != nil {
			s.trpcServer = nil
			err = fmt.Errorf("trpc 服务初始化失败（检查 %s 的 server.service 配置）: %v", confPath, r)
		}
	}()

	trpc.ServerConfigPath = confPath
	ts := trpc.NewServer()
	thttp.RegisterNoProtocolServiceMux(ts.Service(trpcServiceName), s.buildMux())
	s.trpcServer = ts

	addr := trpcServiceAddr()
	go func() {
		log.Printf("[Web] 聊天与管理服务已启动（trpc-go 泛 HTTP，配置: %s）: http://%s", confPath, addr)
		if err := ts.Serve(); err != nil {
			log.Printf("[Web] trpc HTTP 服务出错: %v", err)
		}
	}()
	return nil
}

// trpcServiceAddr 从 trpc 全局配置读取 Web 服务的监听地址（仅用于日志展示）。
func trpcServiceAddr() string {
	for _, svc := range trpc.GlobalConfig().Server.Service {
		if svc.Name == trpcServiceName {
			return svc.Address
		}
	}
	return "(未在 server.service 中配置)"
}

// stopTrpc 停止 trpc 托管（由 Stop 调用）。
func (s *Server) stopTrpc() error {
	if s.trpcServer != nil {
		return s.trpcServer.Close(nil)
	}
	return nil
}
