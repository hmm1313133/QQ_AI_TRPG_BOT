// Package ruleset defines the base interface and common types for TRPG rulesets.
// Each specific ruleset (CoC7, DnD5e) implements the RuleSet interface and
// adds rule-specific methods that can be accessed via type assertion.
package ruleset

import (
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/dice"
)

// RuleSet is the base interface for all TRPG rulesets.
// Specific rulesets (CoC7, DnD5e) implement this and add their own methods.
type RuleSet interface {
	// Name returns the ruleset identifier, e.g. "coc7" or "dnd5e".
	Name() string
	// Roll evaluates a generic dice expression.
	Roll(expr string) (*dice.RollResult, error)
}

// CheckResult is the common result type for skill/ability checks.
type CheckResult struct {
	Skill      string `json:"skill"`                // skill or ability name
	Value      int    `json:"value"`                // skill value or modifier
	Roll       int    `json:"roll"`                 // raw dice result
	Total      int    `json:"total"`                // final value (roll + modifier, if applicable)
	Success    bool   `json:"success"`              // whether the check succeeded
	Level      string `json:"level"`                // success level: "大成功"/"困难成功"/"大失败" etc.
	Detail     string `json:"detail"`               // readable description
	BonusRolls []int  `json:"bonus_rolls,omitempty"` // extra dice rolls (bonus/penalty/advantage)
}

// CheckOptions contains options that modify a check.
type CheckOptions struct {
	BonusDice    int  // CoC bonus dice count
	PenaltyDice  int  // CoC penalty dice count
	Advantage    bool // DnD advantage (roll 2d20, keep higher)
	Disadvantage bool // DnD disadvantage (roll 2d20, keep lower)
}

// String returns a readable summary of a CheckResult.
func (r *CheckResult) String() string {
	if r.Detail != "" {
		return r.Detail
	}
	return r.Level
}
