---
title: praytel-query-language(7)
summary: The selectors, fields, operators and views of praytel's typed query language.
weight: 50
---

# praytel-query-language(7)

## Shape

```
query     = expr [ "|" view ]
expr      = selector | expr op expr | "(" expr ")"
selector  = ( "span" | "log" ) "{" predicate "}"
predicate = field cmp value | "exists(" field ")" | "!" predicate
          | predicate ( "&&" | "||" ) predicate | "(" predicate ")"
view      = "path" | "matches" | "trace"
```

This is an informal sketch; `praytel query check` is the authority. A query is
evaluated independently against each trace.

## Selectors

`span{ ... }` matches spans and `log{ ... }` matches logs. Every predicate
inside one selector must hold for the same record.

## Combining selectors

| Operator | Meaning |
|---|---|
| `A && B` | The same trace contains evidence for both. |
| `A \|\| B` | The same trace contains evidence for either. |
| `A > B` | `B` is a direct child of `A`. |
| `A >> B` | `B` is a descendant of `A`. |

Structural operators use the final log-to-span assignment, so a log counts as a
child of the span it is attached to.

## Fields

### Intrinsics

| Field | Type | Valid in |
|---|---|---|
| `span:name` | string | `span` |
| `span:kind` | string | `span` |
| `span:status` | string | `span` |
| `span:status_message` | string | `span` |
| `span:duration` | duration | `span` |
| `log:body` | string | `log` |
| `log:source` | string (`stdout`, `stderr` or `otlp`) | `log` |
| `log:severity_number` | integer | `log` |
| `log:severity_text` | string | `log` |
| `trace:id` | string | `span`, `log` |
| `trace:duration` | duration | `span`, `log` |

Using a `log:` intrinsic in a span selector, or a `span:` intrinsic in a log
selector, is a check error.

### Attributes

Attribute fields keep their OpenTelemetry keys.

| Form | Looks up |
|---|---|
| `resource.KEY` | A resource attribute, such as `resource.service.name`. |
| `scope.name`, `scope.version` | The instrumentation scope's name and version. |
| `scope.KEY` | An instrumentation scope attribute. |
| `span.KEY` or `KEY` in a span selector | A span attribute. |
| `log.KEY` or `KEY` in a log selector | A log attribute. |

## Comparisons

| Operator | Meaning |
|---|---|
| `=`, `!=` | Equal, not equal. |
| `>`, `>=`, `<`, `<=` | Ordering. |
| `*=` | Literal substring match. |
| `=~`, `!~` | Fully anchored Go regular expression match, and its negation. |
| `exists(field)` | The field is present. |
| `!` | Negate a predicate. |

Comparisons preserve telemetry types and never coerce. A missing field does not
satisfy `!=`; use `!exists(field)` to select absence.

## Values

| Literal | Example |
|---|---|
| Quoted string | `"connection refused"` |
| Bare word | `error`, `client` |
| Integer | `17` |
| Float | `0.5` |
| Boolean | `true`, `false` |
| Duration | `100ms`, `2s` |

Duration literals compare with duration intrinsics, which are held as
nanoseconds.

## Views

A view controls which records are selected for output. It does not change
which traces match.

| View | Selects |
|---|---|
| `\| path` | Evidence spans, their ancestors and descendants, and the logs attached to those spans. The default. |
| `\| matches` | Only the records that directly satisfy a selector. |
| `\| trace` | Every span and log in a matching trace. |

## Examples

```
span{ resource.service.name = "checkout" && span:status = error }
log{ log:body *= "connection refused" && log:severity_number >= 17 }
span{ resource.service.name = "frontend" } >> span{ span:duration > 100ms }
log{ log:body *= "retry" && log:severity_number >= 17 } && span{ span:kind = client } | path
```

## See also

[Typed queries](../guides/queries.md),
[praytel-query(1)](praytel-query.1.md)
