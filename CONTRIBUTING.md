# Contributing

## Writing the docs

Everything under `docs/` is plain CommonMark with relative `.md` links, so it
reads in a forge, an editor or `less`. Each file starts with three frontmatter
fields: `title`, `summary` (one sentence) and `weight` (sort order within its
directory). Prose goes in `docs/guides/`; `docs/reference/` holds one file per
command or configuration surface, named after its manual page.

The recordings in the guides live in `docs/assets/` and are generated from
scripts by `make docs`; see [demo/README.md](demo/README.md). They are refreshed
when a release is published.
