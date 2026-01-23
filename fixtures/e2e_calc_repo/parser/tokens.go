package parser

// TokenKind identifies the type of a lexical token.
type TokenKind int

const (
	TokenNumber TokenKind = iota
	TokenPlus
	TokenMinus
	TokenStar
	TokenSlash
	TokenLParen
	TokenRParen
	TokenEOF
)

// Token represents a single lexical token.
type Token struct {
	Kind  TokenKind
	Value string
}

// Tokenize splits an input string into tokens.
func Tokenize(input string) []Token {
	var tokens []Token
	i := 0
	for i < len(input) {
		ch := input[i]
		switch {
		case ch == ' ' || ch == '\t':
			i++
		case ch >= '0' && ch <= '9' || ch == '.':
			start := i
			for i < len(input) && (input[i] >= '0' && input[i] <= '9' || input[i] == '.') {
				i++
			}
			tokens = append(tokens, Token{Kind: TokenNumber, Value: input[start:i]})
		case ch == '+':
			tokens = append(tokens, Token{Kind: TokenPlus, Value: "+"})
			i++
		case ch == '-':
			tokens = append(tokens, Token{Kind: TokenMinus, Value: "-"})
			i++
		case ch == '*':
			tokens = append(tokens, Token{Kind: TokenStar, Value: "*"})
			i++
		case ch == '/':
			tokens = append(tokens, Token{Kind: TokenSlash, Value: "/"})
			i++
		case ch == '(':
			tokens = append(tokens, Token{Kind: TokenLParen, Value: "("})
			i++
		case ch == ')':
			tokens = append(tokens, Token{Kind: TokenRParen, Value: ")"})
			i++
		default:
			i++ // skip unknown
		}
	}
	tokens = append(tokens, Token{Kind: TokenEOF, Value: ""})
	return tokens
}
