// Package dice provides dice expression parsing and rolling.
// It uses a recursive descent parser to support complex expressions like
// "1d6+2d8", "4d6kh3", "2d6!", and "(1d6+3)*2".
//
// The main entry point is Roll(expr) which returns a *RollResult.
// For custom random sources (e.g. testing), use RollWithSource(expr, r).
package dice

import (
	"fmt"
	"math/rand"
)

// RollResult is the result of a dice roll.
type RollResult struct {
	Expr   string       `json:"expr"`            // original expression
	Rolls  []int        `json:"rolls"`           // all individual die face values (flattened)
	Total  int          `json:"total"`           // final total
	Detail string       `json:"detail"`          // readable detail, e.g. "[3 5 2]+5"
	Terms  []TermResult `json:"terms,omitempty"` // per-term breakdown
}

// TermResult represents the result of a single dice term (e.g. one "3d6kh2" unit).
type TermResult struct {
	Expr       string `json:"expr"`       // term expression, e.g. "3d6kh2"
	Rolls      []int  `json:"rolls"`      // all individual die face values in this term
	Kept       []int  `json:"kept"`       // kept die totals (contribute to Total)
	Dropped    []int  `json:"dropped"`    // dropped die totals (by kh/kl)
	Explosions int    `json:"explosions"` // number of explosion rolls
	Total      int    `json:"total"`      // sum of kept dice
}

// String returns a readable representation: "expr = detail = total".
func (r *RollResult) String() string {
	return fmt.Sprintf("%s = %s = %d", r.Expr, r.Detail, r.Total)
}

// Roll parses and evaluates a dice expression using the global random source.
// Go 1.21+ auto-seeds the global source, so results are non-deterministic.
//
// Supported syntax:
//   - "3d6"       → roll 3 six-sided dice
//   - "1d100+5"   → roll 1d100 and add 5
//   - "1d6+2d8"   → roll 1d6 and 2d8, sum them
//   - "4d6kh3"    → roll 4d6, keep highest 3
//   - "4d6kl1"    → roll 4d6, keep lowest 1
//   - "2d6!"      → roll 2d6 with explosion (re-roll on max)
//   - "(1d6+3)*2" → parenthesized grouping
func Roll(expr string) (*RollResult, error) {
	return rollWithRand(expr, nil)
}

// RollWithSource is like Roll but accepts a custom *rand.Rand for deterministic testing.
func RollWithSource(expr string, r *rand.Rand) (*RollResult, error) {
	return rollWithRand(expr, r)
}

func rollWithRand(expr string, r *rand.Rand) (*RollResult, error) {
	node, err := Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("无效的骰子表达式: %w", err)
	}

	evalResult, err := node.Eval(r)
	if err != nil {
		return nil, err
	}

	// Flatten all individual die face values from all terms
	var allRolls []int
	for _, term := range evalResult.Terms {
		allRolls = append(allRolls, term.Rolls...)
	}

	return &RollResult{
		Expr:   expr,
		Rolls:  allRolls,
		Total:  evalResult.Value,
		Detail: evalResult.Detail,
		Terms:  evalResult.Terms,
	}, nil
}
