package parser

import (
	"fmt"
	"strconv"
)

// Expr is a parsed expression node.
type Expr interface {
	Eval() (float64, error)
}

// NumberExpr is a literal number.
type NumberExpr struct{ Value float64 }

func (n NumberExpr) Eval() (float64, error) { return n.Value, nil }

// BinOpExpr is a binary operation (a op b).
type BinOpExpr struct {
	Op    string
	Left  Expr
	Right Expr
}

func (b BinOpExpr) Eval() (float64, error) {
	l, err := b.Left.Eval()
	if err != nil {
		return 0, err
	}
	r, err := b.Right.Eval()
	if err != nil {
		return 0, err
	}
	switch b.Op {
	case "+":
		return l + r, nil
	case "-":
		return l - r, nil
	case "*":
		return l * r, nil
	case "/":
		if r == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return l / r, nil
	default:
		return 0, fmt.Errorf("unknown op: %s", b.Op)
	}
}

// Parser is a recursive descent parser for arithmetic expressions.
type Parser struct {
	tokens []Token
	pos    int
}

// Parse parses an expression string and returns the AST.
func Parse(input string) (Expr, error) {
	tokens := Tokenize(input)
	p := &Parser{tokens: tokens}
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.current().Kind != TokenEOF {
		return nil, fmt.Errorf("unexpected token: %s", p.current().Value)
	}
	return expr, nil
}

func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) advance() Token {
	t := p.current()
	p.pos++
	return t
}

func (p *Parser) parseExpr() (Expr, error) {
	return p.parseAddSub()
}

func (p *Parser) parseAddSub() (Expr, error) {
	left, err := p.parseMulDiv()
	if err != nil {
		return nil, err
	}
	for p.current().Kind == TokenPlus || p.current().Kind == TokenMinus {
		op := p.advance().Value
		right, err := p.parseMulDiv()
		if err != nil {
			return nil, err
		}
		left = BinOpExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseMulDiv() (Expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.current().Kind == TokenStar || p.current().Kind == TokenSlash {
		op := p.advance().Value
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = BinOpExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.current()
	switch t.Kind {
	case TokenNumber:
		p.advance()
		v, err := strconv.ParseFloat(t.Value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %s", t.Value)
		}
		return NumberExpr{Value: v}, nil
	case TokenLParen:
		p.advance()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.current().Kind != TokenRParen {
			return nil, fmt.Errorf("expected closing paren")
		}
		p.advance()
		return expr, nil
	default:
		return nil, fmt.Errorf("unexpected token: %s", t.Value)
	}
}
