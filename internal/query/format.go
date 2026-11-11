package query

import (
	"strconv"
	"strings"
)

// Format is the canonical text of e. It parses back to the same tree, and
// it puts parentheses wherever precedence would otherwise decide.
func Format(e Expr) string {
	var b strings.Builder
	formatExpr(&b, e, 0)
	return b.String()
}

const (
	precEither = iota + 1
	precBoth
	precChain
	precInter
	precUnary
)

func precOf(e Expr) int {
	switch e.(type) {
	case Either:
		return precEither
	case Both:
		return precBoth
	case Child, Desc:
		return precChain
	case Inter:
		return precInter
	}
	return precUnary
}

// formatExpr writes e, in parentheses if its precedence is below min.
func formatExpr(b *strings.Builder, e Expr, min int) {
	p := precOf(e)
	if p < min {
		b.WriteByte('(')
		defer b.WriteByte(')')
	}
	binary := func(l Expr, op string, r Expr) {
		// Left-associative: the right side must bind tighter.
		formatExpr(b, l, p)
		b.WriteString(" " + op + " ")
		formatExpr(b, r, p+1)
	}
	switch e := e.(type) {
	case Select:
		b.WriteString(e.Kind.String())
		if e.Pred == nil {
			b.WriteString("{}")
			return
		}
		b.WriteString("{ ")
		formatPred(b, e.Pred, 0)
		b.WriteString(" }")
	case Either:
		binary(e.L, "||", e.R)
	case Both:
		binary(e.L, "&&", e.R)
	case Child:
		binary(e.Parent, ">", e.Kid)
	case Desc:
		binary(e.Anc, ">>", e.Kid)
	case Inter:
		binary(e.L, "&", e.R)
	case Not:
		b.WriteByte('!')
		formatExpr(b, e.X, precUnary)
	}
}

const (
	precPOr = iota + 1
	precPAnd
	precPUnary
)

func formatPred(b *strings.Builder, p Pred, min int) {
	prec := precPUnary
	switch p.(type) {
	case POr:
		prec = precPOr
	case PAnd:
		prec = precPAnd
	}
	if prec < min {
		b.WriteByte('(')
		defer b.WriteByte(')')
	}
	switch p := p.(type) {
	case *Cmp:
		b.WriteString(FormatField(p.Field) + " " + p.Op.String() + " " + FormatValue(p.Value))
	case Exists:
		b.WriteString("exists(" + FormatField(p.Field) + ")")
	case PNot:
		b.WriteByte('!')
		formatPred(b, p.P, precPUnary)
	case PAnd:
		formatPred(b, p.L, precPAnd)
		b.WriteString(" && ")
		formatPred(b, p.R, precPAnd+1)
	case POr:
		formatPred(b, p.L, precPOr)
		b.WriteString(" || ")
		formatPred(b, p.R, precPOr+1)
	}
}

// FormatField is the canonical name of f.
func FormatField(f Field) string {
	if f.Intrinsic {
		return f.Scope + ":" + f.Name
	}
	return f.Scope + "." + f.Name
}

// FormatValue is the canonical text of v.
func FormatValue(v Value) string {
	switch v.Kind {
	case Int:
		return strconv.FormatInt(v.I, 10)
	case Float:
		s := strconv.FormatFloat(v.F, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEn") { // keep it a float: 2 -> 2.0
			s += ".0"
		}
		return s
	case Bool:
		return strconv.FormatBool(v.B)
	case Ident:
		return v.S
	}
	return strconv.Quote(v.S)
}
