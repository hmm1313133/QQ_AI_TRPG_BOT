// Package main is the entry point for Voyage World Engine.
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

	"trpc.group/trpc-go/trpc-agent-go/memory/extractor"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/assetparse"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/store"
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

	// 3. Initialize OpenViking client (with degradation support)
	// 支持本地部署和火山引擎云上版本
	openVikingClient := store.NewOpenVikingClient(&store.OpenVikingConfig{
		BaseURL: getEnv("OPENVIKING_BASE_URL", "http://localhost:1933"),
		Enabled: getEnv("OPENVIKING_ENABLED", "false") == "true",
		APIKey:  os.Getenv("OPENVIKING_API_KEY"),
		Account: os.Getenv("OPENVIKING_ACCOUNT"),
		User:    os.Getenv("OPENVIKING_USER"),
		Timeout: 10,
	})

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
	worldEngine := world.NewEngine(worldRepo)

	// 5. Initialize asset parser（素材 LLM 解析器；设计 §11.4）
	assetParser, err := assetparse.NewParser(&assetparse.Config{
		LLMModel:    getEnv("LLM_MODEL", "deepseek-v4-flash"),
		LLMAPIKey:   os.Getenv("LLM_API_KEY"),
		LLMBaseURL:  getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		MaxTokens:   8192,
		Temperature: 0.3,
	})
	if err != nil {
		log.Errorf("初始化素材解析器失败（素材解析功能不可用）: %v", err)
	}

	// 6. Initialize AI Agent (trpc-agent-go + DeepSeek)
	kpAgent, err := agent.NewKPAgent(&agent.Config{
		LLMProvider:  getEnv("LLM_PROVIDER", "deepseek"),
		LLMModel:     getEnv("LLM_MODEL", "deepseek-v4-flash"),
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
		LLMModel:            getEnv("LLM_MODEL", "deepseek-v4-flash"),
		LLMAPIKey:           os.Getenv("LLM_API_KEY"),
		LLMBaseURL:          getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		DirectorTemperature: 0.2,
		DirectorMaxTokens:   2048,
	}, metricsEvaluator)
	if err != nil {
		log.Fatalf("初始化 Director 失败: %v", err)
	}

	narrator, err := agent.NewNarrator(&agent.Config{
		LLMModel:            getEnv("LLM_MODEL", "deepseek-v4-flash"),
		LLMAPIKey:           os.Getenv("LLM_API_KEY"),
		LLMBaseURL:          getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		NarratorTemperature: 0.7,
		NarratorMaxTokens:   4096,
	}, sessions)
	if err != nil {
		log.Fatalf("初始化 Narrator 失败: %v", err)
	}

	turnEngine := agent.NewTurnEngine(narrator, director, metricsEvaluator, worldEngine,
		agent.NewContextBuilder(0), agent.DefaultPlanInterval)

	// 6c. Initialize memory service (记忆层：框架接口 + extractor 双轨写入)
	memoryDir := getEnv("MEMORY_DIR", "./data/memories")
	memoryStore, err := world.NewMemoryStore(memoryDir)
	if err != nil {
		log.Fatalf("初始化记忆存储失败: %v", err)
	}
	var memExtractor extractor.MemoryExtractor
	if getEnv("MEMORY_EXTRACTOR_ENABLED", "true") == "true" {
		memExtractor = extractor.NewExtractor(
			openai.New(getEnv("LLM_MODEL", "deepseek-v4-flash"),
				openai.WithVariant(openai.VariantDeepSeek)),
			extractor.WithPrompt(agent.MemoryExtractPrompt),
		)
	}
	memoryService := agent.NewMemoryService(memoryStore, openVikingClient, director, worldEngine, memExtractor)
	turnEngine.SetMemory(memoryService)

	kpAgent.SetTurnEngine(turnEngine)
	turnEngine.SetPersonaStore(personaStore)
	kpAgent.SetPersonaStore(personaStore)

	// 7. Register AI Agent
	if err := plugins.RegisterAgent(kpAgent); err != nil {
		log.Fatalf("注册 Agent 失败: %v", err)
	}

	// 8. Initialize runtime config store (SQLite, 运行时配置热更新)
	cfgStore, err := config.Open(getEnv("CONFIG_DB", "./data/app.db"))
	if err != nil {
		log.Fatalf("初始化配置存储失败: %v", err)
	}
	cfgStore.Seed(config.KeyLLMModel, getEnv("LLM_MODEL", "deepseek-v4-flash"))
	cfgStore.Seed(config.KeyNarratorTemp, "0.7")
	cfgStore.Seed(config.KeyDirectorTemp, "0.2")
	cfgStore.Seed(config.KeyContextBudget, "45000")
	cfgStore.Seed(config.KeyLoreBudget, "4000")
	cfgStore.Seed(config.KeyLoreScanRounds, "4")
	cfgStore.Seed(config.KeyLoreRecursion, "false")
	cfgStore.Seed(config.KeyPlanInterval, "8")
	cfgStore.Seed(config.KeyExtractorEnabled, getEnv("MEMORY_EXTRACTOR_ENABLED", "true"))
	// 新键启动时从 env 播种（env 仅作初始值，之后以 DB 为准）
	cfgStore.Seed(config.KeyLLMAPIKey, os.Getenv("LLM_API_KEY"))
	cfgStore.Seed(config.KeyLLMBaseURL, getEnv("LLM_BASE_URL", "https://api.deepseek.com"))
	cfgStore.Seed(config.KeyWebChatToken, os.Getenv("WEB_CHAT_TOKEN"))
	turnEngine.SetConfigStore(cfgStore)
	memoryService.SetEnabledFn(func() bool {
		return cfgStore.GetBool(config.KeyExtractorEnabled, true)
	})

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
			MemoryStore:  memoryStore,
			PersonaStore: personaStore,
			ConfigStore:  cfgStore,
			TurnEngine:   turnEngine,
			StartTime:    time.Now(),
		}, os.Getenv("ADMIN_TOKEN"))
		if err := webServer.StartTrpc(getEnv("TRPC_CONF", "./conf/trpc_go.yaml")); err != nil {
			log.Errorf("启动 Web 渠道失败: %v", err)
		} else {
			defer webServer.Stop()
		}
	}

	// 11. Start
	log.Infof("Voyage World Engine 已启动")
	log.Infof("架构: 世界引擎(WorldState) + AI多层(Director->Narrator) + Web 渠道")
	log.Infof("世界状态: %s", worldDir)
	if openVikingClient.IsEnabled() {
		log.Infof("OpenViking: 已连接")
	} else {
		log.Infof("OpenViking: 未启用（仅本地存储）")
	}

	// 12. Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	fmt.Printf("\n收到信号 %v，正在关闭...\n", sig)
	log.Infof("Voyage World Engine 已停止")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
