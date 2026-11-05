package main

import (
	"context"

	"dagger/demos/internal/dagger"
)

// golangciImage is golangci-lint, which runs the linters .golangci.yml
// names, gosec among them.
const golangciImage = "golangci/golangci-lint:v2.15.0@sha256:fbfcfd12a6f8d7f1b97b3362ca80fc2a244d46863148c73cb88e7793af96c481"

// Lint runs golangci-lint over the repository source with its
// .golangci.yml, and returns what it reports. Any finding fails the call.
func (m *Demos) Lint(ctx context.Context) (string, error) {
	return dag.Container().
		From(golangciImage).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("praytel-gomod")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("praytel-gobuild")).
		WithMountedCache("/root/.cache/golangci-lint", dag.CacheVolume("praytel-golangci")).
		WithDirectory("/src", m.Source).
		WithWorkdir("/src").
		WithExec([]string{"golangci-lint", "run", "./..."}).
		Stdout(ctx)
}

// govulncheckVersion pins golang.org/x/vuln/cmd/govulncheck.
const govulncheckVersion = "v1.9.0"

// Vulncheck runs govulncheck over the repository source, with the Go
// toolchain praytel is built with, and returns what it reports. It fails
// the call on any known vulnerability that praytel's code can reach, in a
// dependency or in the standard library; ones in code it never calls are
// listed but don't fail it.
//
// The vulnerability database changes without the source changing, so the
// call is never cached.
//
// +cache="never"
func (m *Demos) Vulncheck(ctx context.Context) (string, error) {
	return goContainer(m.Source).
		WithExec([]string{"go", "run", "golang.org/x/vuln/cmd/govulncheck@" + govulncheckVersion, "./..."}).
		Stdout(ctx)
}

// zizmorImage is zizmor, a security linter for GitHub Actions workflows.
const zizmorImage = "ghcr.io/zizmorcore/zizmor:1.31.1@sha256:42d409dbb851d4c5d35a058cf042b828bea1e3f2884378c526eb0974780e18d9"

// Zizmor audits the GitHub Actions workflows and actions in .github, and
// returns what it reports: script injection, excess permissions, persisted
// credentials, actions not pinned to a commit, and so on. Any finding
// fails the call.
//
// The vulnerability and commit data it checks against changes without the
// workflows changing, so the call is never cached.
//
// +cache="never"
func (m *Demos) Zizmor(
	ctx context.Context,
	// The repository's .github directory.
	// +defaultPath="/.github"
	workflows *dagger.Directory,
	// A GitHub token, to also run the audits that look actions up on
	// GitHub: that a pinned commit really is in the action's repository,
	// and that it has no known vulnerability. Read access is enough.
	// +optional
	token *dagger.Secret,
) (string, error) {
	ctr := dag.Container().
		From(zizmorImage).
		WithDirectory("/src/.github", workflows).
		WithWorkdir("/src")
	args := []string{"zizmor", "--color=never"}
	if token == nil {
		args = append(args, "--offline")
	} else {
		ctr = ctr.WithSecretVariable("GH_TOKEN", token)
	}
	return ctr.
		WithExec(append(args, ".github")).
		Stdout(ctx)
}
