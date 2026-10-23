// Package agent — AI Agent 的骰子 FunctionTool。
//
// roll_dice 是唯一保留的 AI 工具：供叙事层在需要随机判定时调用，
// 直接基于 internal/dice 求值，结果记账到会话（last_dice_result）。
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/dice"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

type RollDiceReq struct {
	Expression string `json:"expression" jsonschema:"description=骰子表达式，例如 1d100、3d6、1d20+5、4d6kh3，required"`
	Reason     string `json:"reason" jsonschema:"description=投骰原因或检定技能名称"`
}

type RollDiceRsp struct {
	Expression string `json:"expression"`
	Rolls      []int  `json:"rolls"`
	Total      int    `json:"total"`
	Detail     string `json:"detail"`
	Reason     string `json:"reason,omitempty"`
}

// NewRollDiceTool creates the dice rolling FunctionTool.
func NewRollDiceTool(sessionMgr *core.SessionManager) tool.Tool {
	fn := func(ctx context.Context, req RollDiceReq) (RollDiceRsp, error) {
		result, err := dice.Roll(req.Expression)
		if err != nil {
			return RollDiceRsp{}, fmt.Errorf("骰子表达式无效: %w", err)
		}

		if sessionID, ok := ctx.Value(sessionIDKey{}).(string); ok {
			if sessionID != "" && sessionMgr != nil {
				session := sessionMgr.GetSession(sessionID)
				session.Set("last_dice_result", result.String())
				session.Set("last_dice_total", result.Total)
				session.Set("last_dice_at", time.Now().Unix())
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
			"投掷骰子。用于游戏中的随机判定场景。"+
				"支持复杂表达式: 1d100, 3d6, 1d20+5, 4d6kh3(保留最高3), 2d6!(爆炸骰), (1d6+3)*2。"+
				"参数: expression 是骰子表达式，reason 是投骰原因。"+
				"返回: 每个骰子的结果、总计和可读描述。"),
	)
}

// --- helpers ---

// sessionIDKey and userIDKey are context key types for passing session/user IDs.
type sessionIDKey struct{}
type userIDKey struct{}

// withSessionID returns a context carrying the sessionID.
func withSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

// withUserID returns a context carrying the userID.
func withUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}
