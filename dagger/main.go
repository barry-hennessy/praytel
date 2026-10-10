// Records praytel's demo walkthroughs.
//
// A scenario is a directory under demo/scenarios holding a VHS tape, its
// captions and a scenario.toml naming the workload and the toolchain images
// needed to build or supply it. Each recording runs a real praytel TUI
// session inside a container, so `dagger call` produces the same output
// locally and in CI.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dagger/demos/internal/dagger"

	"github.com/BurntSushi/toml"
)

const (
	goImage  = "golang:1.26"
	vhsImage = "ghcr.io/charmbracelet/vhs:v0.10.0"

	scenariosDir = "demo/scenarios"
	workloadsDir = "demo/workloads"
)

type Demos struct {
	// +private
	Source *dagger.Directory
}

func New(
	// The praytel repository.
	// +defaultPath="/"
	// +ignore=[".git", ".claude", ".idea", ".scratch", "docs/assets", "dagger"]
	source *dagger.Directory,
) *Demos {
	return &Demos{Source: source}
}

// scenario is the decoded scenario.toml.
type scenario struct {
	// Guide is the docs/guides page the recording illustrates.
	Guide string `toml:"guide"`
	// Workload names a directory under demo/workloads, mounted at /workload
	// in every stage and in the recording container.
	Workload string `toml:"workload"`
	// Packages are extra Debian packages for the recording container.
	Packages []string `toml:"packages"`
	// Env is extra environment for the recording container.
	Env map[string]string `toml:"env"`
	// Stages build or supply the tools the tape runs.
	Stages []stage `toml:"stage"`
}

// stage runs commands in a toolchain image and copies results into the
// recording container. A stage with no commands just lifts files out of an
// image, e.g. a static CLI binary.
type stage struct {
	Image string   `toml:"image"`
	Run   []string `toml:"run"`
	// Copy entries are "source:destination" paths.
	Copy []string `toml:"copy"`
}

// Praytel builds the praytel binary from the repository source.
func (m *Demos) Praytel() *dagger.File {
	return dag.Container().
		From(goImage).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("praytel-gomod")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("praytel-gobuild")).
		WithEnvVariable("CGO_ENABLED", "0").
		WithDirectory("/src", m.Source).
		WithWorkdir("/src").
		WithExec([]string{"go", "build", "-o", "/out/praytel", "./cmd/praytel"}).
		File("/out/praytel")
}

// Scenarios lists the available scenario names.
func (m *Demos) Scenarios(ctx context.Context) ([]string, error) {
	entries, err := m.Source.Directory(scenariosDir).Entries(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e, "/"))
	}
	return names, nil
}

// Record runs one scenario's tape and returns the recordings it wrote.
func (m *Demos) Record(
	ctx context.Context,
	// Scenario directory name under demo/scenarios.
	scenario string,
	// Record again even if nothing changed since the last run.
	// +optional
	fresh bool,
) (*dagger.Directory, error) {
	ctr, err := m.Recorder(ctx, scenario)
	if err != nil {
		return nil, err
	}
	if fresh {
		ctr = ctr.WithEnvVariable("DEMO_RECORDED_AT", time.Now().Format(time.RFC3339Nano))
	}
	return ctr.
		WithExec([]string{"vhs", "scenario.tape"}).
		Directory("/out"), nil
}

// RecordAll runs every scenario and returns all recordings in one directory.
func (m *Demos) RecordAll(
	ctx context.Context,
	// +optional
	fresh bool,
) (*dagger.Directory, error) {
	names, err := m.Scenarios(ctx)
	if err != nil {
		return nil, err
	}
	out := dag.Directory()
	for _, name := range names {
		dir, err := m.Record(ctx, name, fresh)
		if err != nil {
			return nil, err
		}
		out = out.WithDirectory("/", dir)
	}
	return out, nil
}

// Recorder returns the container a scenario is recorded in, ready to run
// its tape. Open a shell in it with `dagger call recorder --scenario X terminal`
// to develop a tape interactively.
func (m *Demos) Recorder(ctx context.Context, scenario string) (*dagger.Container, error) {
	cfg, err := m.config(ctx, scenario)
	if err != nil {
		return nil, err
	}

	packages := append([]string{"tmux"}, cfg.Packages...)
	ctr := dag.Container().
		From(vhsImage).
		WithoutEntrypoint().
		// The image tracks Debian "stable", which has moved on since it was
		// built; pin apt to the release the image actually is.
		WithExec([]string{"sh", "-c", `. /etc/os-release && find /etc/apt -name '*.list' -o -name '*.sources' | xargs sed -i -E "s/\bstable(-updates|-security)?\b/${VERSION_CODENAME}\1/g"`}).
		WithExec([]string{"sh", "-c", "apt-get update && apt-get install -y --no-install-recommends " + strings.Join(packages, " ")}).
		WithFile("/usr/local/bin/praytel", m.Praytel()).
		WithFile("/usr/local/bin/demo-caption", m.Source.File("demo/bin/demo-caption"), dagger.ContainerWithFileOpts{Permissions: 0o755}).
		WithFile("/demo/tmux.conf", m.Source.File("demo/tmux.conf")).
		WithFile("/demo/common.tape", m.Source.File("demo/common.tape"))

	var workload *dagger.Directory
	if cfg.Workload != "" {
		workload = m.Source.Directory(workloadsDir + "/" + cfg.Workload)
		ctr = ctr.WithDirectory("/workload", workload)
	}

	for i, st := range cfg.Stages {
		if st.Image == "" {
			return nil, fmt.Errorf("scenario %s: stage %d has no image", scenario, i)
		}
		build := dag.Container().From(st.Image)
		if workload != nil {
			build = build.WithDirectory("/workload", workload).WithWorkdir("/workload")
		}
		for _, cmd := range st.Run {
			build = build.WithExec([]string{"sh", "-c", cmd})
		}
		for _, c := range st.Copy {
			src, dst, ok := strings.Cut(c, ":")
			if !ok {
				return nil, fmt.Errorf("scenario %s: stage %d: copy %q is not source:destination", scenario, i, c)
			}
			ctr = ctr.WithFile(dst, build.File(src))
		}
	}

	for k, v := range cfg.Env {
		ctr = ctr.WithEnvVariable(k, v)
	}

	return ctr.
		WithDirectory("/scenario", m.Source.Directory(scenariosDir+"/"+scenario)).
		WithEnvVariable("DEMO_CAPTIONS", "/scenario/captions.txt").
		WithWorkdir("/scenario"), nil
}

func (m *Demos) config(ctx context.Context, name string) (scenario, error) {
	var cfg scenario
	raw, err := m.Source.File(scenariosDir + "/" + name + "/scenario.toml").Contents(ctx)
	if err != nil {
		return cfg, fmt.Errorf("scenario %s: %w", name, err)
	}
	if _, err := toml.Decode(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("scenario %s: scenario.toml: %w", name, err)
	}
	return cfg, nil
}
