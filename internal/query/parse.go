package query

import (
	"strconv"
	"strings"
	"time"
)

// Parse reads a query and checks it (see Check).
//
//	expr    = either
//	either  = both { "||" both }
//	both    = chain { "&&" chain }
//	chain   = inter { (">" | ">>" | "~>") inter }
//	inter   = unary { "&" unary }
//	unary   = "!" unary | "(" expr ")" | ("span" | "log") "{" [ pred ] "}"
//	        | "count" "(" expr ")" op integer
//	pred    = por
//	por     = pand { "||" pand }
//	pand    = pun { "&&" pun }
//	pun     = "!" pun | "(" pred ")" | "exists" "(" field ")" | field op value
//	op      = "=" | "!=" | ">" | ">=" | "<" | "<=" | "=~" | "!~" | "*="
//	value   = string | number | duration | "true" | "false" | word | arith
//	arith   = term { ("+" | "-") term }
//	term    = factor { ("*" | "/") factor }
//	factor  = "$" name | number | duration | "(" arith ")"
func Parse(src string) (Expr, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{src: src, toks: toks}
	e, err := p.either()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, errAt(src, t.pos, "unexpected %q", t.text)
	}
	if err := Check(e); err != nil {
		return nil, err
	}
	return e, nil
}

type parser struct {
	src  string
	toks []token
	i    int
	// kind is the selector being parsed, for bare attribute names.
	kind Kind
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }

func (p *parser) expect(k tokKind, what string) (token, error) {
	t := p.next()
	if t.kind != k {
		return t, p.unexpected(t, what)
	}
	return t, nil
}

func (p *parser) unexpected(t token, want string) error {
	if t.kind == tEOF {
		return errAt(p.src, t.pos, "want %s, got the end", want)
	}
	return errAt(p.src, t.pos, "want %s, got %q", want, t.text)
}

func (p *parser) either() (Expr, error) {
	l, err := p.both()
	for err == nil && p.peek().kind == tOrOr {
		p.next()
		var r Expr
		if r, err = p.both(); err == nil {
			l = Either{l, r}
		}
	}
	return l, err
}

func (p *parser) both() (Expr, error) {
	l, err := p.chain()
	for err == nil && p.peek().kind == tAndAnd {
		p.next()
		var r Expr
		if r, err = p.chain(); err == nil {
			l = Both{l, r}
		}
	}
	return l, err
}

func (p *parser) chain() (Expr, error) {
	l, err := p.inter()
	for err == nil && (p.peek().kind == tGt || p.peek().kind == tGtGt || p.peek().kind == tAfter) {
		op := p.next().kind
		var r Expr
		if r, err = p.inter(); err == nil {
			switch op {
			case tGt:
				l = Child{l, r}
			case tGtGt:
				l = Desc{l, r}
			default:
				l = After{l, r}
			}
		}
	}
	return l, err
}

func (p *parser) inter() (Expr, error) {
	l, err := p.unary()
	for err == nil && p.peek().kind == tAnd {
		p.next()
		var r Expr
		if r, err = p.unary(); err == nil {
			l = Inter{l, r}
		}
	}
	return l, err
}

func (p *parser) unary() (Expr, error) {
	t := p.next()
	switch {
	case t.kind == tBang:
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return Not{x}, nil
	case t.kind == tLParen:
		e, err := p.either()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, `")"`); err != nil {
			return nil, err
		}
		return e, nil
	case t.kind == tIdent && t.text == "count" && p.peek().kind == tLParen:
		p.next()
		x, err := p.either()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, `")"`); err != nil {
			return nil, err
		}
		opTok := p.next()
		op, ok := cmpOps[opTok.kind]
		if !ok || op == Re || op == NRe || op == Contains {
			return nil, p.unexpected(opTok, "a comparison of the count")
		}
		nTok := p.next()
		n, err := strconv.ParseInt(nTok.text, 10, 64)
		if nTok.kind != tNumber || err != nil || n < 0 {
			return nil, p.unexpected(nTok, "a whole number")
		}
		return Count{X: x, Op: op, N: n}, nil
	case t.kind == tIdent && (t.text == "span" || t.text == "log"):
		p.kind = KindSpan
		if t.text == "log" {
			p.kind = KindLog
		}
		if _, err := p.expect(tLBrace, `"{"`); err != nil {
			return nil, err
		}
		sel := Select{Kind: p.kind}
		if p.peek().kind != tRBrace {
			pr, err := p.por()
			if err != nil {
				return nil, err
			}
			sel.Pred = pr
		}
		if _, err := p.expect(tRBrace, `"}"`); err != nil {
			return nil, err
		}
		return sel, nil
	}
	return nil, p.unexpected(t, `span{…}, log{…}, "(" or "!"`)
}

func (p *parser) por() (Pred, error) {
	l, err := p.pand()
	for err == nil && p.peek().kind == tOrOr {
		p.next()
		var r Pred
		if r, err = p.pand(); err == nil {
			l = POr{l, r}
		}
	}
	return l, err
}

func (p *parser) pand() (Pred, error) {
	l, err := p.pun()
	for err == nil && p.peek().kind == tAndAnd {
		p.next()
		var r Pred
		if r, err = p.pun(); err == nil {
			l = PAnd{l, r}
		}
	}
	return l, err
}

var cmpOps = map[tokKind]Op{
	tEq: Eq, tNe: Ne, tGt: Gt, tGe: Ge, tLt: Lt, tLe: Le, tRe: Re, tNRe: NRe, tContains: Contains,
}

func (p *parser) pun() (Pred, error) {
	t := p.peek()
	switch {
	case t.kind == tBang:
		p.next()
		x, err := p.pun()
		if err != nil {
			return nil, err
		}
		return PNot{x}, nil
	case t.kind == tLParen:
		p.next()
		x, err := p.por()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, `")"`); err != nil {
			return nil, err
		}
		return x, nil
	case t.kind == tIdent && t.text == "exists" && p.toks[p.i+1].kind == tLParen:
		p.next()
		p.next()
		f, err := p.field()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, `")"`); err != nil {
			return nil, err
		}
		return Exists{f}, nil
	}

	f, err := p.field()
	if err != nil {
		return nil, err
	}
	opTok := p.next()
	op, ok := cmpOps[opTok.kind]
	if !ok {
		return nil, p.unexpected(opTok, "a comparison")
	}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	return &Cmp{Field: f, Op: op, Value: v}, nil
}

func (p *parser) field() (Field, error) {
	t := p.next()
	if t.kind != tIdent {
		return Field{}, p.unexpected(t, "a field")
	}
	return ParseField(t.text, p.kind)
}

// ParseField reads a field name; a bare KEY is an attribute of a record of
// kind k.
func ParseField(s string, k Kind) (Field, error) {
	if scope, name, ok := strings.Cut(s, ":"); ok {
		return Field{Scope: scope, Intrinsic: true, Name: name}, nil
	}
	for _, scope := range []string{"span", "log", "resource"} {
		if name, ok := strings.CutPrefix(s, scope+"."); ok {
			return Field{Scope: scope, Name: name}, nil
		}
	}
	return Field{Scope: k.String(), Name: s}, nil
}

func (p *parser) value() (Value, error) {
	if p.startsArith() {
		e, err := p.arith()
		if err != nil {
			return Value{}, err
		}
		if e.Num != nil {
			return *e.Num, nil // a plain number after all
		}
		return Value{Kind: Arith, E: e}, nil
	}
	t := p.next()
	switch t.kind {
	case tString:
		return Value{Kind: String, S: t.text}, nil
	case tNumber, tDuration:
		p.i--
		return p.literal()
	case tIdent:
		switch t.text {
		case "true", "false":
			return Value{Kind: Bool, B: t.text == "true"}, nil
		}
		return Value{Kind: Ident, S: t.text}, nil
	}
	return Value{}, p.unexpected(t, "a value")
}

// startsArith reports whether the value ahead is arithmetic: a variable, a
// parenthesis, or a number or duration followed by an operator.
func (p *parser) startsArith() bool {
	switch p.peek().kind {
	case tVar, tLParen:
		return true
	case tNumber, tDuration:
		switch p.toks[p.i+1].kind {
		case tPlus, tMinus, tStar, tSlash:
			return true
		}
	}
	return false
}

func (p *parser) arith() (*Expression, error) {
	l, err := p.term()
	for err == nil && (p.peek().kind == tPlus || p.peek().kind == tMinus) {
		op := p.next().text[0]
		var r *Expression
		if r, err = p.term(); err == nil {
			l = &Expression{Op: op, L: l, R: r}
		}
	}
	return l, err
}

func (p *parser) term() (*Expression, error) {
	l, err := p.factor()
	for err == nil && (p.peek().kind == tStar || p.peek().kind == tSlash) {
		op := p.next().text[0]
		var r *Expression
		if r, err = p.factor(); err == nil {
			l = &Expression{Op: op, L: l, R: r}
		}
	}
	return l, err
}

func (p *parser) factor() (*Expression, error) {
	t := p.next()
	switch t.kind {
	case tVar:
		return &Expression{Var: t.text}, nil
	case tLParen:
		e, err := p.arith()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, `")"`); err != nil {
			return nil, err
		}
		return e, nil
	case tNumber, tDuration:
		p.i--
		v, err := p.literal()
		if err != nil {
			return nil, err
		}
		return &Expression{Num: &v}, nil
	}
	return nil, p.unexpected(t, "a variable or a number")
}

// literal reads a number or a duration.
func (p *parser) literal() (Value, error) {
	t := p.next()
	if t.kind == tDuration {
		d, err := time.ParseDuration(t.text)
		if err != nil {
			return Value{}, errAt(p.src, t.pos, "bad duration %q", t.text)
		}
		return Value{Kind: Duration, I: int64(d)}, nil
	}
	text := strings.ReplaceAll(t.text, "_", "")
	if i, err := strconv.ParseInt(text, 10, 64); err == nil {
		return Value{Kind: Int, I: i}, nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return Value{}, errAt(p.src, t.pos, "bad number %q", t.text)
	}
	return Value{Kind: Float, F: f}, nil
}
