---
title: Keep a golden of your tests' traces
summary: Stamp each test's trace tree and graph as a golden file, review it in pull requests, and check it in CI. Praytel does this for its own tests.
weight: 40
---

# Keep a golden of your tests' traces

A passing test says the answer was right. A golden of its trace tree says
which components it called to get there, with what data, and how much of that
is traced. Committed next to the test, it is reviewable: a pull request that
changes what a test touches shows up as a diff.

Praytel does this for its own tests. This guide is that setup, in shell.

## 1. Export spans from the tests

`--snapshot` can only show spans the tests emit. Praytel sets
`OTEL_EXPORTER_OTLP_ENDPOINT` for the command it runs, so the test binary needs
an OpenTelemetry exporter that reads it, and must flush before it exits.

Praytel's is `internal/testtrace`. It does nothing when the variable is unset,
so a plain `go test` is unaffected:

```go
func TestMain(m *testing.M) { testtrace.Main(m) } // exports, and flushes on exit

lt := testtrace.LoggingTracer(t) // spans from the code under test
ctx := testtrace.Context(t)      // roots them under test.<Name>
```

## 2. Stamp one golden per test

`scripts/golden` lists a package's tests with `go test -list`, runs each one
under its own praytel, in parallel, and writes `testdata/<Test>.golden`:

```sh
make golden                        # every instrumented package
make golden PKGS=internal/receiver # one package
```

Each praytel binds free ports (`--grpc-port=0 --http-port=0`), so runs do not
collide. A test that fails, or emits no spans, writes no golden and the script
fails. Which attributes appear is chosen once, in `scripts/golden.attrs`; keep
out anything that changes between runs (ports, temp paths, ids).

## 3. Check in CI

`make golden-check` stamps again in a container and prints what differs from
the committed goldens. It runs on every pull request as the `golden` job in
`check.yml`, and it never fails the run: a difference is for a person to read.
Instead the job comments the differences on the pull request, a header and the
diff, and keeps that comment up to date on every push. When the differences are
gone it edits the comment to say so; when there never were any it posts nothing.
The same report is in the run's summary.

To accept a change, run `make golden` and commit. A golden that differs between
runs of the same code is a test whose trace depends on timing, such as one that
calls a traced function in a loop it sleeps in. `GOMAXPROCS=1 make golden`, then
a diff, finds them.
