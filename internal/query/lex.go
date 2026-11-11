package query

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokKind uint8

const (
	tEOF tokKind = iota
	tIdent
	tString
	tNumber
	tLBrace
	tRBrace
	tLParen
	tRParen
	tAndAnd
	tOrOr
	tAnd
	tBang
	tGt
	tGtGt
	tGe
	tLt
	tLe
	tEq
	tNe
	tRe
	tNRe
	tContains
)

type token struct {
	kind tokKind
	text string // the literal's value for tString; the source otherwise
	pos  int    // byte offset
}

// Error is a syntax or type error at a column of the query.
type Error struct {
	Col int // 1-based
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("query:%d: %s", e.Col, e.Msg) }

func errAt(src string, pos int, format string, args ...any) *Error {
	return &Error{Col: utf8.RuneCountInString(src[:min(pos, len(src))]) + 1, Msg: fmt.Sprintf(format, args...)}
}

// isIdentRune is a rune of a field name or bare word: letters, digits and
// _ . : - / (attribute keys such as http.request.method, k8s.pod/name).
func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_.:-/", r)
}

func lex(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		r, w := utf8.DecodeRuneInString(src[i:])
		switch {
		case unicode.IsSpace(r):
			i += w
			continue
		case r == '"' || r == '\'':
			s, n, err := lexString(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tString, text: s, pos: i})
			i += n
			continue
		case unicode.IsDigit(r) || (r == '-' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9'):
			j := i + 1
			for j < len(src) && (strings.ContainsRune("0123456789.eE+-_", rune(src[j]))) {
				if (src[j] == '+' || src[j] == '-') && src[j-1] != 'e' && src[j-1] != 'E' {
					break
				}
				j++
			}
			toks = append(toks, token{kind: tNumber, text: src[i:j], pos: i})
			i = j
			continue
		case unicode.IsLetter(r) || r == '_':
			j := i
			for j < len(src) {
				r2, w2 := utf8.DecodeRuneInString(src[j:])
				if !isIdentRune(r2) {
					break
				}
				j += w2
			}
			toks = append(toks, token{kind: tIdent, text: src[i:j], pos: i})
			i = j
			continue
		}

		two := ""
		if i+2 <= len(src) {
			two = src[i : i+2]
		}
		kind, n := tEOF, 0
		switch two {
		case "&&":
			kind, n = tAndAnd, 2
		case "||":
			kind, n = tOrOr, 2
		case ">>":
			kind, n = tGtGt, 2
		case ">=":
			kind, n = tGe, 2
		case "<=":
			kind, n = tLe, 2
		case "!=":
			kind, n = tNe, 2
		case "=~":
			kind, n = tRe, 2
		case "!~":
			kind, n = tNRe, 2
		case "*=":
			kind, n = tContains, 2
		}
		if n == 0 {
			n = 1
			switch src[i] {
			case '{':
				kind = tLBrace
			case '}':
				kind = tRBrace
			case '(':
				kind = tLParen
			case ')':
				kind = tRParen
			case '&':
				kind = tAnd
			case '!':
				kind = tBang
			case '>':
				kind = tGt
			case '<':
				kind = tLt
			case '=':
				kind = tEq
			default:
				return nil, errAt(src, i, "unexpected %q", r)
			}
		}
		toks = append(toks, token{kind: kind, text: src[i : i+n], pos: i})
		i += n
	}
	return append(toks, token{kind: tEOF, pos: len(src)}), nil
}

// lexString reads a quoted string at src[i]. Both quote characters work;
// inside, \ escapes as in Go.
func lexString(src string, i int) (string, int, error) {
	q := src[i]
	j := i + 1
	for j < len(src) && src[j] != q {
		if src[j] == '\\' {
			j++
		}
		j++
	}
	if j >= len(src) {
		return "", 0, errAt(src, i, "unterminated string")
	}
	body := src[i+1 : j]
	if q == '\'' {
		// Read a single-quoted string as Go reads a double-quoted one.
		body = strings.ReplaceAll(strings.ReplaceAll(body, `\'`, `'`), `"`, `\"`)
	}
	s, err := strconv.Unquote(`"` + body + `"`)
	if err != nil {
		return "", 0, errAt(src, i, "bad string: %v", err)
	}
	return s, j + 1 - i, nil
}
