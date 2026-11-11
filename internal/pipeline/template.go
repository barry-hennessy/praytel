package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/barry-hennessy/praytel/internal/projection"
)

// traceSink is a sink that writes each trace on its own.
type traceSink interface {
	WriteTrace(r projection.Reader, traceID string, lines []string) error
}

// fileTemplateSink writes each trace to the file its path names, filling
// the placeholders from the trace: {trace_id}, {name} (the root span's
// name) or {<key>} (an attribute of the root span). Each write replaces the
// file, so the last revision of a trace is what stays.
type fileTemplateSink struct {
	pattern string
}

var placeholder = regexp.MustCompile(`\{([^{}]+)\}`)

func (s fileTemplateSink) Write([]string) error {
	return errors.New("a file path with placeholders needs one trace at a time")
}

func (s fileTemplateSink) Close(int) int { return 0 }

func (s fileTemplateSink) WriteTrace(r projection.Reader, traceID string, lines []string) error {
	path, err := expandPath(s.pattern, r, traceID)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// expandPath fills pattern's placeholders from the trace. A value is made
// safe as one path element: it cannot add directories or climb out of
// them, whatever the traced program put in its attributes.
func expandPath(pattern string, r projection.Reader, traceID string) (string, error) {
	root := rootSpan(r, traceID)
	var missing []string
	path := placeholder.ReplaceAllStringFunc(pattern, func(m string) string {
		key := m[1 : len(m)-1]
		var v string
		switch {
		case key == "trace_id":
			v = traceID
		case root == nil:
		case key == "name":
			v = root.Name
		default:
			for _, a := range root.Attributes {
				if a.Key == key {
					v = a.Value.String()
					break
				}
			}
		}
		if v == "" {
			missing = append(missing, key)
			return m
		}
		return pathElement(v)
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("trace %s: no %s on its root span for %q", traceID, strings.Join(missing, ", "), pattern)
	}
	return path, nil
}

// rootSpan is the first real span of the trace with no parent in it.
func rootSpan(r projection.Reader, traceID string) *projection.Span {
	var root *projection.Span
	for sp := range r.Spans() {
		if sp.TraceID != traceID || sp.Synthetic || sp.Declared {
			continue
		}
		if sp.ParentID != "" && r.Span(traceID, sp.ParentID) != nil {
			continue
		}
		if root == nil || sp.Order < root.Order {
			root = sp
		}
	}
	return root
}

func pathElement(v string) string {
	v = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == 0:
			return '_'
		case r < 0x20:
			return -1
		}
		return r
	}, v)
	if strings.HasPrefix(v, ".") {
		v = "_" + v
	}
	return v
}
