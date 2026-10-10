# Demo recordings

Each guide in `docs/guides/` has a short recording of a real praytel session.
They are generated, not hand-made: a script drives the TUI, and the same
command produces them on your machine and in CI.

## Record

You need [Dagger](https://docs.dagger.io/install) and a container runtime.
Nothing else is installed on the host.

```sh
make docs                    # every scenario, into docs/assets
make docs SCENARIO=traces    # one scenario
make docs FRESH=1            # record again even if nothing changed
make docs-list               # list the scenarios
```

Each scenario writes `<name>.gif` and `<name>.mp4`. An unchanged scenario is
served from cache unless `FRESH=1` is set.

The targets are thin wrappers over the Dagger module, which you can also call
directly:

```sh
dagger call record-all export --path docs/assets
dagger call record --scenario traces --fresh export --path docs/assets
```

Recordings are committed only when a release is published
(`.github/workflows/demos.yml`). Other CI runs upload them as an artifact.

## How it fits together

| Part | Role |
|---|---|
| [VHS](https://github.com/charmbracelet/vhs) | Runs a `.tape` script in a headless terminal and renders GIF and MP4. |
| tmux | Its status line is the caption bar. |
| `dagger/` | Builds praytel and the workload, assembles the container, runs the tape. |

Captions are part of the terminal rather than added afterwards. A tape presses
`Ctrl+B` then a digit to show that line of `captions.txt` (`Ctrl+B` `0` clears
it), so a caption changes exactly when the tape says, however long the
workload took.

## Layout

```
demo/
  common.tape             shared tape preamble: canvas size, hidden tmux start
  tmux.conf               caption bar and its key bindings
  bin/demo-caption        sets the caption
  scenarios/<name>/
    scenario.toml         guide, workload and toolchain stages
    scenario.tape         the script
    captions.txt          one caption per line
  workloads/<name>/       what praytel wraps
```

## Add a scenario

Add a directory under `scenarios/`. No pipeline code changes.

`scenario.toml` names everything the tape needs that is not already in the
recording container (praytel, tmux, a shell):

```toml
guide = "traces"          # the docs/guides page it illustrates
workload = "shop"         # optional: demo/workloads/<name>, mounted at /workload
packages = ["jq"]         # optional: extra Debian packages
env = { FOO = "bar" }     # optional: extra environment

[[stage]]                 # repeatable
image = "golang:1.26"     # any toolchain image
run = ["go build -o /out/shop ."]             # optional: run in /workload
copy = ["/out/shop:/usr/local/bin/shop"]      # source:destination
```

A stage with no `run` lifts files straight out of an image, which is how to
bring in a self-contained CLI:

```toml
[[stage]]
image = "denoland/deno:bin"
copy = ["/deno:/usr/local/bin/deno"]
```

The recording container is Debian-based, so a copied binary must run there.
A tool that needs its own services (a Docker daemon, a database) needs those
added to the module.

## Write a tape

Start from an existing `scenario.tape`. The rules that keep recordings stable:

- Begin with `Source "/demo/common.tape"` after the `Output` lines.
- Quote `Output` paths; a bare path starting with `/` is read as a pattern.
- Wait on what the screen shows, not on time:
  `Wait+Screen@30s /complete/` waits for the TUI footer to report the run has
  finished. `Sleep` is for giving the viewer time to read.
- Only wait for text that is on screen. A long tree pushes later spans below
  the fold.
- To try things by hand, open a shell in the same container:
  `make docs-shell SCENARIO=traces`.

## Checking a tool before recording it

A workload only shows spans if it exports OTLP and praytel can receive them.
Praytel hands the child a gRPC endpoint in `OTEL_EXPORTER_OTLP_ENDPOINT` with
`OTEL_EXPORTER_OTLP_PROTOCOL=grpc`. A tool that needs a switch to enable
tracing, or speaks only OTLP over HTTP, needs that handled in the command the
tape runs. Run it under `praytel --tui` in the recorder shell first.
