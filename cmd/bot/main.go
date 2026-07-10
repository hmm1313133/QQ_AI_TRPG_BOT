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

	"trpc.group/trpc-go/trpc-go/log"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/bot"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/handler"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/store"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/character"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/gamelog"
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

	// 7. Initialize progress tracker and timeline engine
	progressTracker := trpg.NewProgressTracker(scriptArchive, openVikingClient)
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

	// 9. Initialize GameState store (多层架构：结构化运行态持久化)
	gameStateDir := getEnv("GAMESTATE_DIR", filepath.Join(scriptDir, "gamestate"))
	gameStateStore, err := agent.NewGameStateStore(gameStateDir)
	if err != nil {
		log.Fatalf("初始化 GameStateStore 失败: %v", err)
	}

	// 10. Create script deps for KPAgent (含 GameStateStore)
	scriptDeps := &agent.ScriptDeps{
		Archive:         scriptArchive,
		ProgressTracker: progressTracker,
		TimelineEngine:  timelineEngine,
		GameStateStore:  gameStateStore,
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

	// 11b. Initialize multi-layer pipeline (Director -> Narrator -> StateUpdate)
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

	pipeline := agent.NewKPPipeline(director, narrator, gameStateStore)
	kpAgent.SetPipeline(pipeline)

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
		scriptArchive, scriptAnalyzer, progressTracker, timelineEngine, sessions, svc, gameStateStore))
	handlerCount++

	// 13. Initialize QQ Bot
	qqBot, err := bot.NewBot(&bot.Config{
		AppID:        os.Getenv("QQ_BOT_APPID"),
		ClientSecret: os.Getenv("QQ_BOT_SECRET"),
	}, plugins, sessions, gameLogger)
	if err != nil {
		log.Fatalf("初始化 QQ Bot 失败: %v", err)
	}

	// 14. Start
	if err := qqBot.Start(); err != nil {
		log.Fatalf("启动 Bot 失败: %v", err)
	}
	log.Infof("QQ AI TRPG Bot 已启动")
	log.Infof("已注册 Handler: %d, Agent: 1 (多层架构: Director + Narrator)", handlerCount)
	log.Infof("架构: Service层 + 功能层(Handler) + AI多层(Director->Narrator->StateUpdate) + 剧本层(Script+GameState) + 联动(Session)")
	log.Infof("规则集: CoC7 + DnD5e | 角色卡: %s | 剧本: %s | 运行态: %s", charDir, scriptDir, gameStateDir)
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
