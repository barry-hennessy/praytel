package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type token struct {
	k, s string
	p    pos
}
type parser struct {
	src string
	t   []token
	i   int
}

// Parse parses and type-checks source. Attribute fields are dynamically typed;
// known intrinsic fields are checked against literal and operator types.
func Parse(source string) (*Query, error) {
	p := parser{src: source}
	if err := p.lex(); err != nil {
		return nil, err
	}
	e, err := p.expression(1)
	if err != nil {
		return nil, err
	}
	v := Path
	if p.take("|") {
		x := p.next()
		switch x.s {
		case "matches":
			v = Matches
		case "path":
			v = Path
		case "trace":
			v = WholeTrace
		default:
			return nil, p.err(x, "expected matches, path, or trace")
		}
	}
	if p.peek().k != "eof" {
		return nil, p.err(p.peek(), "unexpected token "+strconv.Quote(p.peek().s))
	}
	q := &Query{e, v}
	if err := checkExpr(e); err != nil {
		return nil, err
	}
	return q, nil
}

// Check validates source and returns its canonical checked query.
func Check(source string) (*Query, error) { return Parse(source) }

func (p *parser) lex() error {
	s := p.src
	for i := 0; i < len(s); {
		if unicode.IsSpace(rune(s[i])) {
			i++
			continue
		}
		st := i
		if s[i] == '"' {
			i++
			for i < len(s) {
				if s[i] == '\\' {
					i += 2
					continue
				}
				if s[i] == '"' {
					break
				}
				i++
			}
			if i >= len(s) {
				return &Error{st, len(s), "unterminated string literal"}
			}
			i++
			u, err := strconv.Unquote(s[st:i])
			if err != nil {
				return &Error{st, i, err.Error()}
			}
			p.t = append(p.t, token{"string", u, pos{st, i}})
			continue
		}
		two := ""
		if i+1 < len(s) {
			two = s[i : i+2]
		}
		switch two {
		case "&&", "||", ">>", ">=", "<=", "!=", "=~", "!~", "*=":
			p.t = append(p.t, token{two, two, pos{st, i + 2}})
			i += 2
			continue
		}
		if strings.ContainsRune("{}()!=>|<", rune(s[i])) {
			x := s[i : i+1]
			p.t = append(p.t, token{x, x, pos{st, i + 1}})
			i++
			continue
		}
		for i < len(s) && !unicode.IsSpace(rune(s[i])) && !strings.ContainsRune("{}()!=>|<*", rune(s[i])) {
			i++
		}
		if i == st {
			return &Error{st, st + 1, "unexpected character"}
		}
		x := s[st:i]
		k := "word"
		if _, e := strconv.ParseInt(x, 10, 64); e == nil {
			k = "int"
		} else if _, e := strconv.ParseFloat(x, 64); e == nil && strings.ContainsAny(x, ".eE") {
			k = "float"
		}
		p.t = append(p.t, token{k, x, pos{st, i}})
	}
	p.t = append(p.t, token{"eof", "", pos{len(s), len(s)}})
	return nil
}
func (p *parser) peek() token { return p.t[p.i] }
func (p *parser) next() token {
	x := p.peek()
	if x.k != "eof" {
		p.i++
	}
	return x
}
func (p *parser) take(k string) bool {
	if p.peek().k == k || p.peek().s == k {
		p.i++
		return true
	}
	return false
}
func (p *parser) err(t token, s string) error { return &Error{t.p.start, t.p.end, s} }
func (p *parser) expression(min int) (expr, error) {
	var l expr
	if p.take("(") {
		var e error
		l, e = p.expression(1)
		if e != nil {
			return nil, e
		}
		if !p.take(")") {
			return nil, p.err(p.peek(), "expected )")
		}
	} else {
		var e error
		l, e = p.selector()
		if e != nil {
			return nil, e
		}
	}
	for {
		op := p.peek().s
		prec := map[string]int{"||": 1, "&&": 2, ">": 3, ">>": 3}[op]
		if prec < min || prec == 0 {
			break
		}
		p.next()
		r, e := p.expression(prec + 1)
		if e != nil {
			return nil, e
		}
		l = &binary{pos{l.span().start, r.span().end}, op, l, r}
	}
	return l, nil
}
func (p *parser) selector() (expr, error) {
	k := p.next()
	if k.s != "span" && k.s != "log" {
		return nil, p.err(k, "expected span or log selector")
	}
	if !p.take("{") {
		return nil, p.err(p.peek(), "expected {")
	}
	var x pred
	var e error
	if !p.take("}") {
		x, e = p.predicate(1)
		if e != nil {
			return nil, e
		}
		if !p.take("}") {
			return nil, p.err(p.peek(), "expected }")
		}
	}
	return &selector{pos{k.p.start, p.t[p.i-1].p.end}, k.s, x}, nil
}
func (p *parser) predicate(min int) (pred, error) {
	var l pred
	if p.take("!") {
		x, e := p.predicate(3)
		if e != nil {
			return nil, e
		}
		l = &pnot{pos{p.t[p.i-1].p.start, x.span().end}, x}
	} else if p.take("(") {
		x, e := p.predicate(1)
		if e != nil {
			return nil, e
		}
		if !p.take(")") {
			return nil, p.err(p.peek(), "expected )")
		}
		l = x
	} else {
		f := p.next()
		if f.k != "word" {
			return nil, p.err(f, "expected field or exists")
		}
		if f.s == "exists" {
			if !p.take("(") {
				return nil, p.err(p.peek(), "expected (")
			}
			x := p.next()
			if x.k != "word" {
				return nil, p.err(x, "expected field")
			}
			if !p.take(")") {
				return nil, p.err(p.peek(), "expected )")
			}
			l = &exists{pos{f.p.start, p.t[p.i-1].p.end}, x.s}
		} else {
			o := p.next()
			if !regexp.MustCompile(`^(=|!=|>|>=|<|<=|=~|!~|\*=)$`).MatchString(o.s) {
				return nil, p.err(o, "expected comparison operator")
			}
			v := p.next()
			var val Value
			bare := false
			switch v.k {
			case "string":
				val = StringValue(v.s)
			case "int":
				n, _ := strconv.ParseInt(v.s, 10, 64)
				val = IntValue(n)
			case "float":
				n, _ := strconv.ParseFloat(v.s, 64)
				val = FloatValue(n)
			case "word":
				bare = true
				if v.s == "true" || v.s == "false" {
					val = BoolValue(v.s == "true")
				} else if d, err := time.ParseDuration(v.s); err == nil {
					val = IntValue(d.Nanoseconds())
				} else {
					val = StringValue(v.s)
				}
			default:
				return nil, p.err(v, "expected value")
			}
			l = &compare{pos{f.p.start, v.p.end}, f.s, o.s, val, bare, v.s}
		}
	}
	for {
		op := p.peek().s
		prec := map[string]int{"||": 1, "&&": 2}[op]
		if prec < min || prec == 0 {
			break
		}
		p.next()
		r, e := p.predicate(prec + 1)
		if e != nil {
			return nil, e
		}
		l = &pbin{pos{l.span().start, r.span().end}, op, l, r}
	}
	return l, nil
}

var intrinsic = map[string]ValueKind{"span:name": StringKind, "span:kind": StringKind, "span:status": StringKind, "span:status_message": StringKind, "span:duration": IntKind, "trace:id": StringKind, "trace:duration": IntKind, "log:body": StringKind, "log:source": StringKind, "log:severity_number": IntKind, "log:severity_text": StringKind}

func checkExpr(e expr) error {
	switch x := e.(type) {
	case *selector:
		return checkPredFor(x.pred, x.kind)
	case *binary:
		if z := checkExpr(x.left); z != nil {
			return z
		}
		return checkExpr(x.right)
	}
	return nil
}
func checkPred(p pred) error {
	return checkPredFor(p, "")
}

func checkPredFor(p pred, selector string) error {
	switch x := p.(type) {
	case nil:
		return nil
	case *pbin:
		if e := checkPredFor(x.left, selector); e != nil {
			return e
		}
		return checkPredFor(x.right, selector)
	case *pnot:
		return checkPredFor(x.x, selector)
	case *exists:
		return checkFieldScope(x.field, selector, x.p)
	case *compare:
		if err := checkFieldScope(x.field, selector, x.p); err != nil {
			return err
		}
		k, ok := intrinsic[x.field]
		if ok && k != x.value.Kind() {
			return &Error{x.p.start, x.p.end, fmt.Sprintf("%s has type %v, not %v", x.field, k, x.value.Kind())}
		}
		if (x.op == "=~" || x.op == "!~" || x.op == "*=") && x.value.Kind() != StringKind {
			return &Error{x.p.start, x.p.end, x.op + " requires a string value"}
		}
		if x.op == "=~" || x.op == "!~" {
			if _, e := regexp.Compile("^(?:" + x.value.s + ")$"); e != nil {
				return &Error{x.p.start, x.p.end, "invalid regular expression: " + e.Error()}
			}
		}
	}
	return nil
}

func checkFieldScope(field, selector string, p pos) error {
	if selector == "span" && strings.HasPrefix(field, "log:") {
		return &Error{p.start, p.end, "log intrinsic is not valid in a span selector"}
	}
	if selector == "log" && strings.HasPrefix(field, "span:") {
		return &Error{p.start, p.end, "span intrinsic is not valid in a log selector"}
	}
	return nil
}
