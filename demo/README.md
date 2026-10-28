# Demo recordings

Each guide in `docs/guides/` has a short recording of a real praytel session.
They are generated, not hand-made: a script drives the TUI, and the same
command produces them on your machine and in CI.

## Record

You need [Dagger](https://docs.dagger.io/install) and a container runtime.
Nothing else is installed on the host.

```sh
make docs                      # every recording that is out of date
make docs/assets/traces.gif    # one recording; its .mp4 is built with it
make docs/assets/traces        # the same, without naming a format
make docs-list                 # the recordings and what they are built from
```

Each recording is a file target. It is rebuilt when its scenario directory,
praytel's source, a workload or the shared recording setup is newer than it,
so `make docs` after editing one tape records only that scenario.

Make compares timestamps; Dagger caches on content. If make reruns a
scenario whose inputs have not really changed, Dagger returns the earlier
recording at once. To get a genuinely new take, add `FRESH=1`:

```sh
make docs/assets/traces.gif FRESH=1
```

The targets are thin wrappers over the Dagger module, which you can also call
directly:

```sh
dagger call record-all export --path docs/assets
dagger call record --scenario traces --fresh export --path docs/assets
```

## Before a release

Recordings are committed by hand, once per release, before it is tagged.
Each one has a stamp next to it, `docs/assets/<name>.inputs`, that `make
docs` writes: git's hash of every file it was recorded from, and of the
recording. Unlike make's timestamps, that holds in a fresh clone and in CI.

```sh
make docs-check                # recordings at HEAD that are out of date
make docs-check REV=v0.3.0     # the same, at any commit
make docs-refresh              # record again whatever is out of date here
```

Turn on the pre-push hook once with `make hooks`. When you push a `v*` tag
whose recordings are out of date, it asks whether to record them again:

- `y` records them, commits them on top of the tagged commit, moves the
  tag onto that commit, and stops the push. Push again to send the moved
  tag. This needs the tag at the tip of the branch you have checked out,
  and no uncommitted changes.
- `N` pushes as it is.

Without a terminal to ask on, it pushes as it is; set `DEMOS_ANSWER=y` or
`n` to answer without one.

CI checks the same when a `v*` tag is pushed (`.github/workflows/demos.yml`),
so a release tagged without the hook, or on GitHub's release page, still
shows up as out of date. On pull requests, CI records them and uploads the
result as an artifact to look at; it never commits them.

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
