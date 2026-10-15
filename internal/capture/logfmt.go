package capture

import (
	"errors"
	"strconv"
	"strings"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/go-logfmt/logfmt"
)

// parseLogfmtLine reads key=value pairs, as written by slog's TextHandler,
// logrus and zerolog's console writer. Bare words are keys without values, so
// a console line with a timestamp and message before its pairs still parses.
// The line counts only if it names a trace or span; otherwise prose that
// happens to contain "a=b" would become structured. A syntax error ends the
// scan, keeping the pairs read before it.
func parseLogfmtLine(line string) result {
	// Every ID key ends in one of these, so a line without them cannot join
	// and need not be decoded.
	if !strings.Contains(line, "id=") && !strings.Contains(line, "Id=") && !strings.Contains(line, "ID=") {
		return result{}
	}

	dec := logfmt.NewDecoder(strings.NewReader(line))
	if !dec.ScanRecord() {
		return result{}
	}

	var r result
	for dec.ScanKeyval() {
		key, val := string(dec.Key()), string(dec.Value())
		if val != "" {
			r.addID(key, val)
		}
		r.attrs = append(r.attrs, domain.Attr{Key: key, Value: logfmtValue(val)})
	}

	if !r.joined() {
		return result{}
	}

	r.covered = len(line)
	if serr, ok := errors.AsType[*logfmt.SyntaxError](dec.Err()); ok {
		r.covered = serr.Pos - 1
	}

	return r
}

func logfmtValue(s string) domain.Value {
	if b, err := strconv.ParseBool(s); err == nil {
		return domain.BoolValue(b)
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return domain.Int64Value(i)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return domain.Float64Value(f)
	}
	return domain.StringValue(s)
}
