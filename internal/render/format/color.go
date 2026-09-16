package format

import (
	"fmt"
	"io"
	"os"
)

// ColorEnabled reports whether ANSI colour should be used for w.
func ColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}

	f, ok := w.(*os.File)
	if !ok {
		return false
	}

	st, err := f.Stat()
	if err != nil {
		return false
	}

	return st.Mode()&os.ModeCharDevice != 0
}

// ColorCode picks a stable foreground colour for id.
func ColorCode(id string) int {
	var h uint32
	for i := 0; i < len(id); i++ {
		h = 31*h + uint32(id[i])
	}

	return int(h%6) + 31
}

// ANSIWrap wraps s in an ANSI foreground colour sequence.
func ANSIWrap(s string, code int) string {
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", code, s)
}
