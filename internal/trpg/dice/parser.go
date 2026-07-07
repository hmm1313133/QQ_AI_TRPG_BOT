// Package dice provides dice expression parsing and rolling.
// This file implements a recursive descent parser for dice expressions.
//
// Grammar (EBNF):
//
//	expr     := term (('+' | '-') term)*
//	term     := factor (('*' | '/') factor)*
//	factor   := '(' expr ')' | dice | number
//	dice     := [count] ('d'|'D') sides [modifier]*
//	modifier := 'kh' number | 'kl' number | '!' [number]
//	number   := digit+
package dice

import (
	"fmt"
	"strconv"
	"unicode"
)

// parser is a recursive descent parser for dice expressions.
type parser struct {
	input string
	pos   int
}

// Parse parses a dice expression string into an AST.
// Supported syntax: "3d6", "1d100+5", "1d6+2d8", "4d6kh3", "2d6!", "(1d6+3)*2".
func Parse(input string) (Node, error) {
	p := &parser{input: input}
	p.skipSpace()
	node, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos < len(p.input) {
		return nil, fmt.Errorf("位置 %d 处有意外字符: %q", p.pos, string(p.input[p.pos]))
	}
	return node, nil
}

func (p *parser) skipSpace() {
	for p.pos < len(p.input) && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t') {
		p.pos++
	}
}

// parseExpr parses addition and subtraction (lowest precedence).
func (p *parser) parseExpr() (Node, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.input) {
			break
		}
		ch := p.input[p.pos]
		if ch == '+' || ch == '-' {
			p.pos++
			right, err := p.parseTerm()
			if err != nil {
				return nil, err
			}
			left = &BinOpNode{Op: rune(ch), Left: left, Right: right}
		} else {
			break
		}
	}
	return left, nil
}

// parseTerm parses multiplication and division (higher precedence than +/-).
func (p *parser) parseTerm() (Node, error) {
	left, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.input) {
			break
		}
		ch := p.input[p.pos]
		if ch == '*' || ch == '/' {
			p.pos++
			right, err := p.parseFactor()
			if err != nil {
				return nil, err
			}
			left = &BinOpNode{Op: rune(ch), Left: left, Right: right}
		} else {
			break
		}
	}
	return left, nil
}

// parseFactor parses a parenthesized expression, dice, or number.
func (p *parser) parseFactor() (Node, error) {
	p.skipSpace()
	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("表达式不完整")
	}
	ch := p.input[p.pos]
	if ch == '(' {
		p.pos++
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return nil, fmt.Errorf("缺少右括号 ')'")
		}
		p.pos++
		return expr, nil
	}
	return p.parseDiceOrNumber()
}

// parseDiceOrNumber parses a dice expression (NdM[modifiers]) or a plain number.
func (p *parser) parseDiceOrNumber() (Node, error) {
	// Read optional leading number (dice count or just a number)
	start := p.pos
	for p.pos < len(p.input) && unicode.IsDigit(rune(p.input[p.pos])) {
		p.pos++
	}
	numStr := p.input[start:p.pos]

	// Check for 'd' or 'D' indicating a dice expression
	if p.pos < len(p.input) && (p.input[p.pos] == 'd' || p.input[p.pos] == 'D') {
		p.pos++ // consume 'd'/'D'

		count := 1
		if numStr != "" {
			var err error
			count, err = strconv.Atoi(numStr)
			if err != nil {
				return nil, fmt.Errorf("骰子数量无效: %s", numStr)
			}
		}

		// Read sides
		sidesStart := p.pos
		for p.pos < len(p.input) && unicode.IsDigit(rune(p.input[p.pos])) {
			p.pos++
		}
		if p.pos == sidesStart {
			return nil, fmt.Errorf("骰子面数缺失")
		}
		sides, err := strconv.Atoi(p.input[sidesStart:p.pos])
		if err != nil {
			return nil, fmt.Errorf("骰子面数无效")
		}

		node := &DiceNode{Count: count, Sides: sides}
		if err := p.parseModifiers(node); err != nil {
			return nil, err
		}
		return node, nil
	}

	// Plain number (no 'd' followed)
	if numStr == "" {
		return nil, fmt.Errorf("位置 %d: 期望数字或骰子表达式", p.pos)
	}
	val, err := strconv.Atoi(numStr)
	if err != nil {
		return nil, fmt.Errorf("数字无效: %s", numStr)
	}
	return &NumberNode{Value: val}, nil
}

// parseModifiers parses dice modifiers: kh (keep high), kl (keep low), ! (explode).
func (p *parser) parseModifiers(node *DiceNode) error {
	for p.pos < len(p.input) {
		ch := p.input[p.pos]

		// Check 'kh' or 'kl'
		if ch == 'k' && p.pos+1 < len(p.input) {
			next := p.input[p.pos+1]
			if next == 'h' || next == 'l' {
				p.pos += 2
				n, err := p.readNumber()
				if err != nil {
					return fmt.Errorf("kh/kl 后需要数字: %w", err)
				}
				if next == 'h' {
					node.KeepHigh = n
				} else {
					node.KeepLow = n
				}
				continue
			}
		}

		// Check '!' (explosion)
		if ch == '!' {
			node.Explode = true
			p.pos++
			// Optional max explosion count
			if p.pos < len(p.input) && unicode.IsDigit(rune(p.input[p.pos])) {
				n, _ := p.readNumber()
				node.ExplodeN = n
			}
			continue
		}

		break
	}
	return nil
}

// readNumber reads a positive integer from the input.
func (p *parser) readNumber() (int, error) {
	start := p.pos
	for p.pos < len(p.input) && unicode.IsDigit(rune(p.input[p.pos])) {
		p.pos++
	}
	if p.pos == start {
		return 0, fmt.Errorf("期望数字")
	}
	return strconv.Atoi(p.input[start:p.pos])
}
