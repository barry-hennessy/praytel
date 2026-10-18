---
title: praytel-map(1)
summary: Draw an architecture map, or the difference between two, without running a child command.
weight: 25
---

# praytel-map(1)

## Synopsis

```
praytel map [--format FORMAT] MAP
praytel map [--format FORMAT] OLD NEW
praytel --map DIR [flags] -- command [args ...]
```

## Description

An architecture map is the component graph of a run, written as plain text
files by `praytel --map DIR` (see [praytel(1)](praytel.1.md)). Each `MAP`,
`OLD` or `NEW` argument is one map file or a directory. A directory is read
as every `*.map` file below it, combined.

With one map, `praytel map` draws it. With two, it draws the difference: both
maps laid out together, each component and call marked as added, removed,
changed or unchanged. Only the changes and their immediate neighbours are
drawn; the number of components left out is printed beneath. In text format
every change is listed first as one line each, before the drawing.

No child is run and nothing is read from the network.

## Options

| Option | Default | Meaning |
|---|---|---|
| `--format FORMAT` | `text` | `text`: box drawing, coloured when stdout is a terminal and `NO_COLOR` is unset. `mermaid`: a Mermaid flowchart. |

## Recording a map

`praytel --map DIR -- command` runs the command as usual. After the run
drains, praytel writes one file per entry point (a trace's root span) to
`DIR/SERVICE/NAME.map`, where `SERVICE` is the root span's `service.name` and
`NAME` its span name. Characters other than letters, digits, `_`, `-` and `.`
(and a leading `.`) are written as `%XX`. A file is only rewritten when its
contents change, and files for entry points that did not run are left alone.
Each file that changed is reported on stderr with its added and removed lines.

Nothing is written, and praytel exits non-zero, when the command failed, no
traces arrived, the drain reached `--drain-timeout`, data was shed or evicted,
or a span names a parent that never arrived.

`--map` cannot be combined with `--query`, `--query-file`, `--focus` or
`--exclude`.

## File format

```
praytel-map v1
entry SERVICE NAME

node SERVICE NAME
...

call SERVICE NAME -> SERVICE NAME
...
```

- The first line is the format version.
- `entry` names the root span the file is for.
- One `node` per component: a distinct span name within a service.
- One `call` per pair of different components where a span of the first was
  the parent of a span of the second. A component calling itself is not
  recorded.
- `node` lines are sorted by service then name; `call` lines by caller then
  callee. Fields are compared as strings, byte by byte.
- A field is written bare unless it is empty, contains a space, a `"` or a
  character that is not printable, or is `->`. Then it is written as a Go
  double-quoted string.

There are no counts, durations, IDs or timestamps, so the same behaviour
always produces the same bytes.

## Exit status

With one map, `0`, or `2` if it could not be read. With two, `0` when they are
the same, `1` when they differ, and `2` if either could not be read.

## Examples

```sh
praytel --map arch -- go test -count=1 ./...
praytel map arch
praytel map old/arch arch
praytel map --format mermaid old/arch arch > map-diff.md
```

## See also

[Keep an architecture map](../guides/maps.md),
[praytel(1)](praytel.1.md)
