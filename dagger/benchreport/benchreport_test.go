package benchreport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const sampleDiff = `goos: linux
goarch: arm64
pkg: github.com/barry-hennessy/praytel/internal/filter
                               │     base      │                head                 │
                               │    sec/op     │   sec/op     vs base                │
ObserveWithFocus/spans=500-10    130.2m ± 3%   1.559m ± 1%  -98.80% (p=0.002 n=6)
ObserveWithFocus/spans=2000-10   2.502 ± 5%    6.654m ± 1%  -99.73% (p=0.002 n=6)
geomean                          570.8m        3.221m       -99.44%

                               │     base     │                head                │
                               │     B/op     │     B/op      vs base              │
ObserveWithFocus/spans=500-10    471.7Ki ± 0%   471.7Ki ± 0%       ~ (p=0.937 n=6)
ObserveWithFocus/spans=2000-10   1.866Mi ± 0%   1.866Mi ± 0%       ~ (p=0.394 n=6)
geomean                          938.2Ki        938.2Ki       +0.00%

                               │    base     │                head                 │
                               │  allocs/op  │  allocs/op   vs base                │
ObserveWithFocus/spans=500-10    25.97k ± 0%   5.849k ± 0%  -77.48% (p=0.002 n=6)
ObserveWithFocus/spans=2000-10   187.4k ± 0%   187.4k ± 0%       ~ (p=0.368 n=6)
geomean                          69.75k        33.10k       -52.55%
¹ all samples are equal
`

func TestTrimBenchstatKeepsTimesAndWhatChanged(t *testing.T) {
	got := Trim(sampleDiff)
	for _, want := range []string{
		"pkg: github.com/barry-hennessy/praytel/internal/filter",
		"│    sec/op     │",
		"ObserveWithFocus/spans=2000-10   2.502 ± 5%",
		"geomean                          570.8m",
		"│  allocs/op  │",
		"ObserveWithFocus/spans=500-10    25.97k ± 0%   5.849k",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("trimmed output lacks %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"B/op",                  // nothing in it changed
		"187.4k ± 0%   187.4k",  // an allocation row that didn't change
		"69.75k        33.10k",  // a geomean outside the time table
		"all samples are equal", // that table's notes
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("trimmed output keeps %q:\n%s", unwanted, got)
		}
	}
}

func TestBenchReportFitsInAComment(t *testing.T) {
	huge := strings.Repeat(sampleDiff+"\n", 200)
	report := Report(huge, "abc123 against main", "https://example.com/run")
	if len(report) > 65_536 {
		t.Fatalf("report is %d bytes, more than a comment can hold", len(report))
	}
	for _, want := range []string{Marker, "abc123 against main", "https://example.com/run", "cut short"} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

// fakeGitHub keeps one pull request's comments, across as many pages as
// they need.
type fakeGitHub struct {
	mu       sync.Mutex
	comments []issueComment
	nextID   int64
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer secret" {
		http.Error(w, "bad credentials", http.StatusUnauthorized)
		return
	}
	var in struct{ Body string }
	_ = json.NewDecoder(r.Body).Decode(&in)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/7/comments":
		page := 1
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		lo, hi := min((page-1)*100, len(f.comments)), min(page*100, len(f.comments))
		_ = json.NewEncoder(w).Encode(f.comments[lo:hi])
	case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/7/comments":
		_ = json.NewEncoder(w).Encode(f.add(in.Body))
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/o/r/issues/comments/"):
		var id int64
		fmt.Sscan(strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/comments/"), &id)
		for i := range f.comments {
			if f.comments[i].ID == id {
				f.comments[i].Body = in.Body
				_ = json.NewEncoder(w).Encode(f.comments[i])
				return
			}
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeGitHub) add(body string) issueComment {
	f.nextID++
	c := issueComment{ID: f.nextID, Body: body, HTMLURL: fmt.Sprintf("https://github.com/o/r/pull/7#issuecomment-%d", f.nextID)}
	f.comments = append(f.comments, c)
	return c
}

func TestUpsertCommentPostsOnceThenUpdates(t *testing.T) {
	fake := &fakeGitHub{}
	// Enough other comments that the earlier one is on the second page.
	for i := range 150 {
		fake.add(fmt.Sprintf("comment %d", i))
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	gh := GitHub{API: srv.URL, Token: "secret", Client: srv.Client()}
	ctx := context.Background()

	first, err := gh.Upsert(ctx, "o/r", 7, Marker+"\nfirst")
	if err != nil {
		t.Fatal(err)
	}
	second, err := gh.Upsert(ctx, "o/r", 7, Marker+"\nsecond")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("second run posted %s rather than updating %s", second, first)
	}
	if len(fake.comments) != 151 {
		t.Fatalf("%d comments, want the 150 others and one of ours", len(fake.comments))
	}
	if last := fake.comments[150].Body; !strings.HasSuffix(last, "second") {
		t.Fatalf("our comment says %q, want the second run's", last)
	}

	gh.Token = "wrong"
	if _, err := gh.Upsert(ctx, "o/r", 7, "x"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("bad token: err = %v, want a 401", err)
	}
}
