package format

import (
	"regexp"
	"strings"
	"unicode"
)

var ctlSeq = regexp.MustCompile(`\x1b\[[?-]*[0-9;]*[A-Za-z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// SanitizeField strips control characters from a rendered field value.
func SanitizeField(s string) string {
	if s == "" {
		return s
	}

	s = ctlSeq.ReplaceAllString(s, "")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			continue
		case unicode.Is(unicode.Bidi_Control, r):
			continue
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}
