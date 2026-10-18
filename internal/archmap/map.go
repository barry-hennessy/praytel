// Package archmap records a run's component graph as an architecture map:
// plain text files, one per entry point, that hold only what is structural
// -- which components ran and which called which -- so the same behaviour
// always produces the same bytes and the files can be committed, diffed
// and reviewed like code. See docs/guides/maps.md.
//
// A map is built from the same parent/child facts as the TUI's graph
// view, but deliberately differently: components are flat (service, span
// name) pairs rather than nested by whatever other names a run happened
// to see, and nothing that varies between runs -- counts, timings,
// arrival order -- is kept.
package archmap

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Header is a map file's first line: the format and its version.
const Header = "praytel-map v1"

// Node is a component: a span name within a service.
type Node struct {
	Service, Name string
}

func compareNodes(a, b Node) int {
	return cmp.Or(strings.Compare(a.Service, b.Service), strings.Compare(a.Name, b.Name))
}

// Label is how the node is shown in a drawing: the service, then the span
// name, joined by a dot unless the name already starts with the service.
// This is the box label the graph view uses.
func (n Node) Label() string {
	if n.Service == "" {
		return n.Name
	}
	if rest, ok := strings.CutPrefix(n.Name, n.Service); ok && rest != "" && !isWord(rune(rest[0])) {
		return n.Name
	}
	return n.Service + "." + n.Name
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// Call is one component calling another: a span of From was the parent of
// a span of To.
type Call struct {
	From, To Node
}

func compareCalls(a, b Call) int {
	return cmp.Or(compareNodes(a.From, b.From), compareNodes(a.To, b.To))
}

// Map is a set of entry points, components and calls. A map file holds
// one entry point; reading several files, or a directory, combines them.
type Map struct {
	Entries map[Node]bool
	Nodes   map[Node]bool
	Calls   map[Call]bool
}

// New returns an empty map.
func New() Map {
	return Map{Entries: map[Node]bool{}, Nodes: map[Node]bool{}, Calls: map[Call]bool{}}
}

// Merge adds everything in o to m.
func (m Map) Merge(o Map) {
	for n := range o.Entries {
		m.Entries[n] = true
	}
	for n := range o.Nodes {
		m.Nodes[n] = true
	}
	for c := range o.Calls {
		m.Calls[c] = true
	}
}

// Equal reports whether m and o hold the same entry points, nodes and calls.
func (m Map) Equal(o Map) bool {
	return mapsEqual(m.Entries, o.Entries) && mapsEqual(m.Nodes, o.Nodes) && mapsEqual(m.Calls, o.Calls)
}

func mapsEqual[K comparable](a, b map[K]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// SortedNodes returns m's nodes in file order.
func (m Map) SortedNodes() []Node { return sortedKeys(m.Nodes, compareNodes) }

// SortedCalls returns m's calls in file order.
func (m Map) SortedCalls() []Call { return sortedKeys(m.Calls, compareCalls) }

// SortedEntries returns m's entry points in file order.
func (m Map) SortedEntries() []Node { return sortedKeys(m.Entries, compareNodes) }

func sortedKeys[K comparable](m map[K]bool, compare func(a, b K) int) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.SortFunc(out, compare)
	return out
}

// field writes s bare when it reads back unambiguously as one field, and
// as a Go double-quoted string otherwise.
func field(s string) string {
	if s == "" || s == "->" || strings.ContainsFunc(s, func(r rune) bool {
		return r == '"' || unicode.IsSpace(r) || !unicode.IsPrint(r)
	}) {
		return strconv.Quote(s)
	}
	return s
}

func nodeFields(n Node) string { return field(n.Service) + " " + field(n.Name) }

// NodeLine is the line a node is written as, and how a changed node is
// listed.
func NodeLine(n Node) string { return "node " + nodeFields(n) }

// CallLine is the line a call is written as, and how a changed call is
// listed.
func CallLine(c Call) string { return "call " + nodeFields(c.From) + " -> " + nodeFields(c.To) }

// Format writes m in canonical form. m must hold exactly one entry point.
func Format(m Map) ([]byte, error) {
	if len(m.Entries) != 1 {
		return nil, fmt.Errorf("archmap: a map file holds one entry point, not %d", len(m.Entries))
	}
	var b bytes.Buffer
	b.WriteString(Header + "\n")
	b.WriteString("entry " + nodeFields(m.SortedEntries()[0]) + "\n")
	if len(m.Nodes) > 0 {
		b.WriteString("\n")
	}
	for _, n := range m.SortedNodes() {
		b.WriteString(NodeLine(n) + "\n")
	}
	if len(m.Calls) > 0 {
		b.WriteString("\n")
	}
	for _, c := range m.SortedCalls() {
		b.WriteString(CallLine(c) + "\n")
	}
	return b.Bytes(), nil
}

// Parse reads one map file.
func Parse(r io.Reader) (Map, error) {
	m := New()
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if line == 1 {
			if text != Header {
				return Map{}, fmt.Errorf("line 1: want %q, got %q", Header, text)
			}
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		fields, err := splitFields(text)
		if err != nil {
			return Map{}, fmt.Errorf("line %d: %w", line, err)
		}
		switch {
		case len(fields) == 3 && fields[0] == "entry":
			n := Node{fields[1], fields[2]}
			m.Entries[n] = true
			m.Nodes[n] = true
		case len(fields) == 3 && fields[0] == "node":
			m.Nodes[Node{fields[1], fields[2]}] = true
		case len(fields) == 6 && fields[0] == "call" && fields[3] == "->":
			c := Call{Node{fields[1], fields[2]}, Node{fields[4], fields[5]}}
			m.Calls[c] = true
			m.Nodes[c.From] = true
			m.Nodes[c.To] = true
		default:
			return Map{}, fmt.Errorf("line %d: not an entry, node or call: %q", line, text)
		}
	}
	if err := sc.Err(); err != nil {
		return Map{}, err
	}
	if line == 0 {
		return Map{}, errors.New("empty file")
	}
	return m, nil
}

// splitFields splits a line at spaces, reading any field that starts with
// a double quote as a Go quoted string.
func splitFields(s string) ([]string, error) {
	var out []string
	for s != "" {
		if s[0] == ' ' {
			s = s[1:]
			continue
		}
		if s[0] == '"' {
			q, err := strconv.QuotedPrefix(s)
			if err != nil {
				return nil, fmt.Errorf("bad quoted field in %q", s)
			}
			u, _ := strconv.Unquote(q)
			out = append(out, u)
			s = s[len(q):]
			continue
		}
		end := strings.IndexByte(s, ' ')
		if end < 0 {
			end = len(s)
		}
		out = append(out, s[:end])
		s = s[end:]
	}
	return out, nil
}

// Fragments are map files keyed by the entry point they are for. Files
// naming the same entry point are combined.
type Fragments map[Node]Map

// Union combines every fragment into one map.
func (f Fragments) Union() Map {
	m := New()
	for _, frag := range f {
		m.Merge(frag)
	}
	return m
}

// Load reads a map file, or every *.map file below a directory.
func Load(path string) (Fragments, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var files []string
	if info.IsDir() {
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(p, ".map") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		files = []string{path}
	}
	frags := Fragments{}
	for _, file := range files {
		m, err := loadFile(file)
		if err != nil {
			return nil, err
		}
		for e := range m.Entries {
			if frags[e].Nodes == nil {
				frags[e] = New()
			}
			frags[e].Merge(m)
		}
	}
	return frags, nil
}

func loadFile(path string) (Map, error) {
	f, err := os.Open(path)
	if err != nil {
		return Map{}, err
	}
	defer f.Close()
	m, err := Parse(f)
	if err != nil {
		return Map{}, fmt.Errorf("%s: %w", path, err)
	}
	if len(m.Entries) != 1 {
		return Map{}, fmt.Errorf("%s: want one entry line, got %d", path, len(m.Entries))
	}
	return m, nil
}

// FileName is where the map for entry point e lives below a map
// directory: SERVICE/NAME.map, with anything but letters, digits, '_', '-'
// and '.' (and a leading '.') escaped as %XX so every entry point has its
// own file and none escapes the directory.
func FileName(e Node) string {
	return filepath.Join(escape(e.Service), escape(e.Name)+".map")
}

func escape(s string) string {
	if s == "" {
		return "%"
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' && i > 0
		if ok {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
