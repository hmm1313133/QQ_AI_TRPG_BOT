// Package dice 提供骰子解析与投掷功能。
package dice

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
)

var dicePattern = regexp.MustCompile(`(?i)(\d*)d(\d+)([+-]\d+)?`)

// Roll 解析并执行骰子表达式，如 "3d6", "1d100+5", "d20"。
func Roll(expr string) (*RollResult, error) {
	matches := dicePattern.FindStringSubmatch(expr)
	if matches == nil {
		return nil, fmt.Errorf("无效的骰子表达式: %s", expr)
	}

	count := 1
	if matches[1] != "" {
		var err error
		count, err = strconv.Atoi(matches[1])
		if err != nil || count < 1 || count > 1000 {
			return nil, fmt.Errorf("骰子数量无效: %s", matches[1])
		}
	}

	sides, err := strconv.Atoi(matches[2])
	if err != nil || sides < 1 || sides > 10000 {
		return nil, fmt.Errorf("骰子面数无效: %s", matches[2])
	}

	rolls := make([]int, count)
	total := 0
	for i := 0; i < count; i++ {
		rolls[i] = rand.Intn(sides) + 1
		total += rolls[i]
	}

	detail := fmt.Sprintf("%v", rolls)

	// 修正值
	if matches[3] != "" {
		mod, err := strconv.Atoi(matches[3])
		if err != nil {
			return nil, fmt.Errorf("修正值无效: %s", matches[3])
		}
		total += mod
		detail = fmt.Sprintf("%s%+d", detail, mod)
	}

	return &RollResult{
		Expr:   expr,
		Rolls:  rolls,
		Total:  total,
		Detail: detail,
	}, nil
}

// RollResult 骰子投掷结果。
type RollResult struct {
	Expr   string `json:"expr"`
	Rolls  []int  `json:"rolls"`
	Total  int    `json:"total"`
	Detail string `json:"detail"`
}

// String 返回可读的投掷结果字符串。
func (r *RollResult) String() string {
	return fmt.Sprintf("%s = %s = %d", r.Expr, r.Detail, r.Total)
}
