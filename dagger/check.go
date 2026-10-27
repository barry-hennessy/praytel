package main

import (
	"context"
)

// golangciImage is golangci-lint, which runs the linters .golangci.yml
// names, gosec among them.
const golangciImage = "golangci/golangci-lint:v2.15.0"

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
