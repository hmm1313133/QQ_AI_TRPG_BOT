package agent

import (
	"context"
	"fmt"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/dice"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// RollDiceReq 是骰子工具的请求参数。
type RollDiceReq struct {
	Expression string `json:"expression" jsonschema:"description=骰子表达式，例如 1d100、3d6、1d20+5，required"`
	Reason     string `json:"reason" jsonschema:"description=投骰原因或检定技能名称"`
}

// RollDiceRsp 是骰子工具的响应。
type RollDiceRsp struct {
	Expression string `json:"expression"`
	Rolls      []int  `json:"rolls"`
	Total      int    `json:"total"`
	Detail     string `json:"detail"`
	Reason     string `json:"reason,omitempty"`
}

// NewRollDiceTool 创建骰子投掷 FunctionTool。
// 该工具可被 AI Agent 调用，用于在跑团过程中进行骰点判定。
// 投骰结果会通过 sessionCallback 回写到 Session，供 Handler 层读取。
//
// sessionCallback 用于联动：AI Agent 投骰后，结果写入 Session.Data["last_dice_result"]，
// 这样功能层的 LogHandler 等可以感知到骰子结果。
func NewRollDiceTool(sessionMgr *core.SessionManager) tool.Tool {
	fn := func(ctx context.Context, req RollDiceReq) (RollDiceRsp, error) {
		result, err := dice.Roll(req.Expression)
		if err != nil {
			return RollDiceRsp{}, fmt.Errorf("骰子表达式无效: %w", err)
		}

		// 尝试从 context 中提取 sessionID（由 KPAgent.Chat 注入）
		if sessionID, ok := ctx.Value(sessionIDKey{}).(string); ok {
			if sessionID != "" && sessionMgr != nil {
				session := sessionMgr.GetSession(sessionID)
				session.Set("last_dice_result", result.String())
				session.Set("last_dice_total", result.Total)
			}
		}

		return RollDiceRsp{
			Expression: result.Expr,
			Rolls:      result.Rolls,
			Total:      result.Total,
			Detail:     result.String(),
			Reason:     req.Reason,
		}, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("roll_dice"),
		function.WithDescription(
			"投掷骰子。用于 TRPG 游戏中的技能检定、属性判定等场景。"+
				"参数: expression 是骰子表达式（如 1d100, 3d6, 1d20+5），reason 是投骰原因。"+
				"返回: 每个骰子的结果、总计和可读描述。"),
	)
}

// sessionIDKey 是 context 中传递 sessionID 的 key 类型。
type sessionIDKey struct{}

// withSessionID 返回一个携带 sessionID 的 context。
func withSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}
