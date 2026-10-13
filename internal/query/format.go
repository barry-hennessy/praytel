package query

import (
	"fmt"
	"strings"
)

// Format returns the canonical representation of q. It is stable and
// Parse(Format(q)) is equivalent to q. The default path view is omitted.
func Format(q *Query) string {
	if q == nil || q.root == nil {
		return ""
	}
	s := formatExpr(q.root, 0)
	if q.view != Path {
		s += " | " + q.view.String()
	}
	return s
}
func formatExpr(e expr, parent int) string {
	switch x := e.(type) {
	case *selector:
		if x.pred == nil {
			return x.kind + "{}"
		}
		return x.kind + "{ " + formatPred(x.pred, 0) + " }"
	case *binary:
		p := map[string]int{"||": 1, "&&": 2, ">": 3, ">>": 3}[x.op]
		s := formatExpr(x.left, p) + " " + x.op + " " + formatExpr(x.right, p+1)
		if p < parent {
			return "(" + s + ")"
		}
		return s
	}
	return ""
}
func formatPred(p pred, parent int) string {
	switch x := p.(type) {
	case *pbin:
		n := map[string]int{"||": 1, "&&": 2}[x.op]
		s := formatPred(x.left, n) + " " + x.op + " " + formatPred(x.right, n+1)
		if n < parent {
			return "(" + s + ")"
		}
		return s
	case *pnot:
		return "!" + formatPred(x.x, 3)
	case *exists:
		return "exists(" + x.field + ")"
	case *compare:
		v := x.value.String()
		if x.rawBare {
			v = x.raw
		}
		return x.field + " " + x.op + " " + v
	}
	return ""
}

// Explain returns a deterministic, human-readable checked plan.
func Explain(q *Query) string {
	if q == nil {
		return "<nil query>"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "view: %s (%s)\n", q.view, viewExplanation(q.view))
	b.WriteString("required finality: none for matches; final for absence claims\nexpression:\n")
	explainExpr(&b, q.root, "  ")
	return b.String()
}

func viewExplanation(v View) string {
	switch v {
	case Matches:
		return "direct evidence only"
	case WholeTrace:
		return "all spans and logs in each matching trace"
	default:
		return "evidence, span ancestors and descendants, and their logs"
	}
}

func explainExpr(b *strings.Builder, e expr, in string) {
	switch x := e.(type) {
	case *selector:
		fmt.Fprintf(b, "%s%s selector", in, x.kind)
		if x.pred != nil {
			fmt.Fprintf(b, " (%s)", formatPred(x.pred, 0))
		}
		b.WriteByte('\n')
		explainFields(b, x.pred, in+"  ", map[string]bool{})
	case *binary:
		fmt.Fprintf(b, "%s%s\n", in, map[string]string{"&&": "same-trace AND", "||": "same-trace OR", ">": "direct child", ">>": "descendant"}[x.op])
		explainExpr(b, x.left, in+"  ")
		explainExpr(b, x.right, in+"  ")
	}
}

func explainFields(b *strings.Builder, p pred, in string, seen map[string]bool) {
	switch x := p.(type) {
	case *pbin:
		explainFields(b, x.left, in, seen)
		explainFields(b, x.right, in, seen)
	case *pnot:
		explainFields(b, x.x, in, seen)
	case *exists:
		explainField(b, x.field, in, seen)
	case *compare:
		explainField(b, x.field, in, seen)
	}
}

func explainField(b *strings.Builder, field, in string, seen map[string]bool) {
	if seen[field] {
		return
	}
	seen[field] = true
	kind := "attribute (dynamic)"
	if k, ok := intrinsic[field]; ok {
		kind = k.String()
	}
	fmt.Fprintf(b, "%sfield %s: %s\n", in, field, kind)
}
