// Package coc7 implements the Call of Cthulhu 7th Edition ruleset.
// It provides skill checks (with bonus/penalty dice), SAN checks, skill growth,
// attribute generation, opposed checks, and madness symptom tables.
package coc7

import (
	"fmt"
	"math/rand"
	"strconv"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/dice"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset"
)

// CoC7 implements the Call of Cthulhu 7th Edition ruleset.
type CoC7 struct {
	houseRule HouseRule
	r         *rand.Rand
}

// HouseRule defines the thresholds for critical success and fumble.
type HouseRule struct {
	Name           string
	CritMax        int  // rolls 1..CritMax are critical success (default: 1)
	FumbleWhen50   int  // fumble threshold when skill >= 50 (default: 100)
	FumbleWhenLow  int  // fumble threshold when skill < 50 (default: 96)
	CritMustSucceed bool // if true, crit must also be <= skill
}

// Preset house rules matching sealdice's .setcoc options.
var PresetHouseRules = []HouseRule{
	{Name: "0 标准规则", CritMax: 1, FumbleWhen50: 100, FumbleWhenLow: 96},
	{Name: "1 规则一", CritMax: 5, FumbleWhen50: 100, FumbleWhenLow: 96},
	{Name: "2 规则二", CritMax: 5, FumbleWhen50: 96, FumbleWhenLow: 96},
	{Name: "3 规则三", CritMax: 5, FumbleWhen50: 100, FumbleWhenLow: 100},
	{Name: "4 规则四", CritMax: 1, FumbleWhen50: 100, FumbleWhenLow: 100},
}

// SANCheckResult is the result of a SAN check.
type SANCheckResult struct {
	Roll       int    `json:"roll"`
	SANValue   int    `json:"san_value"`
	Success    bool   `json:"success"`
	Level      string `json:"level"`
	LossAmount int    `json:"loss_amount"`
	LossExpr   string `json:"loss_expr"`
	NewSAN     int    `json:"new_san"`
	Detail     string `json:"detail"`
}

// GrowthResult is the result of a skill growth roll (.en).
type GrowthResult struct {
	Skill    string `json:"skill"`
	OldValue int    `json:"old_value"`
	Roll     int    `json:"roll"`
	Success  bool   `json:"success"` // true if skill increased
	Increase int    `json:"increase"`
	NewValue int    `json:"new_value"`
	Detail   string `json:"detail"`
}

// OpposedResult is the result of an opposed check (.rav).
type OpposedResult struct {
	SelfRoll  int    `json:"self_roll"`
	SelfLevel string `json:"self_level"`
	OppRoll   int    `json:"opp_roll"`
	OppLevel  string `json:"opp_level"`
	Winner    string `json:"winner"` // "self" or "opponent"
	Detail    string `json:"detail"`
}

// New creates a CoC7 ruleset with the standard house rule.
func New() *CoC7 {
	return &CoC7{
		houseRule: PresetHouseRules[0],
	}
}

// NewWithRand creates a CoC7 ruleset with a custom random source (for testing).
func NewWithRand(r *rand.Rand) *CoC7 {
	c := New()
	c.r = r
	return c
}

// Name returns "coc7".
func (c *CoC7) Name() string { return "coc7" }

// Roll evaluates a generic dice expression.
func (c *CoC7) Roll(expr string) (*dice.RollResult, error) {
	return dice.Roll(expr)
}

// SetHouseRule changes the active house rule.
func (c *CoC7) SetHouseRule(hr HouseRule) { c.houseRule = hr }

// HouseRule returns the current house rule.
func (c *CoC7) HouseRule() HouseRule { return c.houseRule }

// HouseRules returns all preset house rules.
func (c *CoC7) HouseRules() []HouseRule { return PresetHouseRules }

// SetHouseRuleByIndex sets the house rule by preset index (0-4).
func (c *CoC7) SetHouseRuleByIndex(idx int) error {
	if idx < 0 || idx >= len(PresetHouseRules) {
		return fmt.Errorf("房规编号无效，可选 0-%d", len(PresetHouseRules)-1)
	}
	c.houseRule = PresetHouseRules[idx]
	return nil
}

// Check performs a CoC 7e skill check: roll 1d100 against the skill value.
// Bonus/penalty dice are applied if specified in opts.
func (c *CoC7) Check(skill string, value int, opts ruleset.CheckOptions) (*ruleset.CheckResult, error) {
	roll, tensRolls, unitsRoll := c.rollD100(opts.BonusDice, opts.PenaltyDice)
	success, level := c.evalSuccess(roll, value)

	detail := fmt.Sprintf("1d100=%d", roll)
	if len(tensRolls) > 1 {
		detail = fmt.Sprintf("1d100=%d (十位: %v, 个位: %d)", roll, tensRolls, unitsRoll)
	}
	detail += fmt.Sprintf(" ≤ %d → %s", value, level)

	return &ruleset.CheckResult{
		Skill:      skill,
		Value:      value,
		Roll:       roll,
		Success:    success,
		Level:      level,
		Detail:     detail,
		BonusRolls: tensRolls,
	}, nil
}

// SANCheck performs a SAN check: roll 1d100 against current SAN.
// successLoss and failLoss can be a number or dice expression (e.g. "1d4").
func (c *CoC7) SANCheck(currentSAN int, successLoss, failLoss string) (*SANCheckResult, error) {
	roll, _, _ := c.rollD100(0, 0)
	success, level := c.evalSuccess(roll, currentSAN)

	lossExpr := failLoss
	if success {
		lossExpr = successLoss
	}

	lossAmount, err := c.evalLoss(lossExpr)
	if err != nil {
		return nil, err
	}

	newSAN := currentSAN - lossAmount
	if newSAN < 0 {
		newSAN = 0
	}

	detail := fmt.Sprintf("SAN检定: 1d100=%d vs SAN=%d → %s，损失 %s=%d 点 (SAN: %d→%d)",
		roll, currentSAN, level, lossExpr, lossAmount, currentSAN, newSAN)

	return &SANCheckResult{
		Roll:       roll,
		SANValue:   currentSAN,
		Success:    success,
		Level:      level,
		LossAmount: lossAmount,
		LossExpr:   lossExpr,
		NewSAN:     newSAN,
		Detail:     detail,
	}, nil
}

// SkillGrowth performs a skill growth roll (.en): roll 1d100 against skill value.
// If roll > value, the skill increases by 1d10.
func (c *CoC7) SkillGrowth(skill string, value int) (*GrowthResult, error) {
	roll, _, _ := c.rollD100(0, 0)
	growth := roll > value
	increase := 0
	newValue := value

	detail := fmt.Sprintf("技能成长: %s(%d) 1d100=%d → 未成长", skill, value, roll)
	if growth {
		incRoll, err := dice.Roll("1d10")
		if err != nil {
			return nil, err
		}
		increase = incRoll.Total
		newValue = value + increase
		detail = fmt.Sprintf("技能成长: %s(%d) 1d100=%d → 成功! 1d10=%d，%d→%d",
			skill, value, roll, increase, value, newValue)
	}

	return &GrowthResult{
		Skill:    skill,
		OldValue: value,
		Roll:     roll,
		Success:  growth,
		Increase: increase,
		NewValue: newValue,
		Detail:   detail,
	}, nil
}

// GenerateAttrs generates a complete set of CoC 7e attributes.
func (c *CoC7) GenerateAttrs() (map[string]int, error) {
	rollExpr := func(expr string) int {
		r, err := dice.Roll(expr)
		if err != nil {
			return 0
		}
		return r.Total
	}

	attrs := map[string]int{
		"力量": rollExpr("3d6*5"),
		"体质": rollExpr("3d6*5"),
		"体型": rollExpr("(2d6+6)*5"),
		"敏捷": rollExpr("3d6*5"),
		"外貌": rollExpr("3d6*5"),
		"智力": rollExpr("(2d6+6)*5"),
		"意志": rollExpr("3d6*5"),
		"教育": rollExpr("(2d6+6)*5"),
		"幸运": rollExpr("3d6*5"),
	}
	attrs["SAN"] = attrs["意志"]
	attrs["HP"] = (attrs["体质"] + attrs["体型"]) / 10
	attrs["MP"] = attrs["意志"] / 5

	return attrs, nil
}

// OpposedCheck performs an opposed check (.rav): both sides roll 1d100.
// Higher success level wins; if tied, higher roll wins (among successes)
// or lower roll wins (among failures).
func (c *CoC7) OpposedCheck(selfVal, oppVal int) (*OpposedResult, error) {
	selfRoll, _, _ := c.rollD100(0, 0)
	oppRoll, _, _ := c.rollD100(0, 0)

	_, selfLevel := c.evalSuccess(selfRoll, selfVal)
	_, oppLevel := c.evalSuccess(oppRoll, oppVal)

	selfLevelVal := successLevelValue(selfLevel)
	oppLevelVal := successLevelValue(oppLevel)

	winner := "opponent"
	switch {
	case selfLevelVal > oppLevelVal:
		winner = "self"
	case selfLevelVal < oppLevelVal:
		winner = "opponent"
	default:
		// Same success level
		if selfLevelVal >= 2 { // both succeeded
			if selfRoll >= oppRoll {
				winner = "self"
			}
		} else { // both failed
			if selfRoll <= oppRoll {
				winner = "self"
			}
		}
	}

	winnerName := "对方"
	if winner == "self" {
		winnerName = "己方"
	}
	detail := fmt.Sprintf("对抗检定: 己方 1d100=%d/%d(%s) vs 对方 1d100=%d/%d(%s) → %s获胜",
		selfRoll, selfVal, selfLevel, oppRoll, oppVal, oppLevel, winnerName)

	return &OpposedResult{
		SelfRoll:  selfRoll,
		SelfLevel: selfLevel,
		OppRoll:   oppRoll,
		OppLevel:  oppLevel,
		Winner:    winner,
		Detail:    detail,
	}, nil
}

// RandomMadness returns a random madness symptom.
// If temporary is true, returns a bout of madness (.ti); otherwise underlying insanity (.li).
func (c *CoC7) RandomMadness(temporary bool) string {
	roll := c.randInt(10) // 0-9
	if temporary {
		return TemporaryMadness[roll]
	}
	return UnderlyingMadness[roll]
}

// --- internal helpers ---

// rollD100 rolls 1d100 with optional bonus/penalty dice.
// Returns the final roll, all tens dice results, and the units die result.
func (c *CoC7) rollD100(bonusDice, penaltyDice int) (roll int, tensRolls []int, units int) {
	units = c.randInt(10) // 0-9

	net := bonusDice - penaltyDice
	tensCount := 1
	keepLowest := true // bonus dice → keep lowest tens

	if net > 0 {
		tensCount = 1 + net
		keepLowest = true
	} else if net < 0 {
		tensCount = 1 - net
		keepLowest = false // penalty dice → keep highest tens
	}

	tensRolls = make([]int, tensCount)
	for i := 0; i < tensCount; i++ {
		tensRolls[i] = c.randInt(10) // 0-9
	}

	selectedTens := tensRolls[0]
	for _, t := range tensRolls[1:] {
		if keepLowest {
			if t < selectedTens {
				selectedTens = t
			}
		} else {
			if t > selectedTens {
				selectedTens = t
			}
		}
	}

	roll = selectedTens*10 + units
	if roll == 0 {
		roll = 100 // 00 + 0 = 100
	}
	return roll, tensRolls, units
}

// evalSuccess determines success level based on roll vs skill value.
func (c *CoC7) evalSuccess(roll, skill int) (bool, string) {
	hr := c.houseRule

	// Fumble check
	if roll >= 100 {
		return false, "大失败"
	}
	if skill < 50 && roll >= hr.FumbleWhenLow {
		return false, "大失败"
	}

	// Critical success check
	if roll <= hr.CritMax {
		if !hr.CritMustSucceed || roll <= skill {
			return true, "大成功"
		}
	}

	// Regular success levels (checked from hardest to easiest)
	if roll <= skill/5 {
		return true, "极难成功"
	}
	if roll <= skill/2 {
		return true, "困难成功"
	}
	if roll <= skill {
		return true, "普通成功"
	}

	return false, "失败"
}

// evalLoss parses a loss expression (number or dice expression) and returns the amount.
func (c *CoC7) evalLoss(expr string) (int, error) {
	if n, err := strconv.Atoi(expr); err == nil {
		return n, nil
	}
	result, err := dice.Roll(expr)
	if err != nil {
		return 0, fmt.Errorf("无效的损失表达式: %s", expr)
	}
	return result.Total, nil
}

// randInt returns a random int in [0, n).
func (c *CoC7) randInt(n int) int {
	if c.r != nil {
		return c.r.Intn(n)
	}
	return rand.Intn(n)
}

// successLevelValue converts a success level string to a numeric value for comparison.
func successLevelValue(level string) int {
	switch level {
	case "大成功":
		return 5
	case "极难成功":
		return 4
	case "困难成功":
		return 3
	case "普通成功":
		return 2
	case "失败":
		return 1
	case "大失败":
		return 0
	default:
		return 0
	}
}
