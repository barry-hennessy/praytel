package main

import (
	"context"

	"dagger/demos/benchreport"
	"dagger/demos/internal/dagger"
)

// BenchReport returns the comment BenchComment would post, without posting
// it.
func (m *Demos) BenchReport(
	ctx context.Context,
	// What BenchDiff returned.
	results *dagger.Directory,
	// What was compared, such as "abc123 against origin/main (def456)".
	// +optional
	title string,
	// A link to everything the comment leaves out: every table, and the
	// profiles.
	// +optional
	detailsURL string,
) (string, error) {
	diff, err := results.File("diff.txt").Contents(ctx)
	if err != nil {
		return "", err
	}
	return benchreport.Report(diff, title, detailsURL), nil
}

// BenchComment posts BenchDiff's comparison to a GitHub pull request, or
// updates the comment it posted there before, and returns the comment's
// URL. Times are shown in full, and memory and allocations only where they
// changed.
func (m *Demos) BenchComment(
	ctx context.Context,
	// What BenchDiff returned.
	results *dagger.Directory,
	// The repository, as owner/name.
	repo string,
	// The pull request's number.
	pr int,
	// A GitHub token that can comment on the pull request.
	token *dagger.Secret,
	// What was compared, such as "abc123 against origin/main (def456)".
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
	body, err := m.BenchReport(ctx, results, title, detailsURL)
	if err != nil {
		return "", err
	}
	plain, err := token.Plaintext(ctx)
	if err != nil {
		return "", err
	}
	return benchreport.GitHub{API: api, Token: plain}.Upsert(ctx, repo, pr, body)
}
