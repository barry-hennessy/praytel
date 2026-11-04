# Contributing

## Writing the docs

Everything under `docs/` is plain CommonMark with relative `.md` links, so it
reads in a forge, an editor or `less`. Each file starts with three frontmatter
fields: `title`, `summary` (one sentence) and `weight` (sort order within its
directory). Prose goes in `docs/guides/`; `docs/reference/` holds one file per
command or configuration surface, named after its manual page.

The recordings in the guides live in `docs/assets/` and are generated from
scripts by `make docs`; see [demo/README.md](demo/README.md). They are refreshed
by hand before a release is tagged; `make hooks` turns on a pre-push hook that
offers to do it when a release tag's recordings are out of date.

## Tracing

Praytel's tests are read as traces, and their trace trees are kept as goldens
(`make golden`; see `docs/guides/golden-traces.md`). So every exported function
and method starts a span named `package.Struct.Function` (`package.Function`
for a plain function), including exported methods on unexported types:

```go
ctx, log, span := s.lt.Name("receiver.ingestTraces.Export").Start(ctx)
defer span.End()
```

Pass the caller's context so the span nests; use `context.Background()` only
when the function takes none. `internal/receiver` follows the rule; the other
packages are still to be brought in line.
