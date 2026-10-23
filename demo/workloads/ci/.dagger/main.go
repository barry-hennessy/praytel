// A small CI pipeline for hello, the app beside this module. Its point is
// the shape praytel's graph view draws: a release fans out to lint, test
// and a build per platform, all running at once, then packages what was
// built.
package main

import (
	"context"
	"strings"
	"time"

	"dagger/ci/internal/dagger"

	"golang.org/x/sync/errgroup"
)

// The platforms a release is built for.
var platforms = []dagger.Platform{"linux/amd64", "linux/arm64", "darwin/arm64"}

type Ci struct {
	// +private
	Source *dagger.Directory
}

func New(
	// The app to build.
	// +defaultPath="app"
	source *dagger.Directory,
) *Ci {
	return &Ci{Source: source}
}

// Release checks the app and builds it for every platform, all at once,
// then packages the binaries into one archive.
//
// It is never cached, so every run shows the whole pipeline.
// +cache="never"
func (m *Ci) Release(ctx context.Context) (*dagger.File, error) {
	ctx, span := Tracer().Start(ctx, "release")
	defer span.End()

	// The group's context ends when Wait returns, so packaging gets ctx.
	eg, gctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return m.lint(gctx) })
	eg.Go(func() error { return m.test(gctx) })
	bins := make([]*dagger.File, len(platforms))
	for i, p := range platforms {
		eg.Go(func() (err error) {
			bins[i], err = m.build(gctx, p)
			return err
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return m.pack(ctx, bins)
}

// Lint vets the app.
func (m *Ci) Lint(ctx context.Context) error { return m.lint(ctx) }

// Test runs the app's tests.
func (m *Ci) Test(ctx context.Context) error { return m.test(ctx) }

func (m *Ci) lint(ctx context.Context) error {
	ctx, span := Tracer().Start(ctx, "lint")
	defer span.End()
	_, err := m.base().WithExec([]string{"go", "vet", "./..."}).Sync(ctx)
	return err
}

func (m *Ci) test(ctx context.Context) error {
	ctx, span := Tracer().Start(ctx, "test")
	defer span.End()
	_, err := m.base().WithExec([]string{"go", "test", "-v", "./..."}).Sync(ctx)
	return err
}

// build compiles the app for one platform. The span is named for the step,
// not the platform, so the three builds are one box that ran three times.
func (m *Ci) build(ctx context.Context, platform dagger.Platform) (*dagger.File, error) {
	ctx, span := Tracer().Start(ctx, "build")
	defer span.End()
	goos, goarch, _ := strings.Cut(string(platform), "/")
	bin := m.base().
		WithEnvVariable("CGO_ENABLED", "0").
		WithEnvVariable("GOOS", goos).
		WithEnvVariable("GOARCH", goarch).
		WithExec([]string{"go", "build", "-o", "/out/hello", "."}).
		// Report what was built: the module, Go version and target.
		WithExec([]string{"go", "version", "-m", "/out/hello"}).
		File("/out/hello")
	_, err := bin.Sync(ctx)
	return bin, err
}

// pack puts each platform's binary in one archive.
func (m *Ci) pack(ctx context.Context, bins []*dagger.File) (*dagger.File, error) {
	ctx, span := Tracer().Start(ctx, "package")
	defer span.End()
	dist := dag.Directory()
	for i, p := range platforms {
		dist = dist.WithFile(strings.ReplaceAll(string(p), "/", "-")+"/hello", bins[i])
	}
	archive := dag.Container().From("alpine:3.22").
		WithDirectory("/dist", dist).
		WithExec([]string{"tar", "-czf", "/hello.tar.gz", "-C", "/dist", "."}).
		File("/hello.tar.gz")
	_, err := archive.Sync(ctx)
	return archive, err
}

// base is a Go toolchain with the app mounted and module and build caches
// shared between steps. The run's start time is in its environment, so
// every step really runs, and logs, instead of being replayed from cache.
func (m *Ci) base() *dagger.Container {
	return dag.Container().From("golang:1.26-alpine").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("go-build")).
		WithEnvVariable("CI_RUN", runStarted).
		WithDirectory("/src", m.Source).
		WithWorkdir("/src")
}

var runStarted = time.Now().Format(time.RFC3339Nano)
