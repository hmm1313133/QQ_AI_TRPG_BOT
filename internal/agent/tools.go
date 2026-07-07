// Package agent — AI Agent FunctionTools for TRPG operations.
// Tools are called by the LLM during KP/DM interactions.
// Each tool receives the sessionID and userID via context to access
// the correct game session and character card.
//
// Only KP-relevant operations are exposed as AI tools:
//   - roll_dice     : roll dice for NPCs, random events, etc.
//   - skill_check   : call for and resolve player skill checks
//   - san_check     : trigger SAN checks (CoC only)
//   - get_character : look up player stats to gauge difficulty
//   - set_ruleset   : switch ruleset (rare, but useful mid-session)
//
// All game logic is delegated to trpg.Service, which is shared with
// command Handlers to ensure single-source-of-truth.
package agent

import (
	"context"
	"fmt"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// NewKPTools creates all FunctionTools for the KP AI agent.
// Returns a slice suitable for passing to llmagent.WithTools().
func NewKPTools(sessionMgr *core.SessionManager, svc *trpg.Service) []tool.Tool {
	return []tool.Tool{
		NewRollDiceTool(sessionMgr, svc),
		NewSkillCheckTool(sessionMgr, svc),
		NewSANCheckTool(sessionMgr, svc),
		NewGetCharacterTool(sessionMgr, svc),
		NewSetRulesetTool(sessionMgr, svc),
	}
}

// --- roll_dice tool ---

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
func NewRollDiceTool(sessionMgr *core.SessionManager, svc *trpg.Service) tool.Tool {
	fn := func(ctx context.Context, req RollDiceReq) (RollDiceRsp, error) {
		result, err := svc.RollDice("", req.Expression)
		if err != nil {
			return RollDiceRsp{}, fmt.Errorf("骰子表达式无效: %w", err)
		}

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
				"支持复杂表达式: 1d100, 3d6, 1d20+5, 4d6kh3(保留最高3), 2d6!(爆炸骰), (1d6+3)*2。"+
				"参数: expression 是骰子表达式，reason 是投骰原因。"+
				"返回: 每个骰子的结果、总计和可读描述。"),
	)
}

// --- skill_check tool ---

type SkillCheckReq struct {
	Skill        string `json:"skill" jsonschema:"description=技能或属性名称，如 侦查、力量、运动"`
	Value        int    `json:"value" jsonschema:"description=技能值或调整值。为0时自动从角色卡读取"`
	BonusDice    int    `json:"bonus_dice,omitempty" jsonschema:"description=CoC奖励骰数量"`
	PenaltyDice  int    `json:"penalty_dice,omitempty" jsonschema:"description=CoC惩罚骰数量"`
	Advantage    bool   `json:"advantage,omitempty" jsonschema:"description=DnD优势掷骰"`
	Disadvantage bool   `json:"disadvantage,omitempty" jsonschema:"description=DnD劣势掷骰"`
}

type SkillCheckRsp struct {
	Skill   string `json:"skill"`
	Roll    int    `json:"roll"`
	Total   int    `json:"total"`
	Success bool   `json:"success"`
	Level   string `json:"level"`
	Detail  string `json:"detail"`
}

// NewSkillCheckTool creates a skill check FunctionTool.
// Automatically detects CoC7 or DnD5e based on the active ruleset.
func NewSkillCheckTool(sessionMgr *core.SessionManager, svc *trpg.Service) tool.Tool {
	fn := func(ctx context.Context, req SkillCheckReq) (SkillCheckRsp, error) {
		sessionID, userID, err := getSessionAndUser(ctx)
		if err != nil {
			return SkillCheckRsp{}, err
		}

		result, err := svc.SkillCheck(sessionID, userID, req.Skill, req.Value, ruleset.CheckOptions{
			BonusDice:    req.BonusDice,
			PenaltyDice:  req.PenaltyDice,
			Advantage:    req.Advantage,
			Disadvantage: req.Disadvantage,
		})
		if err != nil {
			return SkillCheckRsp{}, err
		}

		return SkillCheckRsp{
			Skill: result.Skill, Roll: result.Roll, Total: result.Total,
			Success: result.Success, Level: result.Level, Detail: result.Detail,
		}, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("skill_check"),
		function.WithDescription(
			"进行技能检定。根据当前规则集自动选择 CoC7(1d100) 或 DnD5e(1d20) 检定方式。"+
				"CoC: 提供 skill 和 value(或自动从角色卡读取)，可附加 bonus_dice/penalty_dice。"+
				"DnD: 提供 value(调整值)或 skill(自动从角色卡读取)，可附加 advantage/disadvantage。"+
				"返回: 骰点结果、是否成功、成功等级。"),
	)
}

// --- san_check tool (CoC only) ---

type SANCheckReq struct {
	SuccessLoss string `json:"success_loss" jsonschema:"description=成功时损失SAN值，如 0 或 1，required"`
	FailLoss    string `json:"fail_loss" jsonschema:"description=失败时损失SAN值，如 1 或 1d4，required"`
}

type SANCheckRsp struct {
	Roll       int    `json:"roll"`
	SANValue   int    `json:"san_value"`
	Success    bool   `json:"success"`
	Level      string `json:"level"`
	LossAmount int    `json:"loss_amount"`
	NewSAN     int    `json:"new_san"`
	Detail     string `json:"detail"`
}

// NewSANCheckTool creates a SAN check FunctionTool (CoC7 only).
func NewSANCheckTool(sessionMgr *core.SessionManager, svc *trpg.Service) tool.Tool {
	fn := func(ctx context.Context, req SANCheckReq) (SANCheckRsp, error) {
		sessionID, userID, err := getSessionAndUser(ctx)
		if err != nil {
			return SANCheckRsp{}, err
		}

		result, err := svc.SANCheck(sessionID, userID, req.SuccessLoss, req.FailLoss)
		if err != nil {
			return SANCheckRsp{}, err
		}

		return SANCheckRsp{
			Roll: result.Roll, SANValue: result.SANValue, Success: result.Success,
			Level: result.Level, LossAmount: result.LossAmount, NewSAN: result.NewSAN,
			Detail: result.Detail,
		}, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("san_check"),
		function.WithDescription(
			"进行 SAN 检定（仅 CoC7）。根据角色卡当前 SAN 值投 1d100。"+
				"成功损失 success_loss，失败损失 fail_loss（可为数字或骰子表达式如 1d4）。"+
				"自动更新角色卡 SAN 值。返回: 检定结果和新的 SAN 值。"),
	)
}

// --- get_character tool ---

type GetCharacterReq struct {
	PlayerID string `json:"player_id,omitempty" jsonschema:"description=玩家ID，留空则使用当前玩家"`
}

type GetCharacterRsp struct {
	Name   string         `json:"name"`
	System string         `json:"system"`
	Attrs  map[string]int `json:"attrs"`
	Skills map[string]int `json:"skills"`
	Status map[string]int `json:"status"`
	Found  bool           `json:"found"`
}

// NewGetCharacterTool creates a character info query FunctionTool.
func NewGetCharacterTool(sessionMgr *core.SessionManager, svc *trpg.Service) tool.Tool {
	fn := func(ctx context.Context, req GetCharacterReq) (GetCharacterRsp, error) {
		sessionID, userID, err := getSessionAndUser(ctx)
		if err != nil {
			return GetCharacterRsp{}, err
		}

		targetUser := userID
		if req.PlayerID != "" {
			targetUser = req.PlayerID
		}

		card := svc.GetActiveCharacter(sessionID, targetUser)
		if card == nil {
			return GetCharacterRsp{Found: false}, nil
		}

		return GetCharacterRsp{
			Name:   card.Name,
			System: card.System,
			Attrs:  card.Attrs,
			Skills: card.Skills,
			Status: card.Status,
			Found:  true,
		}, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("get_character"),
		function.WithDescription(
			"获取玩家的角色卡信息，包括属性、技能和状态值(HP/SAN/MP)。"+
				"参数 player_id 留空则获取当前玩家的角色卡。"+
				"返回: 角色名、规则系统、属性、技能、状态。"),
	)
}

// --- set_ruleset tool ---

type SetRulesetReq struct {
	Ruleset string `json:"ruleset" jsonschema:"description=规则集名称: coc7 或 dnd5e，required"`
}

type SetRulesetRsp struct {
	Ruleset string `json:"ruleset"`
	Success bool   `json:"success"`
}

// NewSetRulesetTool creates a ruleset switching FunctionTool.
func NewSetRulesetTool(sessionMgr *core.SessionManager, svc *trpg.Service) tool.Tool {
	fn := func(ctx context.Context, req SetRulesetReq) (SetRulesetRsp, error) {
		sessionID, _, err := getSessionAndUser(ctx)
		if err != nil {
			return SetRulesetRsp{}, err
		}

		if err := svc.SetRuleSet(sessionID, req.Ruleset); err != nil {
			return SetRulesetRsp{}, err
		}

		return SetRulesetRsp{Ruleset: req.Ruleset, Success: true}, nil
	}

	return function.NewFunctionTool(fn,
		function.WithName("set_ruleset"),
		function.WithDescription(
			"切换当前会话的 TRPG 规则集。参数: coc7 (克苏鲁的呼唤第7版) 或 dnd5e (龙与地下城第5版)。"),
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

// getSessionAndUser extracts sessionID and userID from context.
func getSessionAndUser(ctx context.Context) (sessionID, userID string, err error) {
	sessionID, ok := ctx.Value(sessionIDKey{}).(string)
	if !ok || sessionID == "" {
		return "", "", fmt.Errorf("无法获取会话ID")
	}
	userID, _ = ctx.Value(userIDKey{}).(string)
	return sessionID, userID, nil
}
