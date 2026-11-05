// Package goldenreport turns the difference between the committed golden
// traces and freshly stamped ones into a pull request comment. Like
// benchreport it is kept apart from the Dagger module's functions so that it
// can be tested on its own.
package goldenreport

import (
	"fmt"
	"strings"
)

// Marker is hidden in every comment Report makes, so that the comment can be
// found again and updated rather than another posted.
const Marker = "<!-- praytel golden-traces -->"

// maxDiffBytes keeps a comment inside GitHub's 65,536 character limit, with
// room for what goes around the diff.
const maxDiffBytes = 55_000

// Results is what stamping the goldens again in a clean checkout produced.
type Results struct {
	// Diff is the unified diff of every golden that differs from the
	// committed one. A new golden is diffed against /dev/null, and a golden
	// whose test no longer produces one the other way round.
	Diff string
	// StampLog is what scripts/golden printed.
	StampLog string
	// StampFailed is whether scripts/golden exited non-zero.
	StampFailed bool
}

// Clean is whether there is nothing to point out.
func (r Results) Clean() bool {
	return !r.StampFailed && strings.TrimSpace(r.Diff) == ""
}

type change struct {
	path string
	kind string // changed, new or removed
}

// changes lists the goldens a unified diff touches.
func changes(diff string) []change {
	var (
		out     []change
		old     string
		oldPath string
	)
	for line := range strings.SplitSeq(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "--- "):
			old = strings.TrimPrefix(line, "--- ")
			oldPath = strings.TrimPrefix(old, "a/")
		case strings.HasPrefix(line, "+++ "):
			cur := strings.TrimPrefix(line, "+++ ")
			switch {
			case old == "/dev/null":
				out = append(out, change{strings.TrimPrefix(cur, "b/"), "new"})
			case cur == "/dev/null":
				out = append(out, change{oldPath, "removed"})
			default:
				out = append(out, change{oldPath, "changed"})
			}
		}
	}
	return out
}

// Report is the comment for res: title says what was compared, and
// detailsURL, if any, links to the run that has everything the comment
// leaves out. A clean result is a one-line comment, to replace an earlier
// one that no longer applies.
func Report(res Results, title, detailsURL string) string {
	var b strings.Builder
	head := "Golden traces"
	if title != "" {
		head += ": " + title
	}
	fmt.Fprintf(&b, "%s\n### %s\n\n", Marker, head)

	if res.Clean() {
		b.WriteString("Stamping the goldens again gives the committed ones: no differences.\n")
		return b.String()
	}

	b.WriteString("This does not fail the check. A golden is the trace of what a test calls, so a difference is either a change to read, then accept with `make golden` and commit, or a test whose trace depends on timing.\n\n")

	if res.StampFailed {
		b.WriteString("**Stamping failed.** Goldens of the tests that did run are compared below.\n\n")
		b.WriteString(fence(tail(res.StampLog, 4_000), "") + "\n")
	}

	if cs := changes(res.Diff); len(cs) > 0 {
		counts := map[string]int{}
		for _, c := range cs {
			counts[c.kind]++
		}
		fmt.Fprintf(&b, "**%d golden(s) differ from the committed ones:** %d changed, %d new, %d removed.\n\n",
			len(cs), counts["changed"], counts["new"], counts["removed"])
		for _, c := range cs {
			fmt.Fprintf(&b, "- `%s` (%s)\n", c.path, c.kind)
		}
		b.WriteString("\n")

		diff, cut := head8(res.Diff, maxDiffBytes)
		b.WriteString(fence(diff, "diff") + "\n")
		if cut {
			b.WriteString("\nThe diff is cut short")
			if detailsURL != "" {
				fmt.Fprintf(&b, "; the run has all of it: %s", detailsURL)
			}
			b.WriteString(".\n")
		}
	}
	return b.String()
}

// fence wraps text in a code fence longer than any run of backticks in it.
func fence(text, lang string) string {
	f := "```"
	for strings.Contains(text, f) {
		f += "`"
	}
	return f + lang + "\n" + strings.TrimRight(text, "\n") + "\n" + f
}

// head8 is the first n bytes of s, cut at a line end; cut says whether it
// had to cut.
func head8(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	s = s[:n]
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s, true
}

// tail is the last n bytes of s, starting at a line.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}
