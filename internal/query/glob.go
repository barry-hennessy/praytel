package query

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// GlobRegexp is the regular expression (for =~, so anchored) that matches
// what the glob g matches, with the rules of the focus and exclude flags:
// path.Match syntax, where * and ? also match "/".
func GlobRegexp(g string) (string, error) {
	if _, err := path.Match(g, ""); err != nil {
		return "", fmt.Errorf("glob %q: %w", g, err)
	}
	var b strings.Builder
	for i := 0; i < len(g); i++ {
		switch c := g[i]; c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '\\':
			if i+1 < len(g) {
				i++
				b.WriteString(regexp.QuoteMeta(g[i : i+1]))
			}
		case '[':
			j := strings.IndexByte(g[i+1:], ']')
			class := g[i+1 : i+1+j]
			if strings.HasPrefix(class, "^") {
				class = "^" + escapeClass(class[1:])
			} else {
				class = escapeClass(class)
			}
			b.WriteString("[" + class + "]")
			i += j + 1
		default:
			b.WriteString(regexp.QuoteMeta(g[i : i+1]))
		}
	}
	return b.String(), nil
}

// escapeClass keeps a glob character class's ranges and escapes the rest.
func escapeClass(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s):
			i++
			b.WriteString(`\` + s[i:i+1])
		case c == '-':
			b.WriteByte('-')
		default:
			b.WriteString(regexp.QuoteMeta(s[i : i+1]))
		}
	}
	return b.String()
}

// NameOrComponent is the query of the focus and exclude flags: spans whose
// name or component matches one of the globs. It is nil for no globs.
func NameOrComponent(globs []string) (Expr, error) {
	var pred Pred
	for _, g := range globs {
		re, err := GlobRegexp(g)
		if err != nil {
			return nil, err
		}
		one := POr{
			&Cmp{Field: Field{Scope: "span", Intrinsic: true, Name: "name"}, Op: Re, Value: Value{Kind: String, S: re}},
			&Cmp{Field: Field{Scope: "span", Intrinsic: true, Name: "component"}, Op: Re, Value: Value{Kind: String, S: re}},
		}
		if pred == nil {
			pred = one
		} else {
			pred = POr{pred, one}
		}
	}
	if pred == nil {
		return nil, nil
	}
	e := Select{Kind: KindSpan, Pred: pred}
	if err := Check(e); err != nil {
		return nil, err
	}
	return e, nil
}
