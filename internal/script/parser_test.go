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
		MaxTokens:   8192,
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

	// 动态导入 store 包（避免 script 包直接依赖 store 包）
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
	httpClient := &http.Client{Timeout: 10 * time.Second}
	healthResp, err := httpClient.Do(healthReq)
	if err != nil {
		t.Skipf("OpenViking 不可达 (%v)，跳过写入测试", err)
	}
	healthResp.Body.Close()
	if healthResp.StatusCode >= 400 {
		t.Skipf("OpenViking 健康检查失败 (HTTP %d)，跳过写入测试", healthResp.StatusCode)
	}
	t.Log("OpenViking 连接成功")

	// --- Step 4: 写入剧本到 OpenViking ---
	t.Log("\n=== Step 4: 写入剧本到 OpenViking ===")
	scriptBasePath := "trpg_scripts/" + scr.Name

	// 写入剧本背景
	backgroundJSON := fmt.Sprintf(`{"title":"%s","system":"%s","era":"%s","location":"%s","atmosphere":"%s","theme":"%s","synopsis":"%s"}`,
		scr.Title, scr.System, scr.Background.Era, scr.Background.Location,
		scr.Background.Atmosphere, scr.Background.MainTheme, scr.Background.Synopsis)
	writeToOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey, scriptBasePath+"/background", backgroundJSON)
	t.Log("  ✓ 剧本背景已写入")

	// 写入时间轴
	for i, node := range scr.Timeline {
		content := fmt.Sprintf("节点 %d: %s (%s)\n描述: %s\n关键节点: %v",
			i+1, node.Name, node.Type, node.Description, node.IsKeyNode)
		writeToOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/timeline/%s", scriptBasePath, node.ID), content)
	}
	t.Logf("  ✓ 时间轴 %d 节点已写入", len(scr.Timeline))

	// 写入角色
	for _, c := range scr.Characters {
		content := fmt.Sprintf("角色: %s (%s)\n性格: %s\n背景: %s",
			c.Name, c.Role, c.Personality, c.Background)
		writeToOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/characters/%s", scriptBasePath, c.ID), content)
	}
	t.Logf("  ✓ 角色 %d 个已写入", len(scr.Characters))

	// 写入场景
	for _, s := range scr.Scenes {
		content := fmt.Sprintf("场景: %s\n描述: %s", s.Name, s.Description)
		writeToOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/scenes/%s", scriptBasePath, s.ID), content)
	}
	t.Logf("  ✓ 场景 %d 个已写入", len(scr.Scenes))

	// --- Step 5: 从 OpenViking 读取验证 ---
	t.Log("\n=== Step 5: 读取验证 ===")
	readContent := readFromOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey, scriptBasePath+"/background")
	t.Logf("读取背景: %s", testTruncate(readContent, 100))

	if len(scr.Timeline) > 0 {
		readTimeline := readFromOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/timeline/%s", scriptBasePath, scr.Timeline[0].ID))
		t.Logf("读取首个节点: %s", testTruncate(readTimeline, 80))
	}

	if len(scr.Characters) > 0 {
		readChar := readFromOpenViking(t, ctx, httpClient, ovBaseURL, ovAPIKey,
			fmt.Sprintf("%s/characters/%s", scriptBasePath, scr.Characters[0].ID))
		t.Logf("读取首个角色: %s", testTruncate(readChar, 80))
	}

	// --- Step 6: 总结 ---
	fmt.Println("\n===== docx → AI识别 → OpenViking 完整流程测试结果 =====")
	fmt.Printf("剧本:        %s (%s)\n", scr.Title, scr.System)
	fmt.Printf("OpenViking:  %s\n", ovBaseURL)
	fmt.Printf("写入路径:    viking://resources/%s/\n", scriptBasePath)
	fmt.Printf("时间轴节点:  %d\n", len(scr.Timeline))
	fmt.Printf("角色:        %d\n", len(scr.Characters))
	fmt.Printf("场景:        %d\n", len(scr.Scenes))
	fmt.Printf("进度推送:    %d 次\n", progressCount)
	fmt.Println("======================================================")

	t.Log("\n完整流程测试通过！docx 解析 → AI 识别 → OpenViking 写入/读取 全部成功")
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
