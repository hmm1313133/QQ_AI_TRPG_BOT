// Package dnd5e implements the Dungeons & Dragons 5th Edition ruleset.
// It provides d20 ability checks (with advantage/disadvantage),
// attribute generation (4d6kh3), death saves, and initiative.
package dnd5e

import (
	"fmt"
	"math/rand"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/dice"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset"
)

// DnD5e implements the D&D 5th Edition ruleset.
type DnD5e struct {
	r *rand.Rand
}

// DeathSaveResult is the result of a death saving throw (.ds).
type DeathSaveResult struct {
	Roll       int    `json:"roll"`
	Success    bool   `json:"success"`    // true if roll >= 10
	Critical   bool   `json:"critical"`   // natural 20 → revive
	Fumble     bool   `json:"fumble"`     // natural 1 → 2 failures
	Failures   int    `json:"failures"`   // total failures after this roll
	Successes  int    `json:"successes"`  // total successes after this roll
	Stabilized bool   `json:"stabilized"` // 3 successes → stable
	Dead       bool   `json:"dead"`       // 3 failures → dead
	Revived    bool   `json:"revived"`    // natural 20 → revived with 1 HP
	Detail     string `json:"detail"`
}

// New creates a DnD5e ruleset.
func New() *DnD5e {
	return &DnD5e{}
}

// NewWithRand creates a DnD5e ruleset with a custom random source (for testing).
func NewWithRand(r *rand.Rand) *DnD5e {
	d := New()
	d.r = r
	return d
}

// Name returns "dnd5e".
func (d *DnD5e) Name() string { return "dnd5e" }

// Roll evaluates a generic dice expression.
func (d *DnD5e) Roll(expr string) (*dice.RollResult, error) {
	return dice.Roll(expr)
}

// Check performs a DnD 5e d20 check: roll 1d20 + modifier.
// With advantage, roll 2d20 and keep the higher.
// With disadvantage, roll 2d20 and keep the lower.
func (d *DnD5e) Check(modifier int, opts ruleset.CheckOptions) (*ruleset.CheckResult, error) {
	roll1 := d.rollD20()
	roll2 := 0
	finalRoll := roll1

	diceDesc := fmt.Sprintf("d20=%d", roll1)

	if opts.Advantage {
		roll2 = d.rollD20()
		finalRoll = max(roll1, roll2)
		diceDesc = fmt.Sprintf("d20=[%d %d]优势→%d", roll1, roll2, finalRoll)
	} else if opts.Disadvantage {
		roll2 = d.rollD20()
		finalRoll = min(roll1, roll2)
		diceDesc = fmt.Sprintf("d20=[%d %d]劣势→%d", roll1, roll2, finalRoll)
	}

	total := finalRoll + modifier
	modStr := ""
	if modifier >= 0 {
		modStr = fmt.Sprintf("+%d", modifier)
	} else {
		modStr = fmt.Sprintf("%d", modifier)
	}

	level := "普通"
	critical := finalRoll == 20
	fumble := finalRoll == 1

	if critical {
		level = "大成功(自然20)"
	} else if fumble {
		level = "大失败(自然1)"
	}

	detail := fmt.Sprintf("%s%s=%d → %s", diceDesc, modStr, total, level)

	return &ruleset.CheckResult{
		Skill:      "",
		Value:      modifier,
		Roll:       finalRoll,
		Total:      total,
		Success:    !fumble,
		Level:      level,
		Detail:     detail,
		BonusRolls: []int{roll1, roll2},
	}, nil
}

// GenerateAttrs generates 6 ability scores using 4d6kh3 (roll 4d6, drop lowest).
// Returns scores in order: 力量, 敏捷, 体质, 智力, 感知, 魅力.
func (d *DnD5e) GenerateAttrs() (map[string]int, error) {
	attrNames := []string{"力量", "敏捷", "体质", "智力", "感知", "魅力"}
	attrs := make(map[string]int, 6)
	for _, name := range attrNames {
		result, err := dice.Roll("4d6kh3")
		if err != nil {
			return nil, err
		}
		attrs[name] = result.Total
	}
	return attrs, nil
}

// DeathSave performs a death saving throw (.ds).
// fails and successes are the current counts before this roll.
func (d *DnD5e) DeathSave(fails, successes int) (*DeathSaveResult, error) {
	roll := d.rollD20()
	result := &DeathSaveResult{
		Roll:      roll,
		Failures:  fails,
		Successes: successes,
	}

	detail := fmt.Sprintf("死亡豁免: d20=%d", roll)

	if roll == 20 {
		result.Critical = true
		result.Revived = true
		result.Success = true
		detail += " → 自然20! 角色恢复意识，HP 恢复为 1"
	} else if roll == 1 {
		result.Fumble = true
		result.Failures = fails + 2
		detail += fmt.Sprintf(" → 自然1，失败+2 (失败 %d/3)", result.Failures)
	} else if roll >= 10 {
		result.Success = true
		result.Successes = successes + 1
		detail += fmt.Sprintf(" → 成功 (成功 %d/3)", result.Successes)
	} else {
		result.Failures = fails + 1
		detail += fmt.Sprintf(" → 失败 (失败 %d/3)", result.Failures)
	}

	if result.Successes >= 3 {
		result.Stabilized = true
		detail += " → 3次成功! 角色稳定，无需再进行豁免"
	}
	if result.Failures >= 3 {
		result.Dead = true
		detail += " → 3次失败! 角色死亡"
	}

	result.Detail = detail
	return result, nil
}

// Initiative rolls initiative: 1d20 + modifier.
func (d *DnD5e) Initiative(modifier int) (int, error) {
	result, err := dice.Roll(fmt.Sprintf("1d20+%d", modifier))
	if err != nil {
		return 0, err
	}
	return result.Total, nil
}

// --- internal helpers ---

func (d *DnD5e) rollD20() int {
	if d.r != nil {
		return d.r.Intn(20) + 1
	}
	return rand.Intn(20) + 1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
