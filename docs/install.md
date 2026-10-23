---
title: Install praytel
summary: How to install praytel with Go, build it from source, update it and remove it.
weight: 5
---

# Install praytel

Praytel is a single Go binary with no runtime dependencies: no daemon, no
collector, nothing to configure. Installing it needs Go 1.26 or later
([go.dev/dl](https://go.dev/dl/)).

## With `go install`

```sh
go install github.com/barry-hennessy/praytel/cmd/praytel@latest
```

The binary lands in `$(go env GOBIN)`, or `$(go env GOPATH)/bin` when `GOBIN`
is unset. Make sure that directory is on your `PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

To pin a version, replace `@latest` with a tag such as `@v0.2.0`.

## From source

```sh
git clone https://github.com/barry-hennessy/praytel.git
cd praytel
go install ./cmd/praytel
```

Install from a checkout when you also want the example apps the
[guides](index.md#guides) use, such as `shop`:

```sh
(cd demo/workloads/shop && go install .)
```

## Check it works

```sh
praytel --help
```

prints the usage line and flags. To see it do something, wrap any command:

```sh
praytel -- echo hello
```

`hello` comes back on a line tagged with the stream it was written to.
`echo` exports no traces, so the line isn't joined to a span. Praytel exits with
the command's exit code. The [first guide](guides/traces.md) has commands that
do produce traces.

If praytel exits with `bind: address already in use`, something else, often a
local OpenTelemetry collector, already listens on the default OTLP ports 4317
and 4318. Pick others:

```sh
praytel --grpc-port 14317 --http-port 14318 -- echo hello
```

## Update and remove

Run the same `go install` command again to update. To remove praytel, delete
the binary:

```sh
rm "$(go env GOPATH)/bin/praytel"
```

Praytel writes no config, cache or state files, so there is nothing else to
clean up.
