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
