// Package benchreport turns benchstat's comparison into a pull request
// comment, and keeps one such comment on a pull request up to date. It is
// kept apart from the Dagger module's functions, which can only run inside
// a Dagger session, so that it can be tested on its own.
package benchreport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Marker is hidden in every comment Report makes, so that Upsert can find
// the comment again and update it rather than post another.
const Marker = "<!-- praytel bench-diff -->"

// maxTableBytes keeps a comment inside GitHub's 65,536 character limit,
// with room for what goes around the tables.
const maxTableBytes = 60_000

// Report is the comment for benchstat's text comparison diff: title says
// what was compared, and detailsURL, if any, links to what the comment
// leaves out.
func Report(diff, title, detailsURL string) string {
	if title == "" {
		title = "this pull request against its base"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n### Benchmarks: %s\n\n", Marker, title)
	b.WriteString("`~` means no significant difference. Shared runners vary by 5–10% in time from run to run; allocations don't. ")
	b.WriteString("Times are shown in full, memory and allocations only where they changed.")
	if detailsURL != "" {
		fmt.Fprintf(&b, " [Every table, and the profiles](%s).", detailsURL)
	}
	b.WriteString("\n\n```text\n")
	tables := Trim(diff)
	if len(tables) > maxTableBytes {
		tables = tables[:strings.LastIndex(tables[:maxTableBytes], "\n")+1] + "… cut short to fit in a comment\n"
	}
	b.WriteString(tables)
	b.WriteString("```\n")
	return b.String()
}

// significant matches a row's change where benchstat found one, such as
// "-12.34% (p=0.002 n=6)"; a row with none shows "~" instead.
var significant = regexp.MustCompile(`[+-]\d+(\.\d+)?% \(p=`)

// Trim keeps the parts of benchstat's text output worth reading in a
// comment: every time table whole, and of the others only the rows that
// changed, under their table's header. Configuration lines such as pkg:
// are kept where they lead into something kept.
func Trim(diff string) string {
	var out strings.Builder
	for _, block := range strings.Split(strings.TrimSpace(diff), "\n\n") {
		var config, header, rows, notes []string
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.Contains(line, "│"):
				header = append(header, line)
			case len(header) == 0:
				config = append(config, line)
			case footnote(line):
				notes = append(notes, line)
			default:
				rows = append(rows, line)
			}
		}
		if len(header) == 0 {
			continue
		}
		if !strings.Contains(header[len(header)-1], "sec/op") {
			var changed []string
			for _, row := range rows {
				if !strings.HasPrefix(row, "geomean") && significant.MatchString(row) {
					changed = append(changed, row)
				}
			}
			if len(changed) == 0 {
				continue
			}
			rows, notes = changed, nil
		}
		for _, part := range [][]string{config, header, rows, notes} {
			for _, line := range part {
				out.WriteString(line)
				out.WriteByte('\n')
			}
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// footnote reports whether line is one of benchstat's numbered notes, such
// as "¹ need >= 6 samples for confidence interval at level 0.95".
func footnote(line string) bool {
	for _, mark := range []string{"¹", "²", "³", "⁴", "⁵", "⁶", "⁷", "⁸", "⁹"} {
		if strings.HasPrefix(line, mark) {
			return true
		}
	}
	return false
}

// GitHub is just enough of GitHub's REST API to keep one comment on a pull
// request up to date.
type GitHub struct {
	API    string // such as https://api.github.com
	Token  string
	Client *http.Client
}

type issueComment struct {
	ID      int64  `json:"id"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

// Upsert updates the comment on pull request pr of repo (owner/name) that
// carries Marker, or posts one if there is none, and returns its URL.
func (g GitHub) Upsert(ctx context.Context, repo string, pr int, body string) (string, error) {
	api := strings.TrimSuffix(g.API, "/")
	for page := 1; ; page++ {
		var comments []issueComment
		url := fmt.Sprintf("%s/repos/%s/issues/%d/comments?per_page=100&page=%d", api, repo, pr, page)
		if err := g.do(ctx, http.MethodGet, url, nil, &comments); err != nil {
			return "", fmt.Errorf("list comments: %w", err)
		}
		for _, c := range comments {
			if strings.Contains(c.Body, Marker) {
				var updated issueComment
				url := fmt.Sprintf("%s/repos/%s/issues/comments/%d", api, repo, c.ID)
				if err := g.do(ctx, http.MethodPatch, url, map[string]string{"body": body}, &updated); err != nil {
					return "", fmt.Errorf("update comment: %w", err)
				}
				return updated.HTMLURL, nil
			}
		}
		if len(comments) < 100 {
			break
		}
	}
	var created issueComment
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", api, repo, pr)
	if err := g.do(ctx, http.MethodPost, url, map[string]string{"body": body}, &created); err != nil {
		return "", fmt.Errorf("post comment: %w", err)
	}
	return created.HTMLURL, nil
}

func (g GitHub) do(ctx context.Context, method, url string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s %s: %s: %s", method, url, resp.Status, bytes.TrimSpace(msg))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
