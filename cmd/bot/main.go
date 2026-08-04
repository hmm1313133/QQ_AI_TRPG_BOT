// Package main is the entry point for QQ AI TRPG Bot.
//
// Architecture:
//   - Service Layer (trpg.Service): unified game operations, single source of truth
//   - Function Layer (Handler): Go-based deterministic features (dice/mode/log/character/ruleset/script)
//   - Agent Layer (trpc-agent-go): AI capabilities (KP/DM hosting + script analysis)
//   - Script Layer (script): PDF/Word parsing, AI script recognition, archive management
//   - Linkage: Service shared by both Handler and Agent; Session for cross-layer state
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"trpc.group/trpc-go/trpc-go/log"

	"trpc.group/trpc-go/trpc-agent-go/memory/extractor"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/bot"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/handler"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/store"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/character"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/gamelog"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/web"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func main() {
	// 1. Initialize core components
	plugins := core.NewPluginManager()
	sessions := core.NewSessionManager()
	gameLogger := gamelog.NewGameLogger()

	// 2. Initialize TRPG engine (rulesets, sessions, character bindings)
	trpgEngine := trpg.NewEngine()

	// 3. Initialize character card manager (loads existing cards from disk)
	charDir := getEnv("CHARACTER_DIR", "./data/characters")
	charMgr, err := character.NewManager(charDir)
	if err != nil {
		log.Fatalf("初始化角色卡管理器失败: %v", err)
	}

	// 4. Create unified Service (shared by Handlers and AI Agent)
	svc := trpg.NewService(trpgEngine, charMgr, sessions)

	// 5. Initialize script archive (loads existing scripts from disk)
	scriptDir := getEnv("SCRIPT_DIR", "./data/scripts")
	scriptArchive, err := script.NewArchive(scriptDir)
	if err != nil {
		log.Fatalf("初始化剧本存档管理器失败: %v", err)
	}

	// 6. Initialize OpenViking client (with degradation support)
	// 支持本地部署和火山引擎云上版本
	openVikingClient := store.NewOpenVikingClient(&store.OpenVikingConfig{
		BaseURL: getEnv("OPENVIKING_BASE_URL", "http://localhost:1933"),
		Enabled: getEnv("OPENVIKING_ENABLED", "false") == "true",
		APIKey:  os.Getenv("OPENVIKING_API_KEY"),
		Account: os.Getenv("OPENVIKING_ACCOUNT"),
		User:    os.Getenv("OPENVIKING_USER"),
		Timeout: 10,
	})

	// 7. Initialize world engine (世界引擎：状态唯一真相)
	worldDir := getEnv("WORLD_DIR", "./data/worlds")
	worldRepo, err := world.NewJSONRepository(worldDir)
	if err != nil {
		log.Fatalf("初始化世界状态存储失败: %v", err)
	}
	worldEngine := world.NewEngine(worldRepo)

	// 7b. 迁移旧版 GameState/Progress 数据到 WorldState
	world.MigrateLegacy(worldEngine, getEnv("GAMESTATE_DIR", filepath.Join(scriptDir, "gamestate")), filepath.Join(scriptDir, "progress"))

	// 7c. Initialize progress tracker (world.Engine 门面) and timeline engine
	progressTracker := trpg.NewProgressTracker(scriptArchive, openVikingClient, worldEngine)
	timelineEngine := trpg.NewTimelineEngine(trpg.TimelineConfig{
		IdleInterval: 15,
		MaxIdleCount: 3,
	}, progressTracker, sessions)

	// 8. Initialize script analyzer Agent
	scriptAnalyzer, err := script.NewScriptAnalyzer(&script.AnalyzerConfig{
		LLMModel:    getEnv("LLM_MODEL", "deepseek-chat"),
		LLMAPIKey:   os.Getenv("LLM_API_KEY"),
		LLMBaseURL:  getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		MaxTokens:   16384,
		Temperature: 0.3,
	})
	if err != nil {
		log.Fatalf("初始化剧本识别 Agent 失败: %v", err)
	}

	// 9. Create script deps for KPAgent (含世界引擎与成长引擎)
	scriptDeps := &agent.ScriptDeps{
		Archive:         scriptArchive,
		ProgressTracker: progressTracker,
		TimelineEngine:  timelineEngine,
		WorldEngine:     worldEngine,
		Progression:     agent.NewProgressionEngine(svc, worldEngine),
	}

	// 11. Initialize AI Agent (trpc-agent-go + DeepSeek)
	kpAgent, err := agent.NewKPAgent(&agent.Config{
		LLMProvider:  getEnv("LLM_PROVIDER", "deepseek"),
		LLMModel:     getEnv("LLM_MODEL", "deepseek-chat"),
		LLMAPIKey:    os.Getenv("LLM_API_KEY"),
		LLMBaseURL:   getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		MaxTokens:    4096,
		Temperature:  0.8,
		MemoryWindow: 20,
	}, sessions, svc, scriptDeps)
	if err != nil {
		log.Fatalf("初始化 AI Agent 失败: %v", err)
	}

	// 11b. Initialize turn engine (规则指导 + 低频 Planner + Narrator)
	metricsEvaluator := agent.NewMetricsEvaluator(svc)
	director, err := agent.NewDirector(&agent.Config{
		LLMModel:            getEnv("LLM_MODEL", "deepseek-chat"),
		LLMAPIKey:           os.Getenv("LLM_API_KEY"),
		LLMBaseURL:          getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		DirectorTemperature: 0.2,
		DirectorMaxTokens:   2048,
	}, metricsEvaluator)
	if err != nil {
		log.Fatalf("初始化 Director 失败: %v", err)
	}

	narrator, err := agent.NewNarrator(&agent.Config{
		LLMModel:            getEnv("LLM_MODEL", "deepseek-chat"),
		LLMAPIKey:           os.Getenv("LLM_API_KEY"),
		LLMBaseURL:          getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
		NarratorTemperature: 0.7,
		NarratorMaxTokens:   4096,
	}, sessions, svc, scriptDeps)
	if err != nil {
		log.Fatalf("初始化 Narrator 失败: %v", err)
	}

	turnEngine := agent.NewTurnEngine(narrator, director, metricsEvaluator, worldEngine,
		agent.NewContextBuilder(0), agent.DefaultPlanInterval)

	// 11c. Initialize memory service (记忆层：框架接口 + extractor 双轨写入)
	memoryDir := getEnv("MEMORY_DIR", "./data/memories")
	memoryStore, err := world.NewMemoryStore(memoryDir)
	if err != nil {
		log.Fatalf("初始化记忆存储失败: %v", err)
	}
	var memExtractor extractor.MemoryExtractor
	if getEnv("MEMORY_EXTRACTOR_ENABLED", "true") == "true" {
		memExtractor = extractor.NewExtractor(
			openai.New(getEnv("LLM_MODEL", "deepseek-chat"),
				openai.WithVariant(openai.VariantDeepSeek)),
			extractor.WithPrompt(agent.TRPGMemoryExtractPrompt),
		)
	}
	memoryService := agent.NewMemoryService(memoryStore, openVikingClient, director, worldEngine, memExtractor)
	turnEngine.SetMemory(memoryService)

	kpAgent.SetTurnEngine(turnEngine)

	// 12. Register AI Agent
	if err := plugins.RegisterAgent(kpAgent); err != nil {
		log.Fatalf("注册 Agent 失败: %v", err)
	}

	// 13. Register command handlers (order matters: specific before general)
	handlerCount := 0
	plugins.RegisterHandler(handler.NewHelpHandler())
	handlerCount++
	plugins.RegisterHandler(handler.NewRulesetHandler(svc))
	handlerCount++
	plugins.RegisterHandler(handler.NewCharacterHandler(svc))
	handlerCount++
	plugins.RegisterHandler(handler.NewCoCHandler(svc))
	handlerCount++
	plugins.RegisterHandler(handler.NewDnDHandler(svc))
	handlerCount++
	plugins.RegisterHandler(handler.NewDiceHandler(svc))
	handlerCount++
	plugins.RegisterHandler(handler.NewModeHandler(sessions))
	handlerCount++
	plugins.RegisterHandler(handler.NewLogHandler(gameLogger))
	handlerCount++
	plugins.RegisterHandler(handler.NewScriptHandler(
		scriptArchive, scriptAnalyzer, progressTracker, timelineEngine, sessions, svc, worldEngine))
	handlerCount++

	// 12b. Initialize runtime config store (SQLite, 运行时配置热更新)
	cfgStore, err := config.Open(getEnv("CONFIG_DB", "./data/app.db"))
	if err != nil {
		log.Fatalf("初始化配置存储失败: %v", err)
	}
	cfgStore.Seed(config.KeyLLMModel, getEnv("LLM_MODEL", "deepseek-chat"))
	cfgStore.Seed(config.KeyNarratorTemp, "0.7")
	cfgStore.Seed(config.KeyDirectorTemp, "0.2")
	cfgStore.Seed(config.KeyContextBudget, "6000")
	cfgStore.Seed(config.KeyPlanInterval, "8")
	cfgStore.Seed(config.KeyExtractorEnabled, getEnv("MEMORY_EXTRACTOR_ENABLED", "true"))
	// 新键启动时从 env 播种（env 仅作初始值，之后以 DB 为准；改凭证后重启机器人即生效）
	cfgStore.Seed(config.KeyQQAppID, os.Getenv("QQ_BOT_APPID"))
	cfgStore.Seed(config.KeyQQSecret, os.Getenv("QQ_BOT_SECRET"))
	cfgStore.Seed(config.KeyLLMAPIKey, os.Getenv("LLM_API_KEY"))
	cfgStore.Seed(config.KeyLLMBaseURL, getEnv("LLM_BASE_URL", "https://api.deepseek.com"))
	cfgStore.Seed(config.KeyWebChatToken, os.Getenv("WEB_CHAT_TOKEN"))
	turnEngine.SetConfigStore(cfgStore)
	memoryService.SetEnabledFn(func() bool {
		return cfgStore.GetBool(config.KeyExtractorEnabled, true)
	})

	// 13. Initialize QQ Bot (QQChannel，路由统一走 core.Router)
	// 凭证经 CredFn 从 ConfigStore 读取（qq_appid/qq_secret，见《管理后台扩展设计.md》2.1/2.2），
	// 未配置时回落环境变量；管理后台改凭证后点"重启机器人"即生效。
	router := core.NewRouter(plugins, sessions, gameLogger)
	qqBot, err := bot.NewBot(&bot.Config{
		CredFn: func() (string, string) {
			return cfgStore.Get("qq_appid", os.Getenv("QQ_BOT_APPID")),
				cfgStore.Get("qq_secret", os.Getenv("QQ_BOT_SECRET"))
		},
	}, router)
	if err != nil {
		log.Fatalf("初始化 QQ Bot 失败: %v", err)
	}

	// 13b. Initialize Web channel (Web 聊天 + 管理后台)
	var webServer *web.Server
	if getEnv("WEB_ENABLED", "true") == "true" {
		webServer = web.NewServer(web.Config{
			Addr:      getEnv("WEB_ADDR", ":8080"),
			ChatToken: os.Getenv("WEB_CHAT_TOKEN"),
		}, router, sessions)
		webServer.SetAdmin(web.AdminDeps{
			Sessions:    sessions,
			Service:     svc,
			WorldEngine: worldEngine,
			Archive:     scriptArchive,
			Analyzer:    scriptAnalyzer,
			CharMgr:     charMgr,
			MemoryStore: memoryStore,
			GameLogger:  gameLogger,
			ConfigStore: cfgStore,
			Bot:         qqBot,
			StartTime:   time.Now(),
		}, os.Getenv("ADMIN_TOKEN"))
		if err := webServer.Start(); err != nil {
			log.Errorf("启动 Web 渠道失败: %v", err)
		} else {
			defer webServer.Stop()
		}
	}

	// 13c. TimelineEngine 主动推送（Web 渠道通过 WS 即时收到无进展提示）
	timelineEngine.SetPushFunc(func(sessionID, msg string) {
		if webServer != nil {
			webServer.PushToSession(sessionID, msg)
		}
	})

	// 14. Start
	if err := qqBot.Start(); err != nil {
		log.Fatalf("启动 Bot 失败: %v", err)
	}
	log.Infof("QQ AI TRPG Bot 已启动")
	log.Infof("已注册 Handler: %d, Agent: 1 (多层架构: Director + Narrator)", handlerCount)
	log.Infof("架构: Service层 + 功能层(Handler) + AI多层(Director->Narrator->StateUpdate) + 剧本层(Script+GameState) + 联动(Session)")
	log.Infof("规则集: CoC7 + DnD5e | 角色卡: %s | 剧本: %s | 世界状态: %s", charDir, scriptDir, worldDir)
	if openVikingClient.IsEnabled() {
		log.Infof("OpenViking: 已连接")
	} else {
		log.Infof("OpenViking: 未启用（仅本地存储）")
	}

	// 15. Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	fmt.Printf("\n收到信号 %v，正在关闭...\n", sig)
	timelineEngine.StopAll()
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
