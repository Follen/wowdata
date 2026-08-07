package sqlquery

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind uint8

const (
	tokEOF tokenKind = iota
	tokIdent
	tokNumber
	tokString
	tokParameter
	tokComma
	tokDot
	tokLParen
	tokRParen
	tokStar
	tokPlus
	tokMinus
	tokSlash
	tokPercent
	tokEq
	tokNE
	tokLT
	tokLE
	tokGT
	tokGE
	tokSemicolon
)

type token struct {
	kind tokenKind
	lit  string
	pos  Position
}

type lexer struct {
	src       string
	offset    int
	line, col int
}

func lex(src string) ([]token, error) {
	l := &lexer{src: src, line: 1, col: 1}
	var out []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		if t.kind == tokEOF {
			return out, nil
		}
	}
}

func (l *lexer) next() (token, error) {
	for l.offset < len(l.src) {
		r, n := utf8.DecodeRuneInString(l.src[l.offset:])
		if unicode.IsSpace(r) {
			l.advance(r, n)
			continue
		}
		if r == '-' && strings.HasPrefix(l.src[l.offset:], "--") {
			for l.offset < len(l.src) {
				r, n = utf8.DecodeRuneInString(l.src[l.offset:])
				l.advance(r, n)
				if r == '\n' {
					break
				}
			}
			continue
		}
		if r == '/' && strings.HasPrefix(l.src[l.offset:], "/*") {
			start := l.position()
			l.advance('/', 1)
			l.advance('*', 1)
			for l.offset < len(l.src) && !strings.HasPrefix(l.src[l.offset:], "*/") {
				r, n = utf8.DecodeRuneInString(l.src[l.offset:])
				l.advance(r, n)
			}
			if l.offset >= len(l.src) {
				return token{}, sqlErr("sql_lex_error", start, "unterminated block comment")
			}
			l.advance('*', 1)
			l.advance('/', 1)
			continue
		}
		break
	}
	pos := l.position()
	if l.offset >= len(l.src) {
		return token{kind: tokEOF, pos: pos}, nil
	}
	r, n := utf8.DecodeRuneInString(l.src[l.offset:])
	switch r {
	case ',':
		l.advance(r, n)
		return token{kind: tokComma, lit: ",", pos: pos}, nil
	case '.':
		l.advance(r, n)
		return token{kind: tokDot, lit: ".", pos: pos}, nil
	case '(':
		l.advance(r, n)
		return token{kind: tokLParen, lit: "(", pos: pos}, nil
	case ')':
		l.advance(r, n)
		return token{kind: tokRParen, lit: ")", pos: pos}, nil
	case '*':
		l.advance(r, n)
		return token{kind: tokStar, lit: "*", pos: pos}, nil
	case '+':
		l.advance(r, n)
		return token{kind: tokPlus, lit: "+", pos: pos}, nil
	case '-':
		l.advance(r, n)
		return token{kind: tokMinus, lit: "-", pos: pos}, nil
	case '/':
		l.advance(r, n)
		return token{kind: tokSlash, lit: "/", pos: pos}, nil
	case '%':
		l.advance(r, n)
		return token{kind: tokPercent, lit: "%", pos: pos}, nil
	case '=':
		l.advance(r, n)
		return token{kind: tokEq, lit: "=", pos: pos}, nil
	case ';':
		l.advance(r, n)
		return token{kind: tokSemicolon, lit: ";", pos: pos}, nil
	case '<':
		l.advance(r, n)
		if l.take('=') {
			return token{kind: tokLE, lit: "<=", pos: pos}, nil
		}
		if l.take('>') {
			return token{kind: tokNE, lit: "<>", pos: pos}, nil
		}
		return token{kind: tokLT, lit: "<", pos: pos}, nil
	case '>':
		l.advance(r, n)
		if l.take('=') {
			return token{kind: tokGE, lit: ">=", pos: pos}, nil
		}
		return token{kind: tokGT, lit: ">", pos: pos}, nil
	case '!':
		l.advance(r, n)
		if l.take('=') {
			return token{kind: tokNE, lit: "!=", pos: pos}, nil
		}
		return token{}, sqlErr("sql_lex_error", pos, "expected '=' after '!'")
	case '\'', '"', '`':
		return l.quoted(r, pos)
	case ':':
		l.advance(r, n)
		start := l.offset
		for l.offset < len(l.src) {
			rr, nn := utf8.DecodeRuneInString(l.src[l.offset:])
			if !isIdentPart(rr) {
				break
			}
			l.advance(rr, nn)
		}
		if start == l.offset {
			return token{}, sqlErr("sql_lex_error", pos, "parameter name required after ':'")
		}
		return token{kind: tokParameter, lit: l.src[start:l.offset], pos: pos}, nil
	}
	if unicode.IsDigit(r) {
		start := l.offset
		dot := false
		for l.offset < len(l.src) {
			rr, nn := utf8.DecodeRuneInString(l.src[l.offset:])
			if rr == '.' && !dot {
				dot = true
				l.advance(rr, nn)
				continue
			}
			if !unicode.IsDigit(rr) {
				break
			}
			l.advance(rr, nn)
		}
		lit := l.src[start:l.offset]
		if _, err := strconv.ParseFloat(lit, 64); err != nil {
			return token{}, sqlErr("sql_lex_error", pos, "invalid number "+lit)
		}
		return token{kind: tokNumber, lit: lit, pos: pos}, nil
	}
	if isIdentStart(r) {
		start := l.offset
		for l.offset < len(l.src) {
			rr, nn := utf8.DecodeRuneInString(l.src[l.offset:])
			if !isIdentPart(rr) {
				break
			}
			l.advance(rr, nn)
		}
		return token{kind: tokIdent, lit: l.src[start:l.offset], pos: pos}, nil
	}
	return token{}, sqlErr("sql_lex_error", pos, "invalid character "+string(r))
}

func (l *lexer) quoted(quote rune, pos Position) (token, error) {
	l.advance(quote, utf8.RuneLen(quote))
	var b strings.Builder
	for l.offset < len(l.src) {
		r, n := utf8.DecodeRuneInString(l.src[l.offset:])
		l.advance(r, n)
		if r == quote {
			if l.offset < len(l.src) {
				rr, nn := utf8.DecodeRuneInString(l.src[l.offset:])
				if rr == quote {
					b.WriteRune(rr)
					l.advance(rr, nn)
					continue
				}
			}
			kind := tokString
			if quote != '\'' {
				kind = tokIdent
			}
			return token{kind: kind, lit: b.String(), pos: pos}, nil
		}
		b.WriteRune(r)
	}
	return token{}, sqlErr("sql_lex_error", pos, "unterminated quoted value")
}

func (l *lexer) position() Position { return Position{Offset: l.offset, Line: l.line, Column: l.col} }
func (l *lexer) advance(r rune, n int) {
	l.offset += n
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
}
func (l *lexer) take(want byte) bool {
	if l.offset < len(l.src) && l.src[l.offset] == want {
		l.advance(rune(want), 1)
		return true
	}
	return false
}
func isIdentStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func isIdentPart(r rune) bool  { return isIdentStart(r) || unicode.IsDigit(r) || r == '$' }
func sqlErr(code string, pos Position, msg string) error {
	return &Error{Code: code, Message: msg, Position: pos}
}
