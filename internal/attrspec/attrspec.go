// Package attrspec parses the --snapshot-attrs selection of span attributes:
// whitespace-separated key globs, where a "-" prefix excludes.
package attrspec

import (
	"fmt"
	"path"
	"strings"
)

// Spec selects attribute keys. The zero value shows none.
type Spec struct {
	// Any is true when attributes may be shown at all (the flag was given).
	Any      bool
	includes []string
	excludes []string
}

// All is the spec that shows every attribute.
func All() Spec { return Spec{Any: true} }

// Parse reads a whitespace-separated list of key globs. An empty list means
// every attribute; a "-" prefix excludes a glob, and excludes win. A list of
// only excludes means everything except those.
func Parse(s string) (Spec, error) {
	sp := Spec{Any: true}
	for f := range strings.FieldsSeq(s) {
		neg := strings.HasPrefix(f, "-")
		pat := strings.TrimPrefix(f, "-")
		if pat == "" {
			return Spec{}, fmt.Errorf("empty attribute pattern %q", f)
		}
		if _, err := path.Match(pat, ""); err != nil {
			return Spec{}, fmt.Errorf("attribute pattern %q: %w", pat, err)
		}
		if neg {
			sp.excludes = append(sp.excludes, pat)
		} else {
			sp.includes = append(sp.includes, pat)
		}
	}
	return sp, nil
}

// Match reports whether the attribute with this key is shown.
func (s Spec) Match(key string) bool {
	if !s.Any {
		return false
	}
	for _, p := range s.excludes {
		if ok, _ := path.Match(p, key); ok {
			return false
		}
	}
	if len(s.includes) == 0 {
		return true
	}
	for _, p := range s.includes {
		if ok, _ := path.Match(p, key); ok {
			return true
		}
	}
	return false
}
