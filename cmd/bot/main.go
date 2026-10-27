// Package main is the entry point for QQ AI TRPG Bot.
//
// Architecture:
//   - Core Layer (core): session/router/plugin abstractions
//   - World Layer (world): deterministic world state engine (single writer)
//   - Agent Layer (trpc-agent-go): AI capabilities (Director -> Narrator pipeline)
//   - Web Layer (web): chat WebSocket + admin REST API（唯一渠道）
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"trpc.group/trpc-go/trpc-go/log"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/assetparse"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/memory"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/web"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func main() {
	// 1. Initialize core components
	plugins := core.NewPluginManager()
	sessions := core.NewSessionManager()

	// 2. Open shared data DB（世界状态/素材库/存档/人设共用 data/app.db，
	// 见《世界编辑器与素材联动设计.md》§9；与配置库各持连接，busy_timeout 错开写入）
	dataDB, err := world.OpenSQLite(getEnv("CONFIG_DB", "./data/app.db"))
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer dataDB.Close()

	// 4. Initialize world engine（世界引擎：状态唯一真相；SQLite 存储 + 旧 JSON 目录自动迁移）
	worldDir := getEnv("WORLD_DIR", "./data/worlds")
	worldRepo, err := world.NewSQLiteRepository(dataDB)
	if err != nil {
		log.Fatalf("初始化世界状态存储失败: %v", err)
	}
	migrateLog := func(format string, args ...any) { log.Infof(format, args...) }
	if _, err := world.MigrateJSONDir(worldRepo, worldDir, migrateLog); err != nil {
		log.Errorf("迁移旧世界状态失败: %v", err)
	}
	assetStore, err := world.NewAssetStore(dataDB)
	if err != nil {
		log.Fatalf("初始化素材库失败: %v", err)
	}
	chatHistory, err := web.NewChatHistoryStore(dataDB)
	if err != nil {
		log.Fatalf("初始化聊天记录存储失败: %v", err)
	}
	personaStore, err := persona.NewStore(dataDB)
	if err != nil {
		log.Fatalf("初始化玩家人设存储失败: %v", err)
	}
	// 长期记忆存储（world_memory + FTS5，与世界状态/配置共用 app.db）
	memStore, err := memory.NewStore(dataDB)
	if err != nil {
		log.Fatalf("初始化长期记忆存储失败: %v", err)
	}
	worldEngine := world.NewEngine(worldRepo)

	// 5. Initialize asset parser（素材 LLM 解析器；设计 §11.4）
	assetParser, err := assetparse.NewParser(&assetparse.Config{
		LLMModel:    getEnv("LLM_MODEL", "deepseek-flash"),
		LLMAPIKey:   os.Getenv("LLM_API_KEY"),
		LLMBaseURL:  getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		MaxTokens:   8192,
		Temperature: 0.3,
	})
	if err != nil {
		log.Errorf("初始化素材解析器失败（素材解析功能不可用）: %v", err)
	}

	// 6. Initialize runtime config store (SQLite, 运行时配置热更新)
	// 提前到 Agent 构造之前：Director/Narrator 的温度等"进程重启生效"配置需从这里读取。
	cfgStore, err := config.Open(getEnv("CONFIG_DB", "./data/app.db"))
	if err != nil {
		log.Fatalf("初始化配置存储失败: %v", err)
	}
	cfgStore.Seed(config.KeyLLMModel, getEnv("LLM_MODEL", "deepseek-flash"))
	cfgStore.Seed(config.KeyNarratorTemp, "0.7")
	cfgStore.Seed(config.KeyDirectorTemp, "0.2")
	cfgStore.Seed(config.KeyContextBudget, "60000")
	cfgStore.Seed(config.KeyLoreBudget, "4000")
	cfgStore.Seed(config.KeyLoreScanRounds, "4")
	cfgStore.Seed(config.KeyLoreRecursion, "false")
	cfgStore.Seed(config.KeyPlanInterval, "8")
	cfgStore.Seed(config.KeySummaryChunk, "6")
	cfgStore.Seed(config.KeyAIGuidance, "true")
	// 新键启动时从 env 播种（env 仅作初始值，之后以 DB 为准）
	cfgStore.Seed(config.KeyLLMAPIKey, os.Getenv("LLM_API_KEY"))
	cfgStore.Seed(config.KeyLLMBaseURL, getEnv("LLM_BASE_URL", "https://api.deepseek.com"))
	cfgStore.Seed(config.KeyWebChatToken, os.Getenv("WEB_CHAT_TOKEN"))

	// 6'. Initialize AI Agent (trpc-agent-go + DeepSeek)
	kpAgent, err := agent.NewKPAgent(&agent.Config{
		LLMProvider:  getEnv("LLM_PROVIDER", "deepseek"),
		LLMModel:     getEnv("LLM_MODEL", "deepseek-flash"),
		LLMAPIKey:    os.Getenv("LLM_API_KEY"),
		LLMBaseURL:   getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		MaxTokens:    4096,
		Temperature:  0.8,
		MemoryWindow: 20,
	}, sessions)
	if err != nil {
		log.Fatalf("初始化 AI Agent 失败: %v", err)
	}

	// 6b. Initialize turn engine (规则指导 + 低频 Planner + Narrator)
	metricsEvaluator := agent.NewMetricsEvaluator()
	director, err := agent.NewDirector(&agent.Config{
		LLMModel:            getEnv("LLM_MODEL", "deepseek-flash"),
		LLMAPIKey:           os.Getenv("LLM_API_KEY"),
		LLMBaseURL:          getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		DirectorTemperature: cfgStore.GetFloat(config.KeyDirectorTemp, 0.2),
		DirectorMaxTokens:   2048,
	}, metricsEvaluator)
	if err != nil {
		log.Fatalf("初始化 Director 失败: %v", err)
	}

	narrator, err := agent.NewNarrator(&agent.Config{
		LLMModel:            getEnv("LLM_MODEL", "deepseek-flash"),
		LLMAPIKey:           os.Getenv("LLM_API_KEY"),
		LLMBaseURL:          getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		NarratorTemperature: cfgStore.GetFloat(config.KeyNarratorTemp, 0.7),
		NarratorMaxTokens:   4096,
	}, sessions)
	if err != nil {
		log.Fatalf("初始化 Narrator 失败: %v", err)
	}

	// 6c. Initialize AI guidance (每轮 AI 叙事指导，低成本模型，超时/失败降级规则版)
	guider, err := agent.NewAIGuidance(&agent.Config{
		LLMModel:   getEnv("LLM_MODEL", "deepseek-flash"),
		LLMAPIKey:  os.Getenv("LLM_API_KEY"),
		LLMBaseURL: getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
	})
	if err != nil {
		log.Fatalf("初始化 AI 叙事指导失败: %v", err)
	}

	turnEngine := agent.NewTurnEngine(narrator, director, metricsEvaluator, worldEngine,
		agent.NewContextBuilder(0), agent.DefaultPlanInterval)

	kpAgent.SetTurnEngine(turnEngine)
	turnEngine.SetGuider(guider)
	turnEngine.SetPersonaStore(personaStore)
	turnEngine.SetMemory(memStore)
	guider.SetMemory(memStore)
	kpAgent.SetPersonaStore(personaStore)

	// 7. Register AI Agent
	if err := plugins.RegisterAgent(kpAgent); err != nil {
		log.Fatalf("注册 Agent 失败: %v", err)
	}

	// 8. 接入运行时配置与滚动剧情摘要服务
	turnEngine.SetConfigStore(cfgStore)
	// 滚动剧情摘要服务（RecentTurns 窗口淘汰轮攒批后由 SummaryService 归纳为阶段摘要，
	// 写入长期记忆 world_memory）
	summarySvc := agent.NewSummaryService(worldEngine, director, cfgStore)
	summarySvc.SetMemory(memStore)
	turnEngine.SetSummary(summarySvc)

	// 9. Initialize router（所有消息经 core.Router 转给 Agent）
	router := core.NewRouter(plugins, sessions)

	// 10. Initialize Web channel (Web 聊天 + 管理后台)
	// 监听地址/端口由 trpc_go.yaml 的 server.service 段配置（trpc.voyage.web.Admin）
	if getEnv("WEB_ENABLED", "true") == "true" {
		webServer := web.NewServer(web.Config{
			ChatToken: os.Getenv("WEB_CHAT_TOKEN"),
		}, router, sessions)
		webServer.SetChatHistory(chatHistory)
		webServer.SetAdmin(web.AdminDeps{
			Sessions:     sessions,
			WorldEngine:  worldEngine,
			AssetStore:   assetStore,
			AssetParser:  assetParser,
			PersonaStore: personaStore,
			ConfigStore:  cfgStore,
			TurnEngine:   turnEngine,
			Memory:       memStore,
			StartTime:    time.Now(),
		}, os.Getenv("ADMIN_TOKEN"))
		if err := webServer.StartTrpc(getEnv("TRPC_CONF", "./conf/trpc_go.yaml")); err != nil {
			log.Errorf("启动 Web 渠道失败: %v", err)
		} else {
			defer webServer.Stop()
		}
	}

	// 11. Start
	log.Infof("QQ AI TRPG Bot 已启动")
	log.Infof("架构: 世界引擎(WorldState) + AI多层(Director->Narrator) + Web 渠道")
	log.Infof("世界状态: %s", worldDir)

	// 12. Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	fmt.Printf("\n收到信号 %v，正在关闭...\n", sig)
	log.Infof("QQ AI TRPG Bot 已停止")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
