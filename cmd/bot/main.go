// Package main 是 QQ AI TRPG Bot 的程序入口。
//
// 架构分两层:
//   - 功能层 (Handler): 基于 Go 代码的确定性功能 (骰子/模式切换/日志管理)
//   - Agent 层 (trpc-agent-go): AI 能力 (KP/DM 主持)
//   - 联动: 通过 Session 共享状态 + Hook 自动记录日志
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"trpc.group/trpc-go/trpc-go/log"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/bot"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/handler"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/gamelog"
)

func main() {
	// 1. 初始化核心组件
	plugins := core.NewPluginManager()
	sessions := core.NewSessionManager()
	gameLogger := gamelog.NewGameLogger()

	// 2. 初始化 TRPG 引擎（功能层依赖）
	trpgEngine := trpg.NewEngine()
	_ = trpgEngine // 后续角色卡等 Handler 会使用

	// 3. 初始化 AI Agent 层 (trpc-agent-go + DeepSeek)
	kpAgent, err := agent.NewKPAgent(&agent.Config{
		LLMProvider:  getEnv("LLM_PROVIDER", "deepseek"),
		LLMModel:     getEnv("LLM_MODEL", "deepseek-chat"),
		LLMAPIKey:    os.Getenv("LLM_API_KEY"),
		LLMBaseURL:   getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		MaxTokens:    4096,
		Temperature:  0.8,
		MemoryWindow: 20,
	}, sessions)
	if err != nil {
		log.Fatalf("初始化 AI Agent 失败: %v", err)
	}

	// 4. 注册 AI Agent
	if err := plugins.RegisterAgent(kpAgent); err != nil {
		log.Fatalf("注册 Agent 失败: %v", err)
	}

	// 5. 注册功能层 Handler (指令处理器)
	plugins.RegisterHandler(handler.NewHelpHandler())
	plugins.RegisterHandler(handler.NewDiceHandler())
	plugins.RegisterHandler(handler.NewModeHandler(sessions))
	plugins.RegisterHandler(handler.NewLogHandler(gameLogger))

	// 6. 注册 Hook (联动机制: 自动日志记录)
	// 在 TRPG 模式下，GameLogger 会自动记录玩家发言和 AI 回复
	// 注意: 日志记录已在 bot.route 中直接调用 gameLogger，无需额外 Hook

	// 7. 初始化 QQ Bot，注入所有组件
	qqBot, err := bot.NewBot(&bot.Config{
		AppID:        os.Getenv("QQ_BOT_APPID"),
		ClientSecret: os.Getenv("QQ_BOT_SECRET"),
	}, plugins, sessions, gameLogger)
	if err != nil {
		log.Fatalf("初始化 QQ Bot 失败: %v", err)
	}

	// 8. 启动
	if err := qqBot.Start(); err != nil {
		log.Fatalf("启动 Bot 失败: %v", err)
	}
	log.Infof("QQ AI TRPG Bot 已启动")
	log.Infof("已注册 Handler: %d, Agent: %d", 4, 1)
	log.Infof("架构: 功能层(Handler) + AI层(Agent) + 联动(Hook/Session)")

	// 9. 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	fmt.Printf("\n收到信号 %v，正在关闭...\n", sig)
	if err := qqBot.Stop(); err != nil {
		log.Errorf("关闭 Bot 出错: %v", err)
	}
	log.Infof("QQ AI TRPG Bot 已停止")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
