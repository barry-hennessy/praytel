package main

import (
	"context"
	"strings"

	"dagger/demos/benchreport"
	"dagger/demos/goldenreport"
	"dagger/demos/internal/dagger"
)

// goldenScript stamps every golden again in a copy of the repository and
// writes, to /out, what differs from the committed ones: diff.txt, a unified
// diff with new and removed goldens against /dev/null; stamp.log, what
// scripts/golden printed; and stamp.rc, its exit status. It never fails: a
// difference is for a person to read, not for CI to block on.
const goldenScript = `set -u
mkdir -p /out
cp -R /src /before
rc=0
scripts/golden >/out/stamp.log 2>&1 || rc=$?
echo "$rc" >/out/stamp.rc
: >/out/diff.txt
files=$( (cd /before && find . -name '*.golden'; cd /src && find . -name '*.golden') | sed 's|^\./||' | sort -u)
for f in $files; do
  a=/before/$f; la=a/$f
  b=/src/$f;    lb=b/$f
  [ -e "$a" ] || { a=/dev/null; la=/dev/null; }
  [ -e "$b" ] || { b=/dev/null; lb=/dev/null; }
  diff -u --label "$la" --label "$lb" "$a" "$b" >>/out/diff.txt || true
done
exit 0`

// GoldenDiff stamps the goldens of every test's trace tree and graph with
// scripts/golden and returns what differs from the committed ones. It does
// not fail on a difference. Update the goldens with make golden and commit.
func (m *Demos) GoldenDiff() *dagger.Directory {
	return goContainer(m.Source).
		WithExec([]string{"bash", "-c", goldenScript}).
		Directory("/out")
}

func goldenResults(ctx context.Context, results *dagger.Directory) (goldenreport.Results, error) {
	diff, err := results.File("diff.txt").Contents(ctx)
	if err != nil {
		return goldenreport.Results{}, err
	}
	log, err := results.File("stamp.log").Contents(ctx)
	if err != nil {
		return goldenreport.Results{}, err
	}
	rc, err := results.File("stamp.rc").Contents(ctx)
	if err != nil {
		return goldenreport.Results{}, err
	}
	return goldenreport.Results{Diff: diff, StampLog: log, StampFailed: strings.TrimSpace(rc) != "0"}, nil
}

// GoldenReport returns the comment GoldenComment would post, without
// posting it.
func (m *Demos) GoldenReport(
	ctx context.Context,
	// What GoldenDiff returned.
	results *dagger.Directory,
	// What was compared, such as the commit.
	// +optional
	title string,
	// A link to everything the comment leaves out.
	// +optional
	detailsURL string,
) (string, error) {
	res, err := goldenResults(ctx, results)
	if err != nil {
		return "", err
	}
	return goldenreport.Report(res, title, detailsURL), nil
}

// GoldenComment posts GoldenDiff's differences to a GitHub pull request, or
// updates the comment it posted there before, and returns the comment's URL.
// With no differences it only updates an earlier comment, to say they are
// gone, and otherwise posts nothing.
func (m *Demos) GoldenComment(
	ctx context.Context,
	// What GoldenDiff returned.
	results *dagger.Directory,
	// The repository, as owner/name.
	repo string,
	// The pull request's number.
	pr int,
	// A GitHub token that can comment on the pull request.
	token *dagger.Secret,
	// What was compared, such as the commit.
	// +optional
	title string,
	// A link to everything the comment leaves out.
	// +optional
	detailsURL string,
	// The GitHub API to post to.
	// +optional
	// +default="https://api.github.com"
	api string,
) (string, error) {
	res, err := goldenResults(ctx, results)
	if err != nil {
		return "", err
	}
	plain, err := token.Plaintext(ctx)
	if err != nil {
		return "", err
	}
	gh := benchreport.GitHub{API: api, Token: plain, Marker: goldenreport.Marker}
	body := goldenreport.Report(res, title, detailsURL)
	if res.Clean() {
		return gh.Update(ctx, repo, pr, body)
	}
	return gh.Upsert(ctx, repo, pr, body)
}
