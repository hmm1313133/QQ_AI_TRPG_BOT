// 剧本管理 Handler：处理 .script / .progress / .timeline 指令。
//
// 指令列表:
//   .script upload <文件路径>  上传并分析剧本文件（PDF/Word/文本）
//   .script list              列出所有可用剧本
//   .script load <名称>        加载剧本到当前会话
//   .script info [名称]        查看剧本信息
//   .script remove <名称>      删除剧本
//   .script unload             卸载当前剧本
//   .progress                  查看跑团进度
//   .timeline                  查看时间轴状态
package handler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
)

// ScriptHandler 处理剧本管理指令。
type ScriptHandler struct {
	archive         *script.Archive
	analyzer        *script.ScriptAnalyzer
	progressTracker *trpg.ProgressTracker
	timelineEngine  *trpg.TimelineEngine
	sessionMgr      *core.SessionManager
	svc             *trpg.Service
	gameStateStore  *agent.GameStateStore
}

// NewScriptHandler 创建剧本管理处理器。
func NewScriptHandler(
	archive *script.Archive,
	analyzer *script.ScriptAnalyzer,
	progressTracker *trpg.ProgressTracker,
	timelineEngine *trpg.TimelineEngine,
	sessionMgr *core.SessionManager,
	svc *trpg.Service,
	gameStateStore *agent.GameStateStore,
) *ScriptHandler {
	return &ScriptHandler{
		archive:         archive,
		analyzer:        analyzer,
		progressTracker: progressTracker,
		timelineEngine:  timelineEngine,
		sessionMgr:      sessionMgr,
		svc:             svc,
		gameStateStore:  gameStateStore,
	}
}

func (h *ScriptHandler) Name() string { return "script" }

func (h *ScriptHandler) Match(ctx *core.MessageContext) bool {
	return strings.HasPrefix(ctx.Content, ".script ") || ctx.Content == ".script" ||
		ctx.Content == ".progress" || ctx.Content == ".timeline"
}

func (h *ScriptHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	content := strings.TrimSpace(ctx.Content)

	if content == ".progress" {
		return h.handleProgress(ctx, reply)
	}
	if content == ".timeline" {
		return h.handleTimeline(ctx, reply)
	}

	// .script 子命令
	parts := strings.SplitN(content, " ", 3)
	if len(parts) < 2 {
		return h.showScriptHelp(ctx, reply)
	}

	subCmd := parts[1]
	switch subCmd {
	case "upload", "analyze":
		return h.handleUpload(ctx, reply, parts)
	case "text":
		return h.handleText(ctx, reply, parts)
	case "list", "ls":
		return h.handleList(ctx, reply)
	case "load":
		return h.handleLoad(ctx, reply, parts)
	case "info":
		return h.handleInfo(ctx, reply, parts)
	case "remove", "rm":
		return h.handleRemove(ctx, reply, parts)
	case "unload":
		return h.handleUnload(ctx, reply)
	default:
		return h.showScriptHelp(ctx, reply)
	}
}

// handleUpload 处理剧本上传和分析。
// 支持多种输入方式:
//   - .script upload <本地文件路径>  读取服务器本地文件
//   - .script upload <HTTP(S) URL>   下载远程文件
//   - .script upload + 文件附件      用户直接发送文件附件（同一条消息）
//   - 先发文件 + .script upload      文件和指令分两条消息发送
//   - .script upload + 后发文件      先发指令再发文件（触发等待状态）
//   - .script text <剧本内容>        直接粘贴文本
func (h *ScriptHandler) handleUpload(ctx *core.MessageContext, reply core.ReplyFunc, parts []string) error {
	if h.analyzer == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "剧本分析器未初始化", ctx.IsGroup)
	}

	// 1. 优先检查当前消息中的文件附件（文件+指令同条消息）
	if fileAtt := ctx.GetFileAttachment(); fileAtt != nil {
		return h.handleUploadAttachment(ctx, reply, fileAtt)
	}

	// 2. 检查参数（路径或URL）
	if len(parts) >= 3 && parts[2] != "" {
		input := strings.TrimSpace(parts[2])
		isURL := strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
		sourceDesc := input
		if isURL {
			sourceDesc = "URL: " + input
		}

		log.Printf("[ScriptHandler] 上传剧本: %s, isURL=%v", sourceDesc, isURL)
		reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("正在获取剧本: %s", sourceDesc), ctx.IsGroup)

		go func() {
			var text string
			var err error
			if isURL {
				text, err = script.ParseFromURL(input)
			} else {
				text, err = script.ParseFile(input)
			}
			if err != nil {
				log.Printf("[ScriptHandler] 获取/解析失败: %v", err)
				reply(context.Background(), ctx.OpenID, ctx.MsgID,
					fmt.Sprintf("剧本解析失败: %v", err), ctx.IsGroup)
				return
			}
			log.Printf("[ScriptHandler] 文件解析成功: %s, %d 字符", input, len([]rune(text)))
			h.analyzeAndSave(ctx, reply, text, input)
		}()
		return nil
	}

	// 3. 无参数，检查会话中缓存的文件附件（先发文件后发指令的场景）
	session := h.sessionMgr.GetSession(ctx.SessionID)
	if cached, ok := session.Get("last_file_attachment"); ok {
		if fileAtt, ok := cached.(*core.Attachment); ok && fileAtt != nil {
			// 检查时效性（5分钟内有效）
			if fileTimeVal, ok := session.Get("last_file_time"); ok {
				if fileTime, ok := fileTimeVal.(int64); ok && time.Now().Unix()-fileTime < 300 {
					session.Set("last_file_attachment", nil) // 清除缓存
					session.Set("last_file_time", nil)
					return h.handleUploadAttachment(ctx, reply, fileAtt)
				}
			}
		}
	}

	// 4. 无文件，设置等待状态（先发指令后发文件的场景）
	session.Set("waiting_script_upload", true)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		"请发送剧本文件（支持 PDF/Word/文本）\n或使用 .script upload <路径或URL>",
		ctx.IsGroup)
}

// handleUploadAttachment 处理用户直接发送的文件附件。
func (h *ScriptHandler) handleUploadAttachment(ctx *core.MessageContext, reply core.ReplyFunc, att *core.Attachment) error {
	filename := att.Filename
	if filename == "" {
		filename = "uploaded_file"
	}

	log.Printf("[ScriptHandler] 处理文件附件: %s, contentType=%s, size=%d, url=%s",
		filename, att.ContentType, att.Size, att.URL)

	reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("正在下载剧本文件: %s", filename), ctx.IsGroup)

	go func() {
		log.Printf("[ScriptHandler] 开始下载附件: %s", att.URL)
		text, err := script.ParseFromURL(att.URL)
		if err != nil {
			log.Printf("[ScriptHandler] 附件下载/解析失败: %v", err)
			reply(context.Background(), ctx.OpenID, ctx.MsgID,
				fmt.Sprintf("附件下载/解析失败: %v", err), ctx.IsGroup)
			return
		}
		log.Printf("[ScriptHandler] 附件解析成功: %s, %d 字符", filename, len([]rune(text)))
		h.analyzeAndSave(ctx, reply, text, filename)
	}()

	return nil
}

// analyzeAndSave 执行 AI 分析并保存剧本，发送结果摘要。
// 所有上传路径（文件/URL/文本）最终都汇聚到这里。
// 通过 progress 回调向用户推送实时进度。
func (h *ScriptHandler) analyzeAndSave(ctx *core.MessageContext, reply core.ReplyFunc, text string, source string) {
	log.Printf("[ScriptHandler] analyzeAndSave 开始: source=%s, textLen=%d", source, len([]rune(text)))

	// 进度回调：将进度消息推送给用户
	progress := func(stage, message string) {
		log.Printf("[ScriptHandler] 进度 [%s]: %s", stage, message)
		// 各阶段关键节点推送消息；频繁的阶段（如单模块提取过程）不推送
		switch stage {
		case "parse_done", "planning", "planning_done",
			"extracting", "extracting_done",
			"integrating", "parsing":
			reply(context.Background(), ctx.OpenID, ctx.MsgID, message, ctx.IsGroup)
		}
	}

	scr, err := h.analyzer.Analyze(context.Background(), text, source, progress)
	if err != nil {
		log.Printf("[ScriptHandler] 分析失败: %v", err)
		reply(context.Background(), ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("剧本识别失败: %v", err), ctx.IsGroup)
		return
	}

	log.Printf("[ScriptHandler] 分析成功: %s, 正在保存", scr.Title)
	reply(context.Background(), ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("识别完成，正在保存剧本..."), ctx.IsGroup)

	if err := h.archive.Save(scr); err != nil {
		log.Printf("[ScriptHandler] 保存失败: %v", err)
		reply(context.Background(), ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("剧本保存失败: %v", err), ctx.IsGroup)
		return
	}

	log.Printf("[ScriptHandler] 保存成功: %s", scr.ID)

	// 最终摘要
	summary := fmt.Sprintf("剧本分析完成！\n")
	summary += fmt.Sprintf("标题: %s\n", scr.Title)
	summary += fmt.Sprintf("规则集: %s\n", scr.System)
	summary += fmt.Sprintf("故事背景: %s\n", scr.Background.Setting)
	if scr.Background.Synopsis != "" {
		summary += fmt.Sprintf("剧情梗概: %s\n", truncateStr(scr.Background.Synopsis, 200))
	}
	summary += fmt.Sprintf("\n时间轴节点: %d\n", len(scr.Timeline))
	summary += fmt.Sprintf("登场角色: %d\n", len(scr.Characters))
	summary += fmt.Sprintf("场景: %d\n", len(scr.Scenes))
	if len(scr.Characters) > 0 {
		summary += "\n角色列表:\n"
		for _, c := range scr.Characters {
			summary += fmt.Sprintf("  - %s (%s): %s\n", c.Name, c.Role, c.Personality)
		}
	}
	if len(scr.Timeline) > 0 {
		summary += "\n时间轴:\n"
		for i, node := range scr.Timeline {
			keyMark := ""
			if node.IsKeyNode {
				keyMark = " [关键]"
			}
			summary += fmt.Sprintf("  %d. %s (%s)%s\n", i+1, node.Name, node.Type, keyMark)
		}
	}
	summary += fmt.Sprintf("\n使用 .script load %s 加载此剧本", scr.Name)

	reply(context.Background(), ctx.OpenID, ctx.MsgID, summary, ctx.IsGroup)
}

// handleText 处理直接粘贴的剧本文本。
// 用法: .script text <剧本内容>
func (h *ScriptHandler) handleText(ctx *core.MessageContext, reply core.ReplyFunc, parts []string) error {
	if len(parts) < 3 || parts[2] == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .script text <剧本内容>\n直接粘贴剧本文本进行AI识别", ctx.IsGroup)
	}

	text := parts[2]
	if len(text) < 50 {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "剧本文本太短（至少50字符），请提供更完整的内容", ctx.IsGroup)
	}

	if h.analyzer == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "剧本分析器未初始化", ctx.IsGroup)
	}

	log.Printf("[ScriptHandler] 直接文本分析: %d 字符", len([]rune(text)))
	reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("收到剧本文本（%d 字符），开始 AI 识别...", len([]rune(text))), ctx.IsGroup)

	go func() {
		h.analyzeAndSave(ctx, reply, text, "direct_input.txt")
	}()

	return nil
}

// handleList 列出所有可用剧本。
func (h *ScriptHandler) handleList(ctx *core.MessageContext, reply core.ReplyFunc) error {
	scripts := h.archive.List()
	if len(scripts) == 0 {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "暂无可用剧本。\n使用 .script upload <文件路径> 上传剧本", ctx.IsGroup)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("可用剧本 (%d):\n", len(scripts)))
	for _, s := range scripts {
		sb.WriteString(fmt.Sprintf("  - %s | %s (%s) | 节点:%d 角色:%d\n",
			s.Name, s.Title, s.System, len(s.Timeline), len(s.Characters)))
	}
	sb.WriteString("\n使用 .script load <名称> 加载剧本")
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)
}

// handleLoad 加载剧本到当前会话。
func (h *ScriptHandler) handleLoad(ctx *core.MessageContext, reply core.ReplyFunc, parts []string) error {
	if len(parts) < 3 || parts[2] == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .script load <剧本名称>", ctx.IsGroup)
	}

	name := strings.TrimSpace(parts[2])
	scr, err := h.archive.GetByName(name)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("剧本 %s 不存在", name), ctx.IsGroup)
	}

	// 设置规则集
	if err := h.svc.SetRuleSet(ctx.SessionID, scr.System); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("设置规则集失败: %v", err), ctx.IsGroup)
	}

	// 存储剧本 ID 到会话
	session := h.sessionMgr.GetSession(ctx.SessionID)
	session.Set("script_id", scr.ID)
	session.Set("script_name", scr.Name)

	// 初始化进度
	if h.progressTracker != nil {
		_, err := h.progressTracker.GetOrCreateProgress(ctx.SessionID, scr.ID)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("初始化进度失败: %v", err), ctx.IsGroup)
		}

		// 初始化 GameState（多层架构：从剧本结构映射运行态）
		if h.gameStateStore != nil {
			if _, err := h.gameStateStore.InitFromScript(ctx.SessionID, scr); err != nil {
				log.Printf("[ScriptHandler] 初始化 GameState 失败: %v", err)
			}
		}

		// 启动时间轴定时器（如果已在 TRPG 模式）
		if h.timelineEngine != nil && session.Mode == core.ModeTRPG {
			h.startTimelineEngine(ctx.SessionID)
		}

		firstNode := scr.GetFirstNode()
		nodeInfo := "无节点"
		if firstNode != nil {
			nodeInfo = firstNode.Name
		}

		summary := fmt.Sprintf("剧本已加载: %s\n", scr.Title)
		summary += fmt.Sprintf("规则集: %s\n", scr.System)
		summary += fmt.Sprintf("故事背景: %s\n", scr.Background.Setting)
		summary += fmt.Sprintf("当前节点: %s\n", nodeInfo)
		summary += fmt.Sprintf("进度: 0/%d\n", scr.TotalNodes())
		if scr.Background.Synopsis != "" {
			summary += fmt.Sprintf("\n剧情梗概: %s", scr.Background.Synopsis)
		}
		summary += "\n\n使用 .mode trpg 进入跑团模式开始游戏"

		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, summary, ctx.IsGroup)
	}

	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("剧本 %s 已加载（进度追踪器未初始化）", scr.Title), ctx.IsGroup)
}

// handleInfo 显示剧本详情。
func (h *ScriptHandler) handleInfo(ctx *core.MessageContext, reply core.ReplyFunc, parts []string) error {
	var scr *script.Script
	var err error

	if len(parts) >= 3 && parts[2] != "" {
		scr, err = h.archive.GetByName(parts[2])
	} else {
		// 显示当前会话加载的剧本
		session := h.sessionMgr.GetSession(ctx.SessionID)
		if scriptID, ok := session.Get("script_id"); ok {
			scr, err = h.archive.Get(scriptID.(string))
		} else {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "当前会话未加载剧本。\n用法: .script info <剧本名称>", ctx.IsGroup)
		}
	}

	if err != nil || scr == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "剧本不存在", ctx.IsGroup)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("剧本: %s\n", scr.Title))
	sb.WriteString(fmt.Sprintf("名称: %s\n", scr.Name))
	sb.WriteString(fmt.Sprintf("规则集: %s\n", scr.System))
	sb.WriteString(fmt.Sprintf("创建时间: %s\n", scr.CreatedAt))
	sb.WriteString(fmt.Sprintf("来源文件: %s\n", scr.SourceFile))

	sb.WriteString(fmt.Sprintf("\n故事背景:\n"))
	sb.WriteString(fmt.Sprintf("  时代: %s\n", scr.Background.Era))
	sb.WriteString(fmt.Sprintf("  地点: %s\n", scr.Background.Location))
	sb.WriteString(fmt.Sprintf("  氛围: %s\n", scr.Background.Atmosphere))
	sb.WriteString(fmt.Sprintf("  主题: %s\n", scr.Background.MainTheme))
	if scr.Background.Synopsis != "" {
		sb.WriteString(fmt.Sprintf("  梗概: %s\n", scr.Background.Synopsis))
	}

	sb.WriteString(fmt.Sprintf("\n时间轴 (%d 节点):\n", len(scr.Timeline)))
	for i, node := range scr.Timeline {
		keyMark := ""
		if node.IsKeyNode {
			keyMark = " [关键]"
		}
		sb.WriteString(fmt.Sprintf("  %d. %s (%s)%s\n", i+1, node.Name, node.Type, keyMark))
		if node.Description != "" {
			sb.WriteString(fmt.Sprintf("     %s\n", truncateStr(node.Description, 60)))
		}
	}

	sb.WriteString(fmt.Sprintf("\n角色 (%d):\n", len(scr.Characters)))
	for _, c := range scr.Characters {
		sb.WriteString(fmt.Sprintf("  - %s (%s): %s\n", c.Name, c.Role, c.Personality))
	}

	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)
}

// handleRemove 删除剧本。
func (h *ScriptHandler) handleRemove(ctx *core.MessageContext, reply core.ReplyFunc, parts []string) error {
	if len(parts) < 3 || parts[2] == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .script remove <剧本名称>", ctx.IsGroup)
	}

	name := strings.TrimSpace(parts[2])
	scr, err := h.archive.GetByName(name)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("剧本 %s 不存在", name), ctx.IsGroup)
	}

	if err := h.archive.Remove(scr.ID); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("删除失败: %v", err), ctx.IsGroup)
	}

	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("剧本 %s 已删除", name), ctx.IsGroup)
}

// handleUnload 卸载当前会话的剧本。
func (h *ScriptHandler) handleUnload(ctx *core.MessageContext, reply core.ReplyFunc) error {
	session := h.sessionMgr.GetSession(ctx.SessionID)

	// 停止时间轴定时器
	if h.timelineEngine != nil {
		h.timelineEngine.Stop(ctx.SessionID)
	}

	// 清理会话状态
	session.Set("script_id", nil)
	session.Set("script_name", nil)

	// 删除进度
	if h.progressTracker != nil {
		_ = h.archive.DeleteProgress(ctx.SessionID)
	}

	// 删除 GameState（多层架构运行态）
	if h.gameStateStore != nil {
		_ = h.gameStateStore.Delete(ctx.SessionID)
	}

	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "剧本已卸载", ctx.IsGroup)
}

// handleProgress 显示跑团进度。
func (h *ScriptHandler) handleProgress(ctx *core.MessageContext, reply core.ReplyFunc) error {
	if h.progressTracker == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "进度追踪器未初始化", ctx.IsGroup)
	}

	progress := h.progressTracker.GetProgress(ctx.SessionID)
	if progress == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "当前会话未加载剧本", ctx.IsGroup)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("剧本: %s\n", progress.ScriptName))
	sb.WriteString(fmt.Sprintf("当前节点: %s (%s)\n", progress.CurrentNodeName, progress.CurrentNodeID))
	sb.WriteString(fmt.Sprintf("已完成: %d 个节点\n", progress.CompletedCount()))
	sb.WriteString(fmt.Sprintf("决策记录: %d 条\n", len(progress.PlayerDecisions)))
	sb.WriteString(fmt.Sprintf("最后更新: %s\n", progress.LastUpdate))

	if progress.StorySummary != "" {
		sb.WriteString(fmt.Sprintf("\n剧情摘要:\n%s\n", progress.StorySummary))
	}

	if len(progress.PlayerDecisions) > 0 {
		sb.WriteString("\n最近决策:\n")
		start := len(progress.PlayerDecisions) - 5
		if start < 0 {
			start = 0
		}
		for _, d := range progress.PlayerDecisions[start:] {
			sb.WriteString(fmt.Sprintf("  [%s] %s → %s\n", d.Timestamp, d.Action, d.Outcome))
		}
	}

	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)
}

// handleTimeline 显示时间轴状态。
func (h *ScriptHandler) handleTimeline(ctx *core.MessageContext, reply core.ReplyFunc) error {
	if h.timelineEngine == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "时间轴引擎未初始化", ctx.IsGroup)
	}

	status := h.timelineEngine.GetTimelineStatus(ctx.SessionID)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, status, ctx.IsGroup)
}

// showScriptHelp 显示剧本指令帮助。
func (h *ScriptHandler) showScriptHelp(ctx *core.MessageContext, reply core.ReplyFunc) error {
	help := `剧本管理指令:

  .script upload <路径或URL>  上传并分析剧本 (PDF/Word/文本)
  .script upload + 发送文件   直接发送文件附件+此指令
  .script text <剧本内容>     直接粘贴剧本文本进行识别
  .script list               列出所有剧本
  .script load <名称>        加载剧本到当前会话
  .script info [名称]        查看剧本详情
  .script remove <名称>      删除剧本
  .script unload             卸载当前剧本
  .progress                  查看跑团进度
  .timeline                  查看时间轴状态`
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, help, ctx.IsGroup)
}

// startTimelineEngine 启动时间轴定时器。
func (h *ScriptHandler) startTimelineEngine(sessionID string) {
	if h.timelineEngine == nil {
		return
	}

	h.timelineEngine.Start(sessionID, func(sid string, progress *script.Progress, idleCount int) string {
		return fmt.Sprintf("玩家们已经在这个场景停留了一段时间。也许是时候探索新的区域或推进调查了...")
	})
}

// truncateStr 截断字符串。
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

var _ core.Handler = (*ScriptHandler)(nil)
