# Opinion: test-definition format (YAML/config vs code vs embedded DSL)

Retrieved 2026-11-08. Coverage is thin. Searches found general config-vs-code opinion, not opinion about trace-test formats specifically. Search-tool summaries were used for several items and are marked.

## Findings by implementation/tool

- **YAML as a de facto programming language (CI pipelines):** negative. Once config needs branching or ordering, "you are coding" in an ill-defined language. Semantics stay hidden (Travis stage inheritance gives indeterminate output) and GitHub Actions versions get hard to read. Maintainer-of-a-competing-tool bias: Earthly sells an alternative. MODERATE (Earthly post plus a CircleCI feature request and talks surfaced by search, which are consistent) · 2023-08-02 · https://earthly.dev/blog/dont-configure-control-flow · https://ideas.circleci.com/config/p/provide-a-circleci-dsl-alternative-to-yaml
- **CircleCI's response to the "give us a DSL" request:** it kept declarative YAML. It argued that a full DSL in a language like JavaScript "would create a new category of complications and debugging", and it offered parameters, reuse and dynamic setup instead. This is the main counterweight on the embedded-language side. Paraphrased from a search summary; I did not read the page. ANECDOTE · undated · https://ideas.circleci.com/config/p/provide-a-circleci-dsl-alternative-to-yaml
- **Tracetest (YAML tests with selector plus assertion lists):** I found no user complaints about the YAML format. Docs show specs as a span selector plus a list of assertions. A search summary noted inconsistent examples across docs and the API schema (plain-string assertions in one place, structured fields in another). That is a doc-inconsistency observation, not a user report. ANECDOTE (UNVERIFIED, from a search summary) · https://docs.tracetest.io/cli/creating-test-specifications · https://github.com/kubeshop/tracetest/blob/d8d1ba8790e41066a096e6ea614863baa4816db8/api/tests.yaml
- **Starlark in Tilt (Tiltfile):** positive, from the vendor and one user. Chosen because it is "more flexible than YAML but easier for your team to maintain than Bash". Docs say it supports conditionals, loops and functions. A Medium comparison calls Skaffold's YAML less flexible. Tilt still accepts raw YAML for manifests through `k8s_yaml()`, so it uses a hybrid. MODERATE (vendor blog and docs, plus one comparison post) · 2018-12-05 (blog, STALE-ish but the language is stable) · https://blog.tilt.dev/2018/12/05/tilt-is-the-start-sh-script-of-my-dreams.html · https://docs.tilt.dev/controlloop.html · https://medium.com/swlh/local-developer-ci-cd-with-tilt-ed7b57b9efb2
- **Starlark generally (learning curve):** the project states its goals as a small language, Python-like syntax, and a lower learning curve. It lets a host add domain functions and types. https://github.com/bazelbuild/starlark (the stated goals came via a search summary of the project description). Secondary aggregator pages list a limited stdlib and little adoption outside Google. ANECDOTE, low-quality sources · https://best-of-web.builder.io/library/google/skylark
- **Starlark / config-language debuggability and termination:** a mixed forum thread (Reddit/HN mirrors) raised several concerns.
  - A buggy loop or runaway recursion "grinds to a halt" with poor debugging.
  - Even a non-Turing-complete language may need timeouts.
  - It is unclear to users how Starlark relates to Python.

  The commenters are unverified and the mirrors are secondary. ANECDOTE · various, undated · https://redlib.librenode.com/r/programming/comments/ls6tgm/intercal_yaml_and_other_horrible_programming/goq5ui8/?context=3 · https://beepb00p.xyz/configs-suck.html
- **CEL (embedded expression language):** the pain points are runtime errors such as "no such key", type mismatches, and the need to guard optional fields with `has()`. A policy can be created and still not work as expected, because errors surface late. The advice is to split expressions and test each one. Source is Kyverno troubleshooting docs plus third-party write-ups (search summary). This is the closest analogue to "assert on an absent span attribute". MODERATE-to-ANECDOTE · undated · https://main.kyverno.io/docs/troubleshooting/cel-expressions
- **Hurl (plain-file, not YAML, request-and-assert format):** one LogRocket review says it has a "steep learning curve" for beginners. ANECDOTE · https://www.plushcap.com/content/logrocket/blog/logrocket-exploring-hurl-a-command-line-alternative-to-postman
- **Postman to k6 (GUI/declarative to JS code):** Grafana's guide recommends rewriting the auto-converted JavaScript for maintainability. The comparison pages are vendor-adjacent and the methodology is not stated. ANECDOTE · 2020-04-19 · https://grafana.com/blog/2020/04/19/load-testing-your-api-with-postman/
- **Alternatives that generate config (Pkl, Dhall, YAMLScript):** suggested as a middle path. Krycho's post suggests Pkl or Dhall, which emit YAML or JSON. ANECDOTE · https://v5.chriskrycho.com/notes/an-ice-cold-take-on-ci-config-systems/ · https://yamlscript.org/cicd/

## Dissent

- Pro-code or embedded language: Earthly, the Tilt authors and the Tilt user post. They argue that flexibility and real control flow beat declarative config. https://earthly.dev/blog/dont-configure-control-flow
- Pro-declarative: CircleCI (a full DSL brings debugging complications) and a forum minority who say declarative configs should not be Turing-complete and Python subsets should not be invented. https://ideas.circleci.com/config/p/provide-a-circleci-dsl-alternative-to-yaml · https://redlib.librenode.com/r/programming/comments/ls6tgm/intercal_yaml_and_other_horrible_programming/goq5ui8/?context=3
- Most sources are vendors arguing for their own product's approach. No neutral measured data was found.

## What I could not find

- Any user report about Tracetest's YAML tests in particular (flakiness, authoring pain, or migration away). The one Tracetest issue I fetched, #2143, was about install and not format (https://github.com/kubeshop/tracetest/issues/2143).
- First-hand Starlark-in-Go embedding post-mortems: sandboxing, step limits, and error-message quality for end-users. These are directly relevant, since praytel would host Starlark. go.starlark.net docs should be checked as primary source.
- Opinion on Cue, Jsonnet or Lua as assertion languages.
- Direct Hacker News threads. Search returned only mirrors, so comment attributions are UNVERIFIED.
- Anything on generated-test-file readability (record-and-codegen tools such as Playwright codegen). This is a worthwhile follow-up lens.
