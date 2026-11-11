package query

import (
	"strconv"
	"strings"
	"time"
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
	case Child, Desc, After:
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
	case After:
		binary(e.Prev, "~>", e.Next)
	case Count:
		b.WriteString("count(")
		formatExpr(b, e.X, 0)
		b.WriteString(") " + e.Op.String() + " " + strconv.FormatInt(e.N, 10))
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
	case Duration:
		return time.Duration(v.I).String()
	case Arith:
		return formatExpression(v.E, 0)
	}
	return strconv.Quote(v.S)
}

// formatExpression writes arithmetic with the parentheses that precedence
// needs: + and - bind looser than * and /, and both are left-associative.
func formatExpression(e *Expression, min int) string {
	if e == nil {
		return ""
	}
	switch {
	case e.Var != "":
		return "$" + e.Var
	case e.Num != nil:
		return FormatValue(*e.Num)
	}
	prec := 1
	if e.Op == '*' || e.Op == '/' {
		prec = 2
	}
	s := formatExpression(e.L, prec) + " " + string(e.Op) + " " + formatExpression(e.R, prec+1)
	if prec < min {
		return "(" + s + ")"
	}
	return s
}
