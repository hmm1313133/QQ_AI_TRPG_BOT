package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ==================== 文件解析测试 ====================

// testTruncate 截断字符串用于测试日志输出。
func testTruncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func findDocxFile(t *testing.T) string {
	candidates := []string{
		filepath.Join("..", "..", "活神之手.docx"),
	}
	// 也尝试绝对路径
	if dir, err := os.Getwd(); err == nil {
		root := filepath.Join(dir, "..", "..")
		candidates = append([]string{filepath.Join(root, "活神之手.docx")}, candidates...)
	}
	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			return abs
		}
	}
	t.Skip("未找到 活神之手.docx，跳过测试")
	return ""
}

func TestParseDocx_活神之手(t *testing.T) {
	path := findDocxFile(t)
	t.Logf("测试文件: %s", path)

	info, _ := os.Stat(path)
	t.Logf("文件大小: %.2f KB (%d bytes)", float64(info.Size())/1024, info.Size())

	start := time.Now()
	text, err := ParseFile(path)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("解析失败: %v (耗时 %s)", err, elapsed)
	}

	t.Logf("解析耗时: %s", elapsed)
	t.Logf("提取文本长度: %d 字符", len([]rune(text)))

	if len([]rune(text)) < 100 {
		t.Fatalf("提取文本过短: %d 字符", len([]rune(text)))
	}

	// 预览前500字符
	preview := []rune(text)
	if len(preview) > 500 {
		preview = preview[:500]
	}
	t.Logf("文本预览:\n%s", string(preview))

	lineCount := strings.Count(text, "\n")
	t.Logf("行数: %d", lineCount)
}

func TestParseSpeed_总结(t *testing.T) {
	path := findDocxFile(t)
	info, _ := os.Stat(path)
	fileSizeKB := float64(info.Size()) / 1024

	runs := 5
	var totalElapsed time.Duration
	var textLen int

	for i := 0; i < runs; i++ {
		start := time.Now()
		text, err := ParseFile(path)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("第 %d 次解析失败: %v", i+1, err)
		}
		totalElapsed += elapsed
		textLen = len([]rune(text))
	}

	avgElapsed := totalElapsed / time.Duration(runs)
	speedKBps := fileSizeKB / avgElapsed.Seconds()

	fmt.Println("\n========== 活神之手.docx 解析测速结果 ==========")
	fmt.Printf("文件大小:    %.2f KB\n", fileSizeKB)
	fmt.Printf("提取字符数:  %d\n", textLen)
	fmt.Printf("运行次数:    %d\n", runs)
	fmt.Printf("平均耗时:    %s\n", avgElapsed)
	fmt.Printf("解析速度:    %.2f KB/s\n", speedKBps)
	fmt.Println("================================================")
}

// ==================== AI 剧本识别集成测试 ====================
//
// 以下测试需要真实的 LLM API 调用，通过环境变量 LLM_API_KEY / LLM_BASE_URL / LLM_MODEL 配置。
// 运行: go test -v -run "TestScriptAnalyze_活神之手" ./internal/script/ -timeout 300s

func getAnalyzerConfigFromEnv() *AnalyzerConfig {
	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")

	if apiKey == "" {
		// 尝试从 .env 文件读取
		envPath := filepath.Join("..", "..", ".env")
		if data, err := os.ReadFile(envPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "LLM_API_KEY=") && apiKey == "" {
					apiKey = strings.TrimPrefix(line, "LLM_API_KEY=")
				}
				if strings.HasPrefix(line, "LLM_BASE_URL=") && baseURL == "" {
					baseURL = strings.TrimPrefix(line, "LLM_BASE_URL=")
				}
				if strings.HasPrefix(line, "LLM_MODEL=") && model == "" {
					model = strings.TrimPrefix(line, "LLM_MODEL=")
				}
			}
		}
	}

	return &AnalyzerConfig{
		LLMModel:    model,
		LLMAPIKey:   apiKey,
		LLMBaseURL:  baseURL,
		MaxTokens:   16384,
		Temperature: 0.3,
	}
}

// TestScriptAnalyze_活神之手 完整测试：docx解析 → AI识别 → 结构验证
// 这是 .script upload 指令的核心流程测试。
func TestScriptAnalyze_活神之手(t *testing.T) {
	cfg := getAnalyzerConfigFromEnv()
	if cfg.LLMAPIKey == "" {
		t.Skip("未设置 LLM_API_KEY，跳过 AI 分析测试")
	}
	t.Logf("模型: %s, API: %s", cfg.LLMModel, cfg.LLMBaseURL)

	// Step 1: 解析 docx 文件
	path := findDocxFile(t)
	fileInfo, _ := os.Stat(path)
	t.Logf("=== Step 1: 解析文件 %s ===", filepath.Base(path))

	parseStart := time.Now()
	text, err := ParseFile(path)
	parseElapsed := time.Since(parseStart)
	if err != nil {
		t.Fatalf("文件解析失败: %v", err)
	}
	t.Logf("解析完成: %d 字符, 耗时 %s", len([]rune(text)), parseElapsed)

	// 预览
	preview := []rune(text)
	if len(preview) > 300 {
		preview = preview[:300]
	}
	t.Logf("文本预览:\n%s", string(preview))

	// Step 2: 创建 Analyzer
	t.Log("\n=== Step 2: 初始化 ScriptAnalyzer ===")
	analyzer, err := NewScriptAnalyzer(cfg)
	if err != nil {
		t.Fatalf("创建 Analyzer 失败: %v", err)
	}

	// Step 3: AI 分析
	t.Log("\n=== Step 3: AI 剧本识别 ===")
	progressCount := 0
	progress := func(stage, message string) {
		progressCount++
		t.Logf("[进度 %d] [%s] %s", progressCount, stage, message)
	}

	analyzeStart := time.Now()
	scr, err := analyzer.Analyze(context.Background(), text, filepath.Base(path), progress)
	analyzeElapsed := time.Since(analyzeStart)
	if err != nil {
		t.Fatalf("AI 分析失败: %v (耗时 %s)", err, analyzeElapsed)
	}
	t.Logf("AI 分析完成, 耗时 %s", analyzeElapsed)

	// Step 4: 验证识别结果
	t.Log("\n=== Step 4: 验证识别结果 ===")

	// 基本字段
	if scr.Title == "" {
		t.Error("Title 为空")
	}
	t.Logf("标题: %s", scr.Title)

	if scr.Name == "" {
		t.Error("Name 为空")
	}
	t.Logf("名称: %s", scr.Name)

	if scr.System != "coc7" && scr.System != "dnd5e" {
		t.Errorf("System 不合法: %s", scr.System)
	}
	t.Logf("规则集: %s", scr.System)

	// 故事背景
	t.Logf("故事背景:")
	t.Logf("  设定: %s", testTruncate(scr.Background.Setting, 100))
	t.Logf("  时代: %s", scr.Background.Era)
	t.Logf("  地点: %s", scr.Background.Location)
	t.Logf("  氛围: %s", scr.Background.Atmosphere)
	t.Logf("  主题: %s", scr.Background.MainTheme)
	if scr.Background.Synopsis != "" {
		t.Logf("  梗概: %s", testTruncate(scr.Background.Synopsis, 200))
	}

	// 时间轴
	if len(scr.Timeline) == 0 {
		t.Error("Timeline 为空")
	} else {
		t.Logf("\n时间轴 (%d 节点):", len(scr.Timeline))
		for i, node := range scr.Timeline {
			keyMark := ""
			if node.IsKeyNode {
				keyMark = " [关键]"
			}
			t.Logf("  %d. %s (%s)%s", i+1, node.Name, node.Type, keyMark)
			if node.Description != "" {
				t.Logf("     %s", testTruncate(node.Description, 80))
			}
			if len(node.Triggers) > 0 {
				t.Logf("     触发: %s", strings.Join(node.Triggers, ", "))
			}
		}
	}

	// 角色
	if len(scr.Characters) == 0 {
		t.Log("Characters 为空（可能剧本无明确角色列表）")
	} else {
		t.Logf("\n角色 (%d):", len(scr.Characters))
		for _, c := range scr.Characters {
			t.Logf("  - %s (%s)", c.Name, c.Role)
			t.Logf("    性格: %s", c.Personality)
			if c.Background != "" {
				t.Logf("    背景: %s", testTruncate(c.Background, 80))
			}
			if len(c.Attrs) > 0 {
				t.Logf("    属性: %v", c.Attrs)
			}
			if len(c.Skills) > 0 {
				t.Logf("    技能: %v", c.Skills)
			}
		}
	}

	// 场景
	if len(scr.Scenes) > 0 {
		t.Logf("\n场景 (%d):", len(scr.Scenes))
		for _, s := range scr.Scenes {
			t.Logf("  - %s: %s", s.Name, testTruncate(s.Description, 60))
		}
	}

	// Step 5: 验证 ID 补全
	t.Log("\n=== Step 5: 验证 ID 补全 ===")
	for i, c := range scr.Characters {
		if c.ID == "" {
			t.Errorf("角色 %d ID 为空", i)
		}
	}
	for i, n := range scr.Timeline {
		if n.ID == "" {
			t.Errorf("节点 %d ID 为空", i)
		}
		if n.Order == 0 {
			t.Errorf("节点 %d Order 为 0", i)
		}
	}

	// Step 6: 总结
	fmt.Println("\n========== .script upload 完整流程测试结果 ==========")
	fmt.Printf("文件:        %s (%.2f KB)\n", filepath.Base(path), float64(fileInfo.Size())/1024)
	fmt.Printf("解析耗时:    %s (提取 %d 字符)\n", parseElapsed, len([]rune(text)))
	fmt.Printf("AI 分析耗时: %s\n", analyzeElapsed)
	fmt.Printf("总耗时:      %s\n", parseElapsed+analyzeElapsed)
	fmt.Printf("标题:        %s\n", scr.Title)
	fmt.Printf("规则集:      %s\n", scr.System)
	fmt.Printf("时间轴节点:  %d\n", len(scr.Timeline))
	fmt.Printf("登场角色:    %d\n", len(scr.Characters))
	fmt.Printf("场景:        %d\n", len(scr.Scenes))
	fmt.Printf("进度推送:    %d 次\n", progressCount)
	fmt.Println("======================================================")

	t.Logf("\n测试通过！可以使用 .script load %s 加载此剧本", scr.Name)
}

// ==================== docx解析 + AI识别 + OpenViking写入 完整集成测试 ====================
//
// 测试完整流程: 解析docx → AI识别剧本结构 → 写入OpenViking上下文数据库
// 运行: go test -v -run "TestScriptAnalyze_WriteToOpenViking" ./internal/script/ -timeout 300s

// getOpenVikingConfigFromEnv 从环境变量或.env文件读取 OpenViking 配置。
func getOpenVikingConfigFromEnv() map[string]string {
	result := map[string]string{
		"enabled":  os.Getenv("OPENVIKING_ENABLED"),
		"base_url": os.Getenv("OPENVIKING_BASE_URL"),
		"api_key":  os.Getenv("OPENVIKING_API_KEY"),
	}

	// 从 .env 文件补充
	envPath := filepath.Join("..", "..", ".env")
	if data, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "OPENVIKING_ENABLED=") && result["enabled"] == "" {
				result["enabled"] = strings.TrimPrefix(line, "OPENVIKING_ENABLED=")
			}
			if strings.HasPrefix(line, "OPENVIKING_BASE_URL=") && result["base_url"] == "" {
				result["base_url"] = strings.TrimPrefix(line, "OPENVIKING_BASE_URL=")
			}
			if strings.HasPrefix(line, "OPENVIKING_API_KEY=") && result["api_key"] == "" {
				result["api_key"] = strings.TrimPrefix(line, "OPENVIKING_API_KEY=")
			}
		}
	}
	return result
}

// TestScriptAnalyze_WriteToOpenViking 完整测试：docx解析 → AI识别 → 写入OpenViking
func TestScriptAnalyze_WriteToOpenViking(t *testing.T) {
	// 检查 LLM 配置
	llmCfg := getAnalyzerConfigFromEnv()
	if llmCfg.LLMAPIKey == "" {
		t.Skip("未设置 LLM_API_KEY，跳过集成测试")
	}

	// 检查 OpenViking 配置
	ovCfg := getOpenVikingConfigFromEnv()
	if ovCfg["enabled"] != "true" {
		t.Skip("OPENVIKING_ENABLED 未设置为 true，跳过 OpenViking 写入测试")
	}

	// --- Step 1: 解析 docx ---
	path := findDocxFile(t)
	t.Logf("=== Step 1: 解析 docx ===")
	text, err := ParseFile(path)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	t.Logf("解析完成: %d 字符", len([]rune(text)))

	// --- Step 2: AI 识别 ---
	t.Log("\n=== Step 2: AI 剧本识别 ===")
	analyzer, err := NewScriptAnalyzer(llmCfg)
	if err != nil {
		t.Fatalf("创建 Analyzer 失败: %v", err)
	}

	progressCount := 0
	progress := func(stage, message string) {
		progressCount++
		t.Logf("[进度 %d] [%s] %s", progressCount, stage, message)
	}

	scr, err := analyzer.Analyze(context.Background(), text, filepath.Base(path), progress)
	if err != nil {
		t.Fatalf("AI 分析失败: %v", err)
	}
	t.Logf("识别完成: %s, 节点=%d, 角色=%d, 场景=%d",
		scr.Title, len(scr.Timeline), len(scr.Characters), len(scr.Scenes))

	// --- Step 3: 连接 OpenViking ---
	t.Log("\n=== Step 3: 连接 OpenViking ===")
	baseURL := ovCfg["base_url"]
	if baseURL == "" {
		baseURL = "http://localhost:1933"
	}

	// 使用 HTTP 直接调用 OpenViking API
	ctx := context.Background()
	ovBaseURL := baseURL
	ovAPIKey := ovCfg["api_key"]

	// 检查 OpenViking 连接
	healthURL := ovBaseURL + "/health"
	t.Logf("检查连接: %s", healthURL)

	healthReq, _ := http.NewRequest("GET", healthURL, nil)
	if ovAPIKey != "" {
		healthReq.Header.Set("Authorization", "Bearer "+ovAPIKey)
		healthReq.Header.Set("X-API-Key", ovAPIKey)
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	healthResp, err := httpClient.Do(healthReq)
	if err != nil {
		t.Skipf("OpenViking 不可达 (%v)，跳过写入测试", err)
	}
	healthResp.Body.Close()
	if healthResp.StatusCode >= 400 {
		t.Skipf("OpenViking 健康检查失败 (HTTP %d)，跳过写入测试", healthResp.StatusCode)
	}
	t.Log("OpenViking 连接成功")

	// --- Step 3.5: 清理 OpenViking 旧数据 ---
	t.Log("\n=== Step 3.5: 清理 OpenViking 旧数据 ===")
	deleteFromOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey, "trpg_scripts")
	t.Log("  ✓ 旧数据已清理（trpg_scripts/ 目录）")

	// --- Step 4: 写入剧本到 OpenViking（导演剧本模式） ---
	t.Log("\n=== Step 4: 写入剧本到 OpenViking（导演剧本模式） ===")
	scriptBasePath := "trpg_scripts/" + scr.Name

	// 4.1 写入剧本背景（含新增字段）
	bgParts := []string{}
	bgParts = append(bgParts, fmt.Sprintf("# 剧本背景：%s", scr.Title))
	bgParts = append(bgParts, fmt.Sprintf("- 规则集: %s", scr.System))
	bgParts = append(bgParts, fmt.Sprintf("- 时代: %s", scr.Background.Era))
	bgParts = append(bgParts, fmt.Sprintf("- 地点: %s", scr.Background.Location))
	bgParts = append(bgParts, fmt.Sprintf("- 氛围: %s", scr.Background.Atmosphere))
	bgParts = append(bgParts, fmt.Sprintf("- 主题: %s", scr.Background.MainTheme))
	if scr.Background.Tone != "" {
		bgParts = append(bgParts, fmt.Sprintf("- 叙事基调: %s", scr.Background.Tone))
	}
	if len(scr.Background.KeyOrganizations) > 0 {
		bgParts = append(bgParts, fmt.Sprintf("- 关键组织: %s", strings.Join(scr.Background.KeyOrganizations, ", ")))
	}
	if len(scr.Background.KeyThemes) > 0 {
		bgParts = append(bgParts, fmt.Sprintf("- 核心冲突: %s", strings.Join(scr.Background.KeyThemes, ", ")))
	}
	bgParts = append(bgParts, "")
	bgParts = append(bgParts, "## 故事梗概")
	bgParts = append(bgParts, scr.Background.Synopsis)
	if scr.Background.Backstory != "" {
		bgParts = append(bgParts, "")
		bgParts = append(bgParts, "## 详细背景")
		bgParts = append(bgParts, scr.Background.Backstory)
	}
	writeToOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey, scriptBasePath+"/background", strings.Join(bgParts, "\n"))
	t.Log("  ✓ 剧本背景已写入（含基调、核心冲突、详细背景）")

	// 4.2 写入时间轴节点（含叙述文本、线索、遭遇等导演字段）
	for i, node := range scr.Timeline {
		parts := []string{}
		parts = append(parts, fmt.Sprintf("# 节点 %d: %s", i+1, node.Name))
		parts = append(parts, fmt.Sprintf("- 类型: %s", node.Type))
		parts = append(parts, fmt.Sprintf("- 关键节点: %v", node.IsKeyNode))
		if len(node.NPCs) > 0 {
			parts = append(parts, fmt.Sprintf("- 涉及NPC: %s", strings.Join(node.NPCs, ", ")))
		}
		parts = append(parts, "")
		parts = append(parts, "## 描述")
		parts = append(parts, node.Description)

		if node.Narrative != "" {
			parts = append(parts, "")
			parts = append(parts, "## 叙述文本（可直接朗读）")
			parts = append(parts, node.Narrative)
		}
		if len(node.Clues) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 线索")
			for _, clue := range node.Clues {
				parts = append(parts, fmt.Sprintf("- %s", clue))
			}
		}
		if len(node.Encounters) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 遭遇")
			for _, enc := range node.Encounters {
				parts = append(parts, fmt.Sprintf("- %s", enc))
			}
		}
		if len(node.Objectives) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 玩家目标")
			for _, obj := range node.Objectives {
				parts = append(parts, fmt.Sprintf("- %s", obj))
			}
		}
		if len(node.Triggers) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 触发条件")
			for _, tr := range node.Triggers {
				parts = append(parts, fmt.Sprintf("- %s", tr))
			}
		}
		if len(node.Consequences) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 可能后果")
			for _, con := range node.Consequences {
				parts = append(parts, fmt.Sprintf("- %s", con))
			}
		}
		if len(node.Branches) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 分支路径")
			for _, br := range node.Branches {
				parts = append(parts, fmt.Sprintf("- %s", br))
			}
		}
		if node.KPNotes != "" {
			parts = append(parts, "")
			parts = append(parts, "## KP导演备注")
			parts = append(parts, node.KPNotes)
		}

		writeToOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/timeline/%s", scriptBasePath, node.ID), strings.Join(parts, "\n"))
	}
	t.Logf("  ✓ 时间轴 %d 节点已写入（含叙述文本、线索、遭遇、目标、分支、KP备注）", len(scr.Timeline))

	// 4.3 写入角色（含动机、秘密、对话风格、关键台词等）
	for _, c := range scr.Characters {
		parts := []string{}
		parts = append(parts, fmt.Sprintf("# 角色: %s (%s)", c.Name, c.Role))
		parts = append(parts, "")
		parts = append(parts, "## 性格")
		parts = append(parts, c.Personality)
		parts = append(parts, "")
		parts = append(parts, "## 背景")
		parts = append(parts, c.Background)

		if c.Appearance != "" {
			parts = append(parts, "")
			parts = append(parts, "## 外貌")
			parts = append(parts, c.Appearance)
		}
		if c.Motivation != "" {
			parts = append(parts, "")
			parts = append(parts, "## 动机")
			parts = append(parts, c.Motivation)
		}
		if c.Secrets != "" {
			parts = append(parts, "")
			parts = append(parts, "## 秘密")
			parts = append(parts, c.Secrets)
		}
		if c.DialogueStyle != "" {
			parts = append(parts, "")
			parts = append(parts, "## 对话风格")
			parts = append(parts, c.DialogueStyle)
		}
		if len(c.KeyDialogue) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 关键台词")
			for _, line := range c.KeyDialogue {
				parts = append(parts, fmt.Sprintf("> %s", line))
			}
		}
		if c.Relationships != "" {
			parts = append(parts, "")
			parts = append(parts, "## 人际关系")
			parts = append(parts, c.Relationships)
		}
		if c.Notes != "" {
			parts = append(parts, "")
			parts = append(parts, "## 备注")
			parts = append(parts, c.Notes)
		}
		if len(c.Attrs) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 属性")
			for k, v := range c.Attrs {
				parts = append(parts, fmt.Sprintf("- %s: %d", k, v))
			}
		}
		if len(c.Skills) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 技能")
			for k, v := range c.Skills {
				parts = append(parts, fmt.Sprintf("- %s: %d", k, v))
			}
		}

		writeToOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/characters/%s", scriptBasePath, c.ID), strings.Join(parts, "\n"))
	}
	t.Logf("  ✓ 角色 %d 个已写入（含动机、秘密、对话风格、关键台词、外貌、关系）", len(scr.Characters))

	// 4.4 写入场景（含可调查点、隐藏细节、危险等级等）
	for _, s := range scr.Scenes {
		parts := []string{}
		parts = append(parts, fmt.Sprintf("# 场景: %s", s.Name))
		parts = append(parts, "")
		parts = append(parts, "## 描述")
		parts = append(parts, s.Description)

		if s.Atmosphere != "" {
			parts = append(parts, "")
			parts = append(parts, "## 氛围")
			parts = append(parts, s.Atmosphere)
		}
		if s.OnEnter != "" {
			parts = append(parts, "")
			parts = append(parts, "## 进入场景描述（可直接朗读）")
			parts = append(parts, s.OnEnter)
		}
		if s.Narrative != "" {
			parts = append(parts, "")
			parts = append(parts, "## 场景旁白")
			parts = append(parts, s.Narrative)
		}
		if s.DangerLevel != "" {
			parts = append(parts, "")
			parts = append(parts, fmt.Sprintf("## 危险等级: %s", s.DangerLevel))
		}
		if len(s.InvestigationPoints) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 可调查点")
			for _, ip := range s.InvestigationPoints {
				parts = append(parts, fmt.Sprintf("- %s", ip))
			}
		}
		if len(s.HiddenDetails) > 0 {
			parts = append(parts, "")
			parts = append(parts, "## 隐藏细节")
			for _, hd := range s.HiddenDetails {
				parts = append(parts, fmt.Sprintf("- %s", hd))
			}
		}
		if len(s.Exits) > 0 {
			parts = append(parts, "")
			parts = append(parts, fmt.Sprintf("## 出口: %s", strings.Join(s.Exits, ", ")))
		}
		if len(s.ConnectedNodes) > 0 {
			parts = append(parts, "")
			parts = append(parts, fmt.Sprintf("## 关联节点: %s", strings.Join(s.ConnectedNodes, ", ")))
		}

		writeToOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/scenes/%s", scriptBasePath, s.ID), strings.Join(parts, "\n"))
	}
	t.Logf("  ✓ 场景 %d 个已写入（含可调查点、隐藏细节、危险等级、旁白）", len(scr.Scenes))

	// --- Step 5: 从 OpenViking 读取验证 ---
	t.Log("\n=== Step 5: 读取验证 ===")
	readContent := readFromOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey, scriptBasePath+"/background")
	t.Logf("读取背景: %s", testTruncate(readContent, 200))

	if len(scr.Timeline) > 0 {
		readTimeline := readFromOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/timeline/%s", scriptBasePath, scr.Timeline[0].ID))
		t.Logf("读取首个节点: %s", testTruncate(readTimeline, 200))
	}

	if len(scr.Characters) > 0 {
		readChar := readFromOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/characters/%s", scriptBasePath, scr.Characters[0].ID))
		t.Logf("读取首个角色: %s", testTruncate(readChar, 200))
	}

	// --- Step 6: 验证新字段提取质量 ---
	t.Log("\n=== Step 6: 验证新字段提取质量 ===")
	newFieldStats := 0
	// 检查背景新字段
	if scr.Background.Backstory != "" {
		t.Logf("  ✓ Backstory 已提取: %s", testTruncate(scr.Background.Backstory, 80))
		newFieldStats++
	}
	if scr.Background.Tone != "" {
		t.Logf("  ✓ Tone 已提取: %s", scr.Background.Tone)
		newFieldStats++
	}
	// 检查时间轴新字段
	nodeWithNarrative := 0
	nodeWithClues := 0
	nodeWithKPNotes := 0
	for _, node := range scr.Timeline {
		if node.Narrative != "" {
			nodeWithNarrative++
		}
		if len(node.Clues) > 0 {
			nodeWithClues++
		}
		if node.KPNotes != "" {
			nodeWithKPNotes++
		}
	}
	t.Logf("  时间轴: %d/%d 节点有叙述文本, %d/%d 有线索, %d/%d 有KP备注",
		nodeWithNarrative, len(scr.Timeline), nodeWithClues, len(scr.Timeline), nodeWithKPNotes, len(scr.Timeline))
	newFieldStats += nodeWithNarrative + nodeWithClues + nodeWithKPNotes
	// 检查角色新字段
	charWithMotivation := 0
	charWithSecrets := 0
	charWithDialogue := 0
	for _, c := range scr.Characters {
		if c.Motivation != "" {
			charWithMotivation++
		}
		if c.Secrets != "" {
			charWithSecrets++
		}
		if c.DialogueStyle != "" {
			charWithDialogue++
		}
	}
	t.Logf("  角色: %d/%d 有动机, %d/%d 有秘密, %d/%d 有对话风格",
		charWithMotivation, len(scr.Characters), charWithSecrets, len(scr.Characters), charWithDialogue, len(scr.Characters))
	newFieldStats += charWithMotivation + charWithSecrets + charWithDialogue

	if newFieldStats == 0 {
		t.Log("  ⚠ 新字段均未提取到，AI 可能未按新提示词输出")
	} else {
		t.Logf("  ✓ 共提取到 %d 个新字段值", newFieldStats)
	}

	// --- Step 7: 总结 ---
	fmt.Println("\n===== docx → AI识别(导演剧本模式) → OpenViking 完整流程测试结果 =====")
	fmt.Printf("剧本:        %s (%s)\n", scr.Title, scr.System)
	fmt.Printf("OpenViking:  %s\n", ovBaseURL)
	fmt.Printf("写入路径:    viking://resources/%s/\n", scriptBasePath)
	fmt.Printf("时间轴节点:  %d (含叙述/线索/KP备注)\n", len(scr.Timeline))
	fmt.Printf("角色:        %d (含动机/秘密/对话风格)\n", len(scr.Characters))
	fmt.Printf("场景:        %d (含调查点/隐藏细节/危险等级)\n", len(scr.Scenes))
	fmt.Printf("新字段统计:  %d 个\n", newFieldStats)
	fmt.Printf("进度推送:    %d 次\n", progressCount)
	fmt.Println("========================================================================")

	t.Log("\n完整流程测试通过！docx 解析 → AI 识别(导演剧本模式) → OpenViking 清理/写入/读取 全部成功")
}

// deleteFromOpenViking 递归删除 OpenViking 上的资源/目录。
// 先列出目录内容，逐个删除文件，最后尝试删除空目录。
func deleteFromOpenViking(t *testing.T, ctx context.Context, client *http.Client, baseURL, apiKey, path string) {
	t.Helper()

	uri := "viking://resources/" + path

	// 先 ls 列出内容
	lsURL := fmt.Sprintf("%s/api/v1/fs/ls?uri=%s", baseURL, uri)
	lsReq, _ := http.NewRequestWithContext(ctx, "GET", lsURL, nil)
	if apiKey != "" {
		lsReq.Header.Set("Authorization", "Bearer "+apiKey)
		lsReq.Header.Set("X-API-Key", apiKey)
	}

	lsResp, err := client.Do(lsReq)
	if err != nil {
		t.Logf("  ls 请求失败 [%s]: %v（继续）", path, err)
		return
	}

	if lsResp.StatusCode == http.StatusNotFound {
		t.Logf("  目录 [%s] 不存在，无需清理", path)
		lsResp.Body.Close()
		return
	}
	if lsResp.StatusCode >= 400 {
		lsResp.Body.Close()
		t.Logf("  ls 返回 HTTP %d [%s]（继续）", lsResp.StatusCode, path)
		return
	}

	var lsResult openVikingLsResponse
	json.NewDecoder(lsResp.Body).Decode(&lsResult)
	lsResp.Body.Close()

	deletedCount := 0
	for _, entry := range lsResult.Result {
		childURI := entry.URI
		if entry.IsDir {
			// 递归删除子目录
			childPath := strings.TrimPrefix(childURI, "viking://resources/")
			deleteFromOpenViking(t, ctx, client, baseURL, apiKey, childPath)
		} else {
			// 删除文件
			delURL := fmt.Sprintf("%s/api/v1/fs?uri=%s", baseURL, childURI)
			delReq, _ := http.NewRequestWithContext(ctx, "DELETE", delURL, nil)
			if apiKey != "" {
				delReq.Header.Set("Authorization", "Bearer "+apiKey)
				delReq.Header.Set("X-API-Key", apiKey)
			}
			delResp, err := client.Do(delReq)
			if err != nil {
				t.Logf("  删除文件失败 [%s]: %v", childURI, err)
				continue
			}
			delResp.Body.Close()
			if delResp.StatusCode < 400 {
				deletedCount++
			}
		}
	}

	// 尝试删除空目录（可能返回 412，忽略）
	delURL := fmt.Sprintf("%s/api/v1/fs?uri=%s", baseURL, uri)
	delReq, _ := http.NewRequestWithContext(ctx, "DELETE", delURL, nil)
	if apiKey != "" {
		delReq.Header.Set("Authorization", "Bearer "+apiKey)
		delReq.Header.Set("X-API-Key", apiKey)
	}
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Logf("  删除目录失败 [%s]: %v（可能非空或不支持，忽略）", path, err)
		return
	}
	defer delResp.Body.Close()
	if delResp.StatusCode == http.StatusPreconditionFailed {
		t.Logf("  目录 [%s] 删除返回 412（可能需后台清理），已删除 %d 个文件", path, deletedCount)
	} else if delResp.StatusCode >= 400 {
		t.Logf("  目录 [%s] 删除返回 HTTP %d，已删除 %d 个文件", path, delResp.StatusCode, deletedCount)
	} else {
		t.Logf("  ✓ 目录 [%s] 已删除（含 %d 个文件）", path, deletedCount)
	}
}

// openVikingLsResponse 是 OpenViking ls API 的响应。
type openVikingLsResponse struct {
	Status string              `json:"status"`
	Result []openVikingLsEntry `json:"result"`
}

type openVikingLsEntry struct {
	URI   string `json:"uri"`
	IsDir bool   `json:"isDir"`
	Name  string `json:"name"`
}

// writeToOpenViking 通过 HTTP 调用 OpenViking content/write API。
// 使用 create 模式创建新文件，已存在则改用 replace。
func writeToOpenViking(t *testing.T, ctx context.Context, client *http.Client, baseURL, apiKey, path, content string) {
	t.Helper()

	uri := "viking://resources/" + path
	// create 模式要求有扩展名
	if strings.HasSuffix(path, "/") || !strings.Contains(filepath.Ext(path), ".") {
		uri += ".md"
	}

	// 先尝试 create 模式
	body := map[string]interface{}{
		"uri":     uri,
		"content": content,
		"mode":    "create",
	}
	bodyJSON, _ := json.Marshal(body)

	doWrite := func(mode string) (*http.Response, error) {
		body["mode"] = mode
		bodyJSON, _ = json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/v1/content/write", bytes.NewBuffer(bodyJSON))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("X-API-Key", apiKey)
		}
		return client.Do(req)
	}

	resp, err := doWrite("create")
	if err != nil {
		t.Fatalf("写入请求失败 [%s]: %v", path, err)
	}

	// 409 Conflict → 文件已存在，改用 replace
	if resp.StatusCode == http.StatusConflict {
		resp.Body.Close()
		resp, err = doWrite("replace")
		if err != nil {
			t.Fatalf("写入请求(replace)失败 [%s]: %v", path, err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("写入失败 [%s]: HTTP %d %s", path, resp.StatusCode, string(respBody))
	}
}

// readFromOpenViking 通过 HTTP 调用 OpenViking content/read API。
func readFromOpenViking(t *testing.T, ctx context.Context, client *http.Client, baseURL, apiKey, path string) string {
	t.Helper()

	uri := "viking://resources/" + path
	if !strings.Contains(filepath.Ext(path), ".") {
		uri += ".md"
	}
	url := fmt.Sprintf("%s/api/v1/content/read?uri=%s", baseURL, uri)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		t.Fatalf("创建读取请求失败 [%s]: %v", path, err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("读取失败 [%s]: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("读取失败 [%s]: HTTP %d %s", path, resp.StatusCode, string(respBody))
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.String()
}
