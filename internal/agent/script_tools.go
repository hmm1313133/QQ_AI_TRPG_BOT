// Package agent — 剧本相关工具集，供 KPAgent 在跑团时调用。
// 这些工具让 AI KP 能够读取剧本上下文、推进剧情时间轴、
// 查询/保存进度和获取 NPC 信息。
package agent

import (
	"context"
	"fmt"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ScriptDeps 封装剧本相关依赖，供工具函数使用。
type ScriptDeps struct {
	Archive         *script.Archive
	ProgressTracker *trpg.ProgressTracker
	TimelineEngine  *trpg.TimelineEngine
}

// NewScriptTools 创建剧本相关的 FunctionTool 集合。
func NewScriptTools(deps *ScriptDeps) []tool.Tool {
	if deps == nil {
		return nil
	}
	return []tool.Tool{
		NewGetScriptContextTool(deps),
		NewAdvanceTimelineTool(deps),
		NewGetProgressTool(deps),
		NewSaveProgressTool(deps),
		NewGetNPCTool(deps),
	}
}

// --- get_script_context tool ---

type GetScriptContextReq struct {
	IncludeTimeline bool `json:"include_timeline,omitempty" jsonschema:"description=是否包含完整时间轴信息"`
}

type GetScriptContextRsp struct {
	Title        string             `json:"title"`
	System       string             `json:"system"`
	Setting      string             `json:"setting"`
	Synopsis     string             `json:"synopsis"`
	CurrentNode  string             `json:"current_node"`
	NextNode     string             `json:"next_node,omitempty"`
	Timeline     []TimelineNodeInfo `json:"timeline,omitempty"`
	Found        bool               `json:"found"`
}

type TimelineNodeInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Completed bool   `json:"completed"`
	IsCurrent bool   `json:"is_current"`
}

func NewGetScriptContextTool(deps *ScriptDeps) tool.Tool {
	fn := func(ctx context.Context, req GetScriptContextReq) (GetScriptContextRsp, error) {
		sessionID, _, err := getSessionAndUser(ctx)
		if err != nil {
			return GetScriptContextRsp{}, err
		}

		if deps.ProgressTracker == nil {
			return GetScriptContextRsp{Found: false}, nil
		}

		progress := deps.ProgressTracker.GetProgress(sessionID)
		if progress == nil {
			return GetScriptContextRsp{Found: false}, nil
		}

		if deps.Archive == nil {
			return GetScriptContextRsp{Found: false}, nil
		}

		scr, err := deps.Archive.Get(progress.ScriptID)
		if err != nil {
			return GetScriptContextRsp{Found: false}, nil
		}

		rsp := GetScriptContextRsp{
			Title:    scr.Title,
			System:   scr.System,
			Setting:  scr.Background.Setting,
			Synopsis: scr.Background.Synopsis,
			CurrentNode: progress.CurrentNodeID,
			Found:    true,
		}

		// 下一个节点
		if next, err := scr.GetNextNode(progress.CurrentNodeID); err == nil && next != nil {
			rsp.NextNode = fmt.Sprintf("%s: %s", next.ID, next.Name)
		}

		// 时间轴
		if req.IncludeTimeline {
			for _, node := range scr.Timeline {
				rsp.Timeline = append(rsp.Timeline, TimelineNodeInfo{
					ID:        node.ID,
					Name:      node.Name,
					Type:      node.Type,
					Completed: progress.IsNodeCompleted(node.ID),
					IsCurrent: node.ID == progress.CurrentNodeID,
				})
			}
		}

		return rsp, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("get_script_context"),
		function.WithDescription(
			"获取当前剧本的上下文信息，包括故事背景、当前剧情节点和可选的完整时间轴。"+
				"用于了解当前跑团所处的剧情阶段和可推进的方向。"+
				"参数 include_timeline 设为 true 可获取完整时间轴列表。"),
	)
}

// --- advance_timeline tool ---

type AdvanceTimelineReq struct {
	NodeID    string `json:"node_id,omitempty" jsonschema:"description=目标节点ID。留空则自动推进到下一节点"`
	Reason    string `json:"reason,omitempty" jsonschema:"description=推进原因（如：玩家完成了某任务）"`
}

type AdvanceTimelineRsp struct {
	Success      bool   `json:"success"`
	OldNodeID    string `json:"old_node_id"`
	NewNodeID    string `json:"new_node_id"`
	NewNodeName  string `json:"new_node_name"`
	NewNodeDesc  string `json:"new_node_desc"`
	IsLastNode   bool   `json:"is_last_node"`
	Message      string `json:"message"`
}

func NewAdvanceTimelineTool(deps *ScriptDeps) tool.Tool {
	fn := func(ctx context.Context, req AdvanceTimelineReq) (AdvanceTimelineRsp, error) {
		sessionID, _, err := getSessionAndUser(ctx)
		if err != nil {
			return AdvanceTimelineRsp{}, err
		}

		if deps.ProgressTracker == nil || deps.Archive == nil {
			return AdvanceTimelineRsp{Message: "进度追踪器未初始化"}, nil
		}

		progress := deps.ProgressTracker.GetProgress(sessionID)
		if progress == nil {
			return AdvanceTimelineRsp{Message: "未找到跑团进度"}, nil
		}

		scr, err := deps.Archive.Get(progress.ScriptID)
		if err != nil {
			return AdvanceTimelineRsp{Message: "剧本不存在"}, nil
		}

		oldNodeID := progress.CurrentNodeID
		targetNodeID := req.NodeID

		// 如果未指定目标节点，自动推进到下一节点
		if targetNodeID == "" {
			nextNode, err := scr.GetNextNode(oldNodeID)
			if err != nil || nextNode == nil {
				return AdvanceTimelineRsp{
					Success:   false,
					OldNodeID: oldNodeID,
					Message:   "已经是最后一个剧情节点",
					IsLastNode: true,
				}, nil
			}
			targetNodeID = nextNode.ID
		}

		// 推进节点
		if err := deps.ProgressTracker.AdvanceNode(sessionID, targetNodeID); err != nil {
			return AdvanceTimelineRsp{
				Success: false,
				Message: fmt.Sprintf("推进失败: %v", err),
			}, nil
		}

		// 获取新节点信息
		newNode, _ := scr.GetNodeByID(targetNodeID)
		rsp := AdvanceTimelineRsp{
			Success:   true,
			OldNodeID: oldNodeID,
			NewNodeID: targetNodeID,
		}
		if newNode != nil {
			rsp.NewNodeName = newNode.Name
			rsp.NewNodeDesc = newNode.Description
		}
		rsp.IsLastNode = scr.IsLastNode(targetNodeID)
		rsp.Message = fmt.Sprintf("剧情已从 %s 推进到 %s", oldNodeID, targetNodeID)

		// 重置定时器无进展计数
		if deps.TimelineEngine != nil {
			deps.TimelineEngine.ResetIdleCount(sessionID)
		}

		return rsp, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("advance_timeline"),
		function.WithDescription(
			"推进剧情时间轴到下一节点或指定节点。"+
				"当玩家完成当前节点的关键事件或达成触发条件时调用此工具。"+
				"参数 node_id 留空则自动推进到下一节点；reason 描述推进原因。"+
				"返回: 新节点信息、是否为最后一个节点。"),
	)
}

// --- get_progress tool ---

type GetProgressReq struct{}

type GetProgressRsp struct {
	ScriptName      string             `json:"script_name"`
	CurrentNodeID   string             `json:"current_node_id"`
	CurrentNodeName string             `json:"current_node_name"`
	CompletedCount  int                `json:"completed_count"`
	TotalNodes      int                `json:"total_nodes"`
	StorySummary    string             `json:"story_summary"`
	RecentDecisions []DecisionInfo     `json:"recent_decisions"`
	Found           bool               `json:"found"`
}

type DecisionInfo struct {
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`
	Outcome   string `json:"outcome"`
}

func NewGetProgressTool(deps *ScriptDeps) tool.Tool {
	fn := func(ctx context.Context, req GetProgressReq) (GetProgressRsp, error) {
		sessionID, _, err := getSessionAndUser(ctx)
		if err != nil {
			return GetProgressRsp{}, err
		}

		if deps.ProgressTracker == nil {
			return GetProgressRsp{Found: false}, nil
		}

		progress := deps.ProgressTracker.GetProgress(sessionID)
		if progress == nil {
			return GetProgressRsp{Found: false}, nil
		}

		rsp := GetProgressRsp{
			ScriptName:      progress.ScriptName,
			CurrentNodeID:   progress.CurrentNodeID,
			CurrentNodeName: progress.CurrentNodeName,
			CompletedCount:  progress.CompletedCount(),
			StorySummary:    progress.StorySummary,
			Found:           true,
		}

		// 总节点数
		if deps.Archive != nil {
			if scr, err := deps.Archive.Get(progress.ScriptID); err == nil {
				rsp.TotalNodes = scr.TotalNodes()
			}
		}

		// 最近 5 条决策
		if len(progress.PlayerDecisions) > 0 {
			start := len(progress.PlayerDecisions) - 5
			if start < 0 {
				start = 0
			}
			for _, d := range progress.PlayerDecisions[start:] {
				rsp.RecentDecisions = append(rsp.RecentDecisions, DecisionInfo{
					Timestamp: d.Timestamp,
					Action:    d.Action,
					Outcome:   d.Outcome,
				})
			}
		}

		return rsp, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("get_progress"),
		function.WithDescription(
			"获取当前跑团进度，包括当前剧情节点、已完成节点数、剧情摘要和最近的玩家决策。"+
				"用于了解跑团当前状态和回顾关键决策。"),
	)
}

// --- save_progress tool ---

type SaveProgressReq struct {
	StorySummary    string `json:"story_summary" jsonschema:"description=AI总结的当前剧情进度（100-200字），required"`
	ChapterSummary  string `json:"chapter_summary,omitempty" jsonschema:"description=当前章节摘要（50字以内）"`
	DecisionAction  string `json:"decision_action,omitempty" jsonschema:"description=记录玩家本次的关键决策行动"`
	DecisionOutcome string `json:"decision_outcome,omitempty" jsonschema:"description=决策的结果"`
}

type SaveProgressRsp struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func NewSaveProgressTool(deps *ScriptDeps) tool.Tool {
	fn := func(ctx context.Context, req SaveProgressReq) (SaveProgressRsp, error) {
		sessionID, _, err := getSessionAndUser(ctx)
		if err != nil {
			return SaveProgressRsp{}, err
		}

		if deps.ProgressTracker == nil {
			return SaveProgressRsp{Message: "进度追踪器未初始化"}, nil
		}

		// 更新摘要
		if req.StorySummary != "" {
			if err := deps.ProgressTracker.UpdateSummary(sessionID, req.StorySummary, req.ChapterSummary); err != nil {
				return SaveProgressRsp{
					Success: false,
					Message: fmt.Sprintf("保存摘要失败: %v", err),
				}, nil
			}
		}

		// 记录决策
		if req.DecisionAction != "" {
			if err := deps.ProgressTracker.RecordDecision(sessionID, script.Decision{
				Action:  req.DecisionAction,
				Outcome: req.DecisionOutcome,
			}); err != nil {
				return SaveProgressRsp{
					Success: false,
					Message: fmt.Sprintf("记录决策失败: %v", err),
				}, nil
			}
		}

		// 重置定时器
		if deps.TimelineEngine != nil {
			deps.TimelineEngine.ResetIdleCount(sessionID)
		}

		return SaveProgressRsp{
			Success: true,
			Message: "进度已保存",
		}, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("save_progress"),
		function.WithDescription(
			"保存跑团进度，包括 AI 生成的剧情摘要和玩家关键决策记录。"+
				"建议在重要剧情转折点或每轮互动后调用。"+
				"参数 story_summary 是必填的剧情总结，chapter_summary 是章节摘要，"+
				"decision_action 和 decision_outcome 用于记录玩家关键决策。"),
	)
}

// --- get_npc tool ---

type GetNPCReq struct {
	Name string `json:"name,omitempty" jsonschema:"description=NPC角色名。留空则返回所有NPC列表"`
}

type GetNPCRsp struct {
	NPCs       []NPCInfo `json:"npcs"`
	Found      bool      `json:"found"`
}

type NPCInfo struct {
	Name        string         `json:"name"`
	Role        string         `json:"role"`
	Personality string         `json:"personality"`
	Background  string         `json:"background"`
	Attrs       map[string]int `json:"attrs"`
	Skills      map[string]int `json:"skills"`
	Notes       string         `json:"notes"`
}

func NewGetNPCTool(deps *ScriptDeps) tool.Tool {
	fn := func(ctx context.Context, req GetNPCReq) (GetNPCRsp, error) {
		sessionID, _, err := getSessionAndUser(ctx)
		if err != nil {
			return GetNPCRsp{}, err
		}

		if deps.ProgressTracker == nil || deps.Archive == nil {
			return GetNPCRsp{Found: false}, nil
		}

		progress := deps.ProgressTracker.GetProgress(sessionID)
		if progress == nil {
			return GetNPCRsp{Found: false}, nil
		}

		scr, err := deps.Archive.Get(progress.ScriptID)
		if err != nil {
			return GetNPCRsp{Found: false}, nil
		}

		// 如果指定了名称，返回单个 NPC
		if req.Name != "" {
			char, err := scr.GetCharacterByName(req.Name)
			if err != nil {
				return GetNPCRsp{Found: false}, nil
			}
			return GetNPCRsp{
				NPCs:  []NPCInfo{npcToInfo(char)},
				Found: true,
			}, nil
		}

		// 返回所有 NPC
		rsp := GetNPCRsp{Found: true}
		for i := range scr.Characters {
			rsp.NPCs = append(rsp.NPCs, npcToInfo(&scr.Characters[i]))
		}
		return rsp, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("get_npc"),
		function.WithDescription(
			"获取剧本中的 NPC 角色信息，包括性格、背景、属性和技能。"+
				"参数 name 指定 NPC 名称获取详细信息；留空则返回所有 NPC 列表。"+
				"用于扮演 NPC 时参考其性格和属性。"),
	)
}

func npcToInfo(char *script.ScriptCharacter) NPCInfo {
	return NPCInfo{
		Name:        char.Name,
		Role:        char.Role,
		Personality: char.Personality,
		Background:  char.Background,
		Attrs:       char.Attrs,
		Skills:      char.Skills,
		Notes:       char.Notes,
	}
}
