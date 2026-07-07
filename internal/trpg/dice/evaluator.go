// Package dice — AST node definitions and evaluator.
// Each Node implementation knows how to evaluate itself against a random source.
package dice

import (
	"fmt"
	"math/rand"
	"sort"
)

// Node is the AST node interface for dice expressions.
type Node interface {
	// Eval evaluates the node, returning the numeric value, per-term results, and detail string.
	Eval(r *rand.Rand) (*EvalResult, error)
	// String returns the expression representation of this node.
	String() string
}

// EvalResult is the result of evaluating an AST node.
type EvalResult struct {
	Value  int          // numeric value of this node
	Terms  []TermResult // dice terms encountered during evaluation
	Detail string       // readable calculation detail, e.g. "[3 5 2]+5"
}

// NumberNode represents a literal number constant.
type NumberNode struct {
	Value int
}

func (n *NumberNode) Eval(_ *rand.Rand) (*EvalResult, error) {
	return &EvalResult{
		Value:  n.Value,
		Detail: fmt.Sprintf("%d", n.Value),
	}, nil
}

func (n *NumberNode) String() string {
	return fmt.Sprintf("%d", n.Value)
}

// DiceNode represents a dice roll expression like 3d6, 4d6kh3, 2d6!.
type DiceNode struct {
	Count    int  // number of dice (1-1000)
	Sides    int  // sides per die (1-10000)
	KeepHigh int  // keep highest N dice (0 = disabled)
	KeepLow  int  // keep lowest N dice (0 = disabled)
	Explode  bool // explode on max roll
	ExplodeN int  // max explosion depth (0 = default 10)
}

func (d *DiceNode) String() string {
	s := fmt.Sprintf("%dd%d", d.Count, d.Sides)
	if d.KeepHigh > 0 {
		s += fmt.Sprintf("kh%d", d.KeepHigh)
	}
	if d.KeepLow > 0 {
		s += fmt.Sprintf("kl%d", d.KeepLow)
	}
	if d.Explode {
		s += "!"
		if d.ExplodeN > 0 {
			s += fmt.Sprintf("%d", d.ExplodeN)
		}
	}
	return s
}

// Eval rolls the dice and applies modifiers (keep high/low, explosion).
func (d *DiceNode) Eval(r *rand.Rand) (*EvalResult, error) {
	// Validation
	if d.Count < 1 || d.Count > 1000 {
		return nil, fmt.Errorf("骰子数量必须在 1-1000 之间，当前为 %d", d.Count)
	}
	if d.Sides < 1 || d.Sides > 10000 {
		return nil, fmt.Errorf("骰子面数必须在 1-10000 之间，当前为 %d", d.Sides)
	}
	if d.KeepHigh > 0 && d.KeepLow > 0 {
		return nil, fmt.Errorf("不能同时使用 kh 和 kl")
	}
	if d.KeepHigh > d.Count {
		return nil, fmt.Errorf("kh 值 %d 超过骰子数量 %d", d.KeepHigh, d.Count)
	}
	if d.KeepLow > d.Count {
		return nil, fmt.Errorf("kl 值 %d 超过骰子数量 %d", d.KeepLow, d.Count)
	}

	maxExplode := d.ExplodeN
	if maxExplode == 0 {
		maxExplode = 10 // prevent infinite loops
	}

	rollDie := func() int {
		if r != nil {
			return r.Intn(d.Sides) + 1
		}
		return rand.Intn(d.Sides) + 1
	}

	// Roll each die, handling explosions. Each die may produce multiple face values.
	type dieResult struct {
		rolls []int // individual face values (initial + explosions)
		total int   // sum of all face values for this die
	}

	dice := make([]dieResult, d.Count)
	totalExplosions := 0

	for i := 0; i < d.Count; i++ {
		roll := rollDie()
		dr := dieResult{rolls: []int{roll}, total: roll}

		if d.Explode && d.Sides > 1 {
			explodeCount := 0
			for roll == d.Sides && explodeCount < maxExplode {
				explodeCount++
				totalExplosions++
				roll = rollDie()
				dr.rolls = append(dr.rolls, roll)
				dr.total += roll
			}
		}
		dice[i] = dr
	}

	// Collect all individual face values (flattened)
	var allRolls []int
	for _, dr := range dice {
		allRolls = append(allRolls, dr.rolls...)
	}

	// Apply keep high / keep low
	var keptValues []int
	var droppedValues []int

	if d.KeepHigh > 0 || d.KeepLow > 0 {
		indices := make([]int, d.Count)
		for i := range indices {
			indices[i] = i
		}

		if d.KeepHigh > 0 {
			// Sort descending by die total
			sort.Slice(indices, func(a, b int) bool {
				return dice[indices[a]].total > dice[indices[b]].total
			})
		} else {
			// Sort ascending by die total
			sort.Slice(indices, func(a, b int) bool {
				return dice[indices[a]].total < dice[indices[b]].total
			})
		}

		keepCount := d.KeepHigh
		if d.KeepLow > 0 {
			keepCount = d.KeepLow
		}
		for i := 0; i < d.Count; i++ {
			if i < keepCount {
				keptValues = append(keptValues, dice[indices[i]].total)
			} else {
				droppedValues = append(droppedValues, dice[indices[i]].total)
			}
		}
	} else {
		// No keep modifier: all dice are kept
		for _, dr := range dice {
			keptValues = append(keptValues, dr.total)
		}
	}

	// Calculate total from kept dice
	total := 0
	for _, v := range keptValues {
		total += v
	}

	// Build detail string
	detail := fmt.Sprintf("%v", keptValues)
	if len(droppedValues) > 0 {
		detail = fmt.Sprintf("%v 丢弃%v", keptValues, droppedValues)
	}
	if totalExplosions > 0 {
		detail += fmt.Sprintf(" (爆炸×%d)", totalExplosions)
	}

	term := TermResult{
		Expr:       d.String(),
		Rolls:      allRolls,
		Kept:       keptValues,
		Dropped:    droppedValues,
		Explosions: totalExplosions,
		Total:      total,
	}

	return &EvalResult{
		Value:  total,
		Terms:  []TermResult{term},
		Detail: detail,
	}, nil
}

// BinOpNode represents a binary operation (+, -, *, /).
type BinOpNode struct {
	Op          rune // '+', '-', '*', '/'
	Left, Right Node
}

func (n *BinOpNode) String() string {
	return fmt.Sprintf("(%s %c %s)", n.Left, n.Op, n.Right)
}

func (n *BinOpNode) Eval(r *rand.Rand) (*EvalResult, error) {
	left, err := n.Left.Eval(r)
	if err != nil {
		return nil, err
	}
	right, err := n.Right.Eval(r)
	if err != nil {
		return nil, err
	}

	result := &EvalResult{
		Terms:  append(left.Terms, right.Terms...),
		Detail: fmt.Sprintf("%s%c%s", left.Detail, n.Op, right.Detail),
	}

	switch n.Op {
	case '+':
		result.Value = left.Value + right.Value
	case '-':
		result.Value = left.Value - right.Value
	case '*':
		result.Value = left.Value * right.Value
	case '/':
		if right.Value == 0 {
			return nil, fmt.Errorf("除以零")
		}
		result.Value = left.Value / right.Value
	default:
		return nil, fmt.Errorf("未知运算符: %c", n.Op)
	}

	return result, nil
}
