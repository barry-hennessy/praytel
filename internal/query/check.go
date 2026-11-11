package query

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
)

type fieldType uint8

const (
	ftString fieldType = iota
	ftInt
	ftEnum
)

type intrinsic struct {
	typ   fieldType
	enums []string
}

var intrinsics = map[string]map[string]intrinsic{
	"span": {
		"name":      {typ: ftString},
		"component": {typ: ftString},
		"id":        {typ: ftString},
		"parent_id": {typ: ftString},
		"status":    {typ: ftEnum, enums: []string{"unset", "ok", "error"}},
		"kind":      {typ: ftEnum, enums: []string{"unspecified", "internal", "server", "client", "producer", "consumer"}},
	},
	"trace": {
		"id": {typ: ftString},
	},
	"log": {
		"body":          {typ: ftString},
		"source":        {typ: ftEnum, enums: []string{"stdout", "stderr", "otlp"}},
		"severity":      {typ: ftInt},
		"severity_text": {typ: ftString},
	},
}

// Check validates every field and value of e and compiles its regular
// expressions. Parse calls it; call it on a tree made some other way.
func Check(e Expr) error {
	switch e := e.(type) {
	case Select:
		if e.Pred == nil {
			return nil
		}
		return checkPred(e.Pred, e.Kind)
	case Both:
		return errors.Join(Check(e.L), Check(e.R))
	case Either:
		return errors.Join(Check(e.L), Check(e.R))
	case Inter:
		return errors.Join(Check(e.L), Check(e.R))
	case Not:
		return Check(e.X)
	case Child:
		return errors.Join(Check(e.Parent), Check(e.Kid))
	case Desc:
		return errors.Join(Check(e.Anc), Check(e.Kid))
	case nil:
		return errors.New("empty query")
	}
	return fmt.Errorf("unknown query node %T", e)
}

func checkPred(p Pred, k Kind) error {
	switch p := p.(type) {
	case *Cmp:
		return checkCmp(p, k)
	case Exists:
		_, err := fieldInfo(p.Field, k)
		return err
	case PNot:
		return checkPred(p.P, k)
	case PAnd:
		return errors.Join(checkPred(p.L, k), checkPred(p.R, k))
	case POr:
		return errors.Join(checkPred(p.L, k), checkPred(p.R, k))
	}
	return fmt.Errorf("unknown predicate %T", p)
}

// fieldInfo checks that a record of kind k has f, and returns its type
// (nil for an attribute, which can hold any type).
func fieldInfo(f Field, k Kind) (*intrinsic, error) {
	allowed := map[Kind][]string{
		KindSpan: {"span", "trace", "resource"},
		KindLog:  {"log", "trace"},
	}[k]
	if !slices.Contains(allowed, f.Scope) {
		return nil, fmt.Errorf("a %s has no %s field %s", k, f.Scope, FormatField(f))
	}
	if !f.Intrinsic {
		if f.Name == "" {
			return nil, fmt.Errorf("empty attribute name in %s", FormatField(f))
		}
		return nil, nil
	}
	in, ok := intrinsics[f.Scope][f.Name]
	if !ok {
		return nil, fmt.Errorf("unknown field %s", FormatField(f))
	}
	return &in, nil
}

func checkCmp(c *Cmp, k Kind) error {
	in, err := fieldInfo(c.Field, k)
	if err != nil {
		return err
	}
	field := FormatField(c.Field)
	if c.Value.Kind == Ident && (in == nil || in.typ != ftEnum) {
		return fmt.Errorf("%s: bare word %s is not a value here; quote it", field, c.Value.S)
	}
	if in != nil && in.typ == ftEnum {
		if c.Op != Eq && c.Op != Ne {
			return fmt.Errorf("%s compares only with = and !=", field)
		}
		v := c.Value.S
		if c.Value.Kind != Ident && c.Value.Kind != String || !slices.Contains(in.enums, v) {
			return fmt.Errorf("%s is one of %v, not %s", field, in.enums, FormatValue(c.Value))
		}
		c.Value = Value{Kind: Ident, S: v}
		return nil
	}
	switch c.Op {
	case Re, NRe:
		if c.Value.Kind != String {
			return fmt.Errorf("%s %s needs a string pattern", field, c.Op)
		}
		re, err := regexp.Compile(`^(?:` + c.Value.S + `)$`)
		if err != nil {
			return fmt.Errorf("%s %s: %w", field, c.Op, err)
		}
		c.re = re
	case Contains:
		if c.Value.Kind != String {
			return fmt.Errorf("%s *= needs a string", field)
		}
	case Gt, Ge, Lt, Le:
		if c.Value.Kind == Bool {
			return fmt.Errorf("%s %s cannot order a bool", field, c.Op)
		}
	}
	if in != nil && in.typ == ftInt && c.Value.Kind != Int && c.Value.Kind != Float {
		return fmt.Errorf("%s is a number, not %s", field, FormatValue(c.Value))
	}
	return nil
}

// RecordLocal reports whether one record decides the query on its own: a
// selector, or &, || and ! of selectors of the same kind. Such a query can
// filter events as they arrive. Any other query needs the trace: its
// structure (> and >>) or more than one record (&&).
func RecordLocal(e Expr) (Kind, bool) {
	switch e := e.(type) {
	case Select:
		return e.Kind, true
	case Inter:
		return sameKind(e.L, e.R)
	case Either:
		return sameKind(e.L, e.R)
	case Not:
		return RecordLocal(e.X)
	}
	return 0, false
}

func sameKind(a, b Expr) (Kind, bool) {
	ka, oka := RecordLocal(a)
	kb, okb := RecordLocal(b)
	return ka, oka && okb && ka == kb
}
