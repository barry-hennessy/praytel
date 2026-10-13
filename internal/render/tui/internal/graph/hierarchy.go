package graph

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Hierarchy derives each span's place in a tree of nested components from
// the span names themselves rather than from a naming convention: names are
// arranged into a prefix tree per component, and a prefix becomes a
// component of its own wherever the names diverge after it (two or more
// names share it) or wherever it is itself an observed span name that other
// names extend. There is no separate notion of a function: what a span
// executes is simply the innermost component of its path, the tree's leaf.
//
// No particular separator is assumed, but a prefix may only end at a word
// boundary -- where a run of letters, digits or underscores gives way to
// anything else -- so "validate" and "view" never split at "v", and "get"
// is never a parent of "getAll".
//
// A path therefore depends on every name added so far: all names must be
// Added before any is resolved with Path.
type Hierarchy struct {
	roots map[string]*nameNode // by component
}

// nameNode is one word-boundary token's position in a component's prefix
// tree. observed marks that a span name ends exactly here.
type nameNode struct {
	children map[string]*nameNode
	observed bool
}

// NewHierarchy returns an empty hierarchy.
func NewHierarchy() *Hierarchy {
	return &Hierarchy{roots: map[string]*nameNode{}}
}

// Add records a span name under its component.
func (h *Hierarchy) Add(component, name string) {
	node, ok := h.roots[component]
	if !ok {
		node = &nameNode{}
		h.roots[component] = node
	}
	for _, tok := range tokenize(relativeName(component, name)) {
		child, ok := node.children[tok]
		if !ok {
			if node.children == nil {
				node.children = map[string]*nameNode{}
			}
			child = &nameNode{}
			node.children[tok] = child
		}
		node = child
	}
	node.observed = true
}

// Path resolves a span's component and name into its path through the
// tree: Component first, then one element per nested component down to the
// one the span itself names. Elements below the Component keep the
// separator that introduced them, so concatenating a path reproduces the
// full name (see Node.Label); e.g. with "Bar.Qux.Baz" and "Bar.Qux.Quux"
// added under component "Foo", the former resolves to
// ["Foo", ".Bar.Qux", ".Baz"].
func (h *Hierarchy) Path(component, name string) []string {
	var path []string
	if component != "" {
		path = append(path, component)
	}
	segment := ""
	flush := func() {
		if len(path) == 1 && component != "" && startsWithWord(segment) {
			segment = "." + segment
		}
		path = append(path, segment)
		segment = ""
	}

	node := h.roots[component]
	tokens := tokenize(relativeName(component, name))
	for i, tok := range tokens {
		segment += tok
		if node != nil {
			node = node.children[tok]
		}
		// A never-Added name has nothing to nest under past the point it
		// leaves the tree, so the rest of it stays one segment.
		last := i == len(tokens)-1
		if last || (node != nil && (node.observed || len(node.children) > 1)) {
			flush()
		}
	}
	return path
}

// relativeName strips a redundant leading component from name, along with
// the separator joining them, so "Foo.Bar" and "Bar" under component "Foo"
// land in the same place.
func relativeName(component, name string) string {
	if component == "" {
		return name
	}
	rest, ok := strings.CutPrefix(name, component)
	if !ok {
		return name
	}
	if r, _ := utf8.DecodeRuneInString(rest); rest != "" && isWord(r) {
		return name
	}
	return strings.TrimLeftFunc(rest, isSeparator)
}

// tokenize splits s at word boundaries into tokens that each carry their
// leading separator run ("a.b/c" -> "a", ".b", "/c"), so the tokens
// concatenate back to s. A trailing separator run stays on the last token.
func tokenize(s string) []string {
	var tokens []string
	start, seenWord := 0, false
	for i, r := range s {
		if isWord(r) {
			seenWord = true
			continue
		}
		if seenWord {
			tokens = append(tokens, s[start:i])
			start, seenWord = i, false
		}
	}
	if start == len(s) {
		return tokens
	}
	if seenWord || len(tokens) == 0 {
		return append(tokens, s[start:])
	}
	tokens[len(tokens)-1] += s[start:]
	return tokens
}

func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isSeparator(r rune) bool { return !isWord(r) }

func startsWithWord(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return s != "" && isWord(r)
}
