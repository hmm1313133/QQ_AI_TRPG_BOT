package script

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMultiAgentAnalyze_活神之手 测试多阶段多 Agent 流水线分析剧本。
//
// 完整流程：docx 解析 -> Phase1 Planner -> Phase2 并行 Extractor -> Phase3 Integrator -> 生成 JSON 结果文件
//
// 运行: go test -v -run "TestMultiAgentAnalyze_活神之手" ./internal/script/ -timeout 600s
func TestMultiAgentAnalyze_活神之手(t *testing.T) {
	cfg := getAnalyzerConfigFromEnv()
	if cfg.LLMAPIKey == "" {
		t.Skip("未设置 LLM_API_KEY，跳过 AI 分析测试")
	}
	t.Logf("模型: %s, API: %s", cfg.LLMModel, cfg.LLMBaseURL)

	// === Step 1: 解析 docx 文件 ===
	path := findDocxFile(t)
	fileInfo, _ := os.Stat(path)
	t.Logf("=== Step 1: 解析文件 %s (%.2f KB) ===", filepath.Base(path), float64(fileInfo.Size())/1024)

	parseStart := time.Now()
	text, err := ParseFile(path)
	parseElapsed := time.Since(parseStart)
	if err != nil {
		t.Fatalf("文件解析失败: %v", err)
	}
	textLen := len([]rune(text))
	t.Logf("解析完成: %d 字符, 耗时 %s", textLen, parseElapsed)

	if textLen < 100 {
		t.Fatalf("解析文本过短 (%d 字符)，可能解析异常", textLen)
	}

	// 预览前 500 字符
	preview := []rune(text)
	if len(preview) > 500 {
		preview = preview[:500]
	}
	t.Logf("文本预览:\n%s", string(preview))

	// === Step 2: 初始化多阶段流水线 Analyzer ===
	t.Log("\n=== Step 2: 初始化 ScriptAnalyzer（多阶段流水线）===")
	analyzer, err := NewScriptAnalyzer(cfg)
	if err != nil {
		t.Fatalf("创建 Analyzer 失败: %v", err)
	}

	// === Step 3: 执行三阶段 AI 分析 ===
	t.Log("\n=== Step 3: 多阶段 AI 剧本分析 ===")
	t.Log("  Phase 1: Planner - 通读全文，制定提取计划")
	t.Log("  Phase 2: 4个 Extractor 并行 - 工具调用 Agent 自主读取段落提取模块")
	t.Log("  Phase 3: Integrator - 整合交叉引用")

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

	// === Step 4: 验证识别结果 ===
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
	t.Logf("\n故事背景:")
	t.Logf("  设定: %s", testTruncate(scr.Background.Setting, 150))
	t.Logf("  时代: %s", scr.Background.Era)
	t.Logf("  地点: %s", scr.Background.Location)
	t.Logf("  氛围: %s", scr.Background.Atmosphere)
	t.Logf("  主题: %s", scr.Background.MainTheme)
	t.Logf("  基调: %s", scr.Background.Tone)
	if scr.Background.Synopsis != "" {
		t.Logf("  梗概: %s", testTruncate(scr.Background.Synopsis, 300))
	}
	if scr.Background.Backstory != "" {
		t.Logf("  详细背景: %s", testTruncate(scr.Background.Backstory, 300))
	}
	if len(scr.Background.KeyOrganizations) > 0 {
		t.Logf("  关键组织: %v", scr.Background.KeyOrganizations)
	}
	if len(scr.Background.KeyThemes) > 0 {
		t.Logf("  核心冲突: %v", scr.Background.KeyThemes)
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
				t.Logf("     描述: %s", testTruncate(node.Description, 100))
			}
			if node.Narrative != "" {
				t.Logf("     叙述: %s", testTruncate(node.Narrative, 100))
			}
			if len(node.Clues) > 0 {
				t.Logf("     线索: %s", strings.Join(node.Clues, "; "))
			}
			if len(node.Encounters) > 0 {
				t.Logf("     遭遇: %s", strings.Join(node.Encounters, "; "))
			}
			if len(node.Objectives) > 0 {
				t.Logf("     目标: %s", strings.Join(node.Objectives, "; "))
			}
			if node.KPNotes != "" {
				t.Logf("     KP备注: %s", testTruncate(node.KPNotes, 80))
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
			t.Logf("    性格: %s", testTruncate(c.Personality, 80))
			if c.Background != "" {
				t.Logf("    背景: %s", testTruncate(c.Background, 80))
			}
			if c.Motivation != "" {
				t.Logf("    动机: %s", testTruncate(c.Motivation, 80))
			}
			if c.Secrets != "" {
				t.Logf("    秘密: %s", testTruncate(c.Secrets, 80))
			}
			if c.DialogueStyle != "" {
				t.Logf("    对话风格: %s", c.DialogueStyle)
			}
			if len(c.KeyDialogue) > 0 {
				t.Logf("    关键台词: %s", strings.Join(c.KeyDialogue, " / "))
			}
			if c.Appearance != "" {
				t.Logf("    外貌: %s", testTruncate(c.Appearance, 80))
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
			t.Logf("  - %s: %s", s.Name, testTruncate(s.Description, 80))
			if s.OnEnter != "" {
				t.Logf("    进入描述: %s", testTruncate(s.OnEnter, 80))
			}
			if s.DangerLevel != "" {
				t.Logf("    危险等级: %s", s.DangerLevel)
			}
			if len(s.InvestigationPoints) > 0 {
				t.Logf("    可调查: %s", strings.Join(s.InvestigationPoints, "; "))
			}
			if len(s.HiddenDetails) > 0 {
				t.Logf("    隐藏细节: %s", strings.Join(s.HiddenDetails, "; "))
			}
		}
	}

	// === Step 5: 验证 ID 补全 ===
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
	t.Log("  ✓ ID/Order 补全验证通过")

	// === Step 6: 统计新字段提取质量 ===
	t.Log("\n=== Step 6: 统计字段提取质量 ===")
	stats := 0

	// 背景新字段
	if scr.Background.Backstory != "" {
		stats++
	}
	if scr.Background.Tone != "" {
		stats++
	}
	if len(scr.Background.KeyThemes) > 0 {
		stats++
	}

	// 时间轴新字段
	nodeWithNarrative := 0
	nodeWithClues := 0
	nodeWithKPNotes := 0
	nodeWithEncounters := 0
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
		if len(node.Encounters) > 0 {
			nodeWithEncounters++
		}
	}
	t.Logf("  时间轴: %d/%d 有叙述, %d/%d 有线索, %d/%d 有遭遇, %d/%d 有KP备注",
		nodeWithNarrative, len(scr.Timeline),
		nodeWithClues, len(scr.Timeline),
		nodeWithEncounters, len(scr.Timeline),
		nodeWithKPNotes, len(scr.Timeline))
	stats += nodeWithNarrative + nodeWithClues + nodeWithKPNotes + nodeWithEncounters

	// 角色新字段
	charWithMotivation := 0
	charWithSecrets := 0
	charWithDialogue := 0
	charWithKeyDialogue := 0
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
		if len(c.KeyDialogue) > 0 {
			charWithKeyDialogue++
		}
	}
	t.Logf("  角色: %d/%d 有动机, %d/%d 有秘密, %d/%d 有对话风格, %d/%d 有关键台词",
		charWithMotivation, len(scr.Characters),
		charWithSecrets, len(scr.Characters),
		charWithDialogue, len(scr.Characters),
		charWithKeyDialogue, len(scr.Characters))
	stats += charWithMotivation + charWithSecrets + charWithDialogue + charWithKeyDialogue

	// 场景新字段
	sceneWithInvestigation := 0
	sceneWithHidden := 0
	sceneWithDanger := 0
	for _, s := range scr.Scenes {
		if len(s.InvestigationPoints) > 0 {
			sceneWithInvestigation++
		}
		if len(s.HiddenDetails) > 0 {
			sceneWithHidden++
		}
		if s.DangerLevel != "" {
			sceneWithDanger++
		}
	}
	t.Logf("  场景: %d/%d 有调查点, %d/%d 有隐藏细节, %d/%d 有危险等级",
		sceneWithInvestigation, len(scr.Scenes),
		sceneWithHidden, len(scr.Scenes),
		sceneWithDanger, len(scr.Scenes))
	stats += sceneWithInvestigation + sceneWithHidden + sceneWithDanger

	t.Logf("  ✓ 共提取到 %d 个增强字段值", stats)

	// === Step 7: 生成结果 JSON 文件 ===
	t.Log("\n=== Step 7: 生成结果文件 ===")

	// 确定输出目录（项目根目录下的 test_output/）
	outputDir := filepath.Join("..", "..", "test_output")
	if dir, err := os.Getwd(); err == nil {
		outputDir = filepath.Join(dir, "..", "..", "test_output")
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("创建输出目录失败: %v", err)
	}

	// 7.1 完整 JSON 结果文件
	jsonPath := filepath.Join(outputDir, scr.Name+"_analysis.json")
	jsonData, err := json.MarshalIndent(scr, "", "  ")
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		t.Fatalf("写入 JSON 文件失败: %v", err)
	}
	t.Logf("  ✓ 完整 JSON 结果: %s (%d bytes)", jsonPath, len(jsonData))

	// 7.2 可读的 Markdown 报告文件
	mdPath := filepath.Join(outputDir, scr.Name+"_report.md")
	mdContent := generateMarkdownReport(scr, parseElapsed, analyzeElapsed, stats)
	if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
		t.Fatalf("写入 Markdown 报告失败: %v", err)
	}
	t.Logf("  ✓ Markdown 报告: %s", mdPath)

	// === Step 8: 总结 ===
	fmt.Println("\n========== 多阶段多 Agent 流水线分析结果 ==========")
	fmt.Printf("文件:          %s (%.2f KB)\n", filepath.Base(path), float64(fileInfo.Size())/1024)
	fmt.Printf("文本长度:      %d 字符\n", textLen)
	fmt.Printf("解析耗时:      %s\n", parseElapsed.Round(time.Millisecond))
	fmt.Printf("AI 分析耗时:   %s\n", analyzeElapsed.Round(time.Millisecond))
	fmt.Printf("总耗时:        %s\n", (parseElapsed + analyzeElapsed).Round(time.Millisecond))
	fmt.Printf("标题:          %s\n", scr.Title)
	fmt.Printf("名称:          %s\n", scr.Name)
	fmt.Printf("规则集:        %s\n", scr.System)
	fmt.Printf("时间轴节点:    %d\n", len(scr.Timeline))
	fmt.Printf("登场角色:      %d\n", len(scr.Characters))
	fmt.Printf("场景:          %d\n", len(scr.Scenes))
	fmt.Printf("增强字段统计:  %d\n", stats)
	fmt.Printf("进度推送:      %d 次\n", progressCount)
	fmt.Printf("JSON 结果文件: %s\n", jsonPath)
	fmt.Printf("MD 报告文件:   %s\n", mdPath)
	fmt.Println("====================================================")

	t.Logf("\n测试通过！结果文件已生成:\n  JSON: %s\n  MD:   %s", jsonPath, mdPath)
}

// generateMarkdownReport 生成可读的 Markdown 分析报告。
func generateMarkdownReport(scr *Script, parseElapsed, analyzeElapsed time.Duration, stats int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", scr.Title))
	sb.WriteString(fmt.Sprintf("- **名称**: %s\n", scr.Name))
	sb.WriteString(fmt.Sprintf("- **规则集**: %s\n", scr.System))
	sb.WriteString(fmt.Sprintf("- **创建时间**: %s\n", scr.CreatedAt))
	sb.WriteString(fmt.Sprintf("- **源文件**: %s\n\n", scr.SourceFile))
	sb.WriteString(fmt.Sprintf("- **解析耗时**: %s\n", parseElapsed.Round(time.Millisecond)))
	sb.WriteString(fmt.Sprintf("- **AI分析耗时**: %s\n\n", analyzeElapsed.Round(time.Millisecond)))

	// 故事背景
	sb.WriteString("## 故事背景\n\n")
	sb.WriteString(fmt.Sprintf("- **设定**: %s\n", scr.Background.Setting))
	sb.WriteString(fmt.Sprintf("- **时代**: %s\n", scr.Background.Era))
	sb.WriteString(fmt.Sprintf("- **地点**: %s\n", scr.Background.Location))
	sb.WriteString(fmt.Sprintf("- **氛围**: %s\n", scr.Background.Atmosphere))
	sb.WriteString(fmt.Sprintf("- **主题**: %s\n", scr.Background.MainTheme))
	if scr.Background.Tone != "" {
		sb.WriteString(fmt.Sprintf("- **叙事基调**: %s\n", scr.Background.Tone))
	}
	if len(scr.Background.KeyOrganizations) > 0 {
		sb.WriteString(fmt.Sprintf("- **关键组织**: %s\n", strings.Join(scr.Background.KeyOrganizations, ", ")))
	}
	if len(scr.Background.KeyThemes) > 0 {
		sb.WriteString(fmt.Sprintf("- **核心冲突**: %s\n", strings.Join(scr.Background.KeyThemes, ", ")))
	}
	if scr.Background.Synopsis != "" {
		sb.WriteString("\n### 故事梗概\n\n")
		sb.WriteString(scr.Background.Synopsis)
		sb.WriteString("\n")
	}
	if scr.Background.Backstory != "" {
		sb.WriteString("\n### 详细背景\n\n")
		sb.WriteString(scr.Background.Backstory)
		sb.WriteString("\n")
	}

	// 时间轴
	sb.WriteString(fmt.Sprintf("\n## 时间轴 (%d 节点)\n\n", len(scr.Timeline)))
	for i, node := range scr.Timeline {
		keyMark := ""
		if node.IsKeyNode {
			keyMark = " ⭐关键"
		}
		sb.WriteString(fmt.Sprintf("### %d. %s (%s)%s\n\n", i+1, node.Name, node.Type, keyMark))
		if node.Description != "" {
			sb.WriteString(node.Description)
			sb.WriteString("\n\n")
		}
		if node.Narrative != "" {
			sb.WriteString("**叙述文本:**\n\n> ")
			sb.WriteString(node.Narrative)
			sb.WriteString("\n\n")
		}
		if len(node.Triggers) > 0 {
			sb.WriteString("**触发条件:**\n")
			for _, tr := range node.Triggers {
				sb.WriteString(fmt.Sprintf("- %s\n", tr))
			}
			sb.WriteString("\n")
		}
		if len(node.Clues) > 0 {
			sb.WriteString("**线索:**\n")
			for _, clue := range node.Clues {
				sb.WriteString(fmt.Sprintf("- %s\n", clue))
			}
			sb.WriteString("\n")
		}
		if len(node.Encounters) > 0 {
			sb.WriteString("**遭遇:**\n")
			for _, enc := range node.Encounters {
				sb.WriteString(fmt.Sprintf("- %s\n", enc))
			}
			sb.WriteString("\n")
		}
		if len(node.Objectives) > 0 {
			sb.WriteString("**目标:**\n")
			for _, obj := range node.Objectives {
				sb.WriteString(fmt.Sprintf("- %s\n", obj))
			}
			sb.WriteString("\n")
		}
		if len(node.Consequences) > 0 {
			sb.WriteString("**可能后果:**\n")
			for _, con := range node.Consequences {
				sb.WriteString(fmt.Sprintf("- %s\n", con))
			}
			sb.WriteString("\n")
		}
		if len(node.Branches) > 0 {
			sb.WriteString("**分支路径:**\n")
			for _, br := range node.Branches {
				sb.WriteString(fmt.Sprintf("- %s\n", br))
			}
			sb.WriteString("\n")
		}
		if len(node.NPCs) > 0 {
			sb.WriteString(fmt.Sprintf("**涉及NPC**: %s\n\n", strings.Join(node.NPCs, ", ")))
		}
		if node.KPNotes != "" {
			sb.WriteString("**KP备注:**\n\n")
			sb.WriteString(node.KPNotes)
			sb.WriteString("\n\n")
		}
		sb.WriteString("---\n\n")
	}

	// 角色
	sb.WriteString(fmt.Sprintf("## 角色 (%d)\n\n", len(scr.Characters)))
	for _, c := range scr.Characters {
		sb.WriteString(fmt.Sprintf("### %s (%s)\n\n", c.Name, c.Role))
		if c.Appearance != "" {
			sb.WriteString(fmt.Sprintf("**外貌**: %s\n\n", c.Appearance))
		}
		if c.Personality != "" {
			sb.WriteString(fmt.Sprintf("**性格**: %s\n\n", c.Personality))
		}
		if c.Background != "" {
			sb.WriteString(fmt.Sprintf("**背景**: %s\n\n", c.Background))
		}
		if c.Motivation != "" {
			sb.WriteString(fmt.Sprintf("**动机**: %s\n\n", c.Motivation))
		}
		if c.Secrets != "" {
			sb.WriteString(fmt.Sprintf("**秘密**: %s\n\n", c.Secrets))
		}
		if c.DialogueStyle != "" {
			sb.WriteString(fmt.Sprintf("**对话风格**: %s\n\n", c.DialogueStyle))
		}
		if len(c.KeyDialogue) > 0 {
			sb.WriteString("**关键台词:**\n")
			for _, line := range c.KeyDialogue {
				sb.WriteString(fmt.Sprintf("> %s\n\n", line))
			}
		}
		if c.Relationships != "" {
			sb.WriteString(fmt.Sprintf("**人际关系**: %s\n\n", c.Relationships))
		}
		if len(c.Attrs) > 0 {
			sb.WriteString("**属性:**\n")
			for k, v := range c.Attrs {
				sb.WriteString(fmt.Sprintf("- %s: %d\n", k, v))
			}
			sb.WriteString("\n")
		}
		if len(c.Skills) > 0 {
			sb.WriteString("**技能:**\n")
			for k, v := range c.Skills {
				sb.WriteString(fmt.Sprintf("- %s: %d\n", k, v))
			}
			sb.WriteString("\n")
		}
		if c.Notes != "" {
			sb.WriteString(fmt.Sprintf("**备注**: %s\n\n", c.Notes))
		}
		sb.WriteString("---\n\n")
	}

	// 场景
	sb.WriteString(fmt.Sprintf("## 场景 (%d)\n\n", len(scr.Scenes)))
	for _, s := range scr.Scenes {
		sb.WriteString(fmt.Sprintf("### %s\n\n", s.Name))
		if s.Description != "" {
			sb.WriteString(s.Description)
			sb.WriteString("\n\n")
		}
		if s.Atmosphere != "" {
			sb.WriteString(fmt.Sprintf("**氛围**: %s\n\n", s.Atmosphere))
		}
		if s.OnEnter != "" {
			sb.WriteString("**进入场景描述（可直接朗读）:**\n\n> ")
			sb.WriteString(s.OnEnter)
			sb.WriteString("\n\n")
		}
		if s.Narrative != "" {
			sb.WriteString("**场景旁白:**\n\n> ")
			sb.WriteString(s.Narrative)
			sb.WriteString("\n\n")
		}
		if s.DangerLevel != "" {
			sb.WriteString(fmt.Sprintf("**危险等级**: %s\n\n", s.DangerLevel))
		}
		if len(s.InvestigationPoints) > 0 {
			sb.WriteString("**可调查点:**\n")
			for _, ip := range s.InvestigationPoints {
				sb.WriteString(fmt.Sprintf("- %s\n", ip))
			}
			sb.WriteString("\n")
		}
		if len(s.HiddenDetails) > 0 {
			sb.WriteString("**隐藏细节:**\n")
			for _, hd := range s.HiddenDetails {
				sb.WriteString(fmt.Sprintf("- %s\n", hd))
			}
			sb.WriteString("\n")
		}
		if len(s.Exits) > 0 {
			sb.WriteString(fmt.Sprintf("**出口**: %s\n\n", strings.Join(s.Exits, ", ")))
		}
		if len(s.ConnectedNodes) > 0 {
			sb.WriteString(fmt.Sprintf("**关联节点**: %s\n\n", strings.Join(s.ConnectedNodes, ", ")))
		}
		sb.WriteString("---\n\n")
	}

	return sb.String()
}
