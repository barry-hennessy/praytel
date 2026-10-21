package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dagger/demos/internal/dagger"
)

// benchstatVersion pins golang.org/x/perf/cmd/benchstat, which compares
// benchmark runs.
const benchstatVersion = "v0.0.0-20261009192801-be2c69fb417e"

// BenchDiff runs the benchmarks of base and of the repository source, and
// returns how they compare along with CPU and memory profiles of the
// latter:
//
//	base.txt, head.txt  every run, in go test -bench format
//	diff.txt            benchstat's comparison of the two
//	profiles/           a CPU and memory profile per package, and the top of each
//
// Benchmarks run with -short, so their largest sizes are left out. Both
// sides are built first and then run in turns, a round of every base
// benchmark and then every head one, so a machine that slows down or
// speeds up part way through shows on both sides rather than as a
// difference between them.
//
// The call itself is never cached, so that fresh runs again even when
// asked twice with the same arguments; the run inside it is, by its
// inputs, so without fresh an unchanged comparison is not run twice.
//
// +cache="never"
func (m *Demos) BenchDiff(
	// The source to compare against, such as a checkout of main.
	// +ignore=[".git", ".github", ".claude", ".idea", ".scratch", "docs", "design", "dagger", "Makefile", "**/*.md"]
	base *dagger.Directory,
	// Rounds of every benchmark on each side.
	// +optional
	// +default=6
	count int,
	// Run again even if nothing changed since the last run.
	// +optional
	fresh bool,
) *dagger.Directory {
	return benchCompare([]benchSide{{"base", base}, {"head", m.Source}}, count, fresh)
}

// BenchHistory runs the benchmarks of each of revs, in the same way and in
// the same turns as BenchDiff, and compares every one with the first: a
// baseline, and where each revision after it stands. It returns a <name>.txt
// of runs per revision, diff.txt comparing them, and profiles/ of the last.
//
// +cache="never"
func (m *Demos) BenchHistory(
	// The Go source of each revision to compare, oldest first.
	revs []*dagger.Directory,
	// A name for each revision, such as its commit; by default r0, r1 and so on.
	// +optional
	names []string,
	// Rounds of every benchmark for each revision.
	// +optional
	// +default=6
	count int,
	// Run again even if nothing changed since the last run.
	// +optional
	fresh bool,
) (*dagger.Directory, error) {
	if len(names) != 0 && len(names) != len(revs) {
		return nil, fmt.Errorf("%d names for %d revisions", len(names), len(revs))
	}
	sides := make([]benchSide, len(revs))
	for i, rev := range revs {
		name := fmt.Sprintf("r%d", i)
		if len(names) != 0 {
			name = names[i]
		}
		if name == "" || strings.ContainsAny(name, "/= \t\n") {
			return nil, fmt.Errorf("revision name %q: it names a file, so it can't be empty or hold '/', '=' or spaces", name)
		}
		sides[i] = benchSide{name, rev}
	}
	return benchCompare(sides, count, fresh), nil
}

type benchSide struct {
	name string
	src  *dagger.Directory
}

// benchCompare runs every side's benchmarks in turns, compares them all
// with the first, and profiles the last.
func benchCompare(sides []benchSide, count int, fresh bool) *dagger.Directory {
	ctr := dag.Container().
		From(goImage).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("praytel-gomod")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("praytel-gobuild")).
		WithExec([]string{"go", "install", "golang.org/x/perf/cmd/benchstat@" + benchstatVersion})
	names := make([]string, len(sides))
	for i, s := range sides {
		names[i] = s.name
		ctr = ctr.
			WithDirectory("/sides/"+s.name+"/src", s.src).
			WithDirectory("/sides/"+s.name+"/bin", benchBinaries(s.src))
	}
	ctr = ctr.
		WithEnvVariable("SIDES", strings.Join(names, " ")).
		WithEnvVariable("COUNT", strconv.Itoa(count))
	if fresh {
		ctr = ctr.WithEnvVariable("BENCH_RUN_AT", time.Now().Format(time.RFC3339Nano))
	}
	return ctr.
		WithExec([]string{"sh", "-c", benchScript}).
		Directory("/out")
}

// benchBinaries compiles the test binary of every package in src that has
// benchmarks, as <name>.test, beside a <name>.dir naming the package's
// directory to run it in. Source with no benchmarks gives an empty
// directory.
func benchBinaries(src *dagger.Directory) *dagger.Directory {
	return goContainer(src).
		WithExec([]string{"sh", "-c", `
			set -eu
			mkdir -p /bin-out
			for dir in $(grep -rl --include='*_test.go' '^func Benchmark' . | xargs -r -n1 dirname | sort -u); do
				name=$(echo "${dir#./}" | tr / _)
				go test -c -o "/bin-out/$name.test" "$dir"
				echo "$dir" > "/bin-out/$name.dir"
			done
		`}).
		Directory("/bin-out")
}

// benchScript runs in a container holding, for each of the space-separated
// $SIDES, its source and test binaries under /sides/<side>, and writes what
// benchCompare returns to /out.
const benchScript = `
set -eu
mkdir -p /out/profiles

# run SIDE NAME ARGS... runs one test binary's benchmarks in its package's
# directory, which is where go test would.
run() {
	side=$1 name=$2
	shift 2
	cd "/sides/$side/src/$(cat "/sides/$side/bin/$name.dir")"
	"/sides/$side/bin/$name.test" -test.run '^$' -test.bench . -test.short -test.benchmem "$@"
	cd /
}

for side in $SIDES; do
	: > "/out/$side.txt"
	last=$side
done

i=0
while [ "$i" -lt "$COUNT" ]; do
	for side in $SIDES; do
		for bin in /sides/$side/bin/*.test; do
			[ -e "$bin" ] || continue
			run "$side" "$(basename "$bin" .test)" -test.count 1 >> "/out/$side.txt"
		done
	done
	i=$((i + 1))
done

set --
for side in $SIDES; do
	set -- "$@" "$side=/out/$side.txt"
done
benchstat "$@" > /out/diff.txt

for bin in /sides/$last/bin/*.test; do
	[ -e "$bin" ] || continue
	name=$(basename "$bin" .test)
	prof=/out/profiles/$name
	run "$last" "$name" -test.count 1 -test.cpuprofile "$prof.cpu.prof" -test.memprofile "$prof.mem.prof" > /dev/null
	go tool pprof -top -nodecount 15 "$bin" "$prof.cpu.prof" > "$prof.cpu.txt" 2>/dev/null
	go tool pprof -top -nodecount 15 -sample_index=alloc_space "$bin" "$prof.mem.prof" > "$prof.mem.txt" 2>/dev/null
done
`
