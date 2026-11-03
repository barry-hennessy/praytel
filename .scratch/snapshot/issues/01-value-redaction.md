# Redact unstable attribute values in snapshots

**Status:** open  
**Type:** AFK

## Parent

`--snapshot` golden trace output (design agreed in conversation; no PRD yet).

## What to build

`--snapshot` v1 prints attribute values exactly as received. Values that vary
between runs (ports, temp paths, ids, timestamps) therefore make a golden
unstable unless the caller hides the whole attribute with `--snapshot-attrs`.

Add value redaction: rewrite known-unstable substrings in attribute values to
fixed tokens, so an attribute stays visible (and reviewable) while its volatile
part does not churn the golden. Applies to attribute values in the snapshot
only, never to the live stream or TUI.

Candidate rewrites, each shown in the output as a token so a reviewer can see
something was masked:

- loopback `host:port` (`127.0.0.1:54321`, `[::1]:54321`, `localhost:54321`) → `<port>`
- temp-dir paths (`/tmp/...`, `/var/folders/...`, `$TMPDIR`) → `<tmp>`
- UUIDs and long hex ids → `<id>`
- RFC 3339 timestamps → `<time>`

## Open questions

- Always on, or opt-in/opt-out via a flag (for example `--snapshot-redact`)?
  v1 decision: off, redaction deferred; whoever hits this uses
  `--snapshot-attrs="-key"` to drop the attribute.
- Extensible rules (user-supplied regexes), or a fixed built-in set?
- Does a redaction-rule change need to be visible in the golden's version header?
- Interaction with `--snapshot-attrs`: redact only attributes that survive the
  filter (the obvious answer), and sort order must not depend on the redacted
  value.

## Acceptance criteria

- [ ] Each built-in rewrite has table tests, including near-misses that must not match
- [ ] Output with redaction on is byte-identical across runs that differ only in the redacted values
- [ ] Redaction runs after attribute filtering and before layout
- [ ] Documented in the reference page, with the token list

## Blocked by

- `--snapshot` core (canonicaliser, `Snapshot()`, flags): not yet ticketed
