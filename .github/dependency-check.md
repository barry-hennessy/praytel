Weekly check of the dependencies Dependabot can't update: the ones pinned in Go constants, TOML and the Dagger setup action. Dependabot keeps `go.mod` (both modules) and the workflows' actions up to date itself. Tick what's done or up to date, and close this when it's all ticked; next week's opens a new one.

Find the newest tag of an image, and the digest to pin:

```sh
docker buildx imagetools inspect <image>:<tag> | sed -n 3p
```

## Images

Pin each as `<image>:<tag>@sha256:<digest>`. A tag's digest moves when it is rebuilt, so check the digest even when the tag hasn't changed.

- [ ] `golang:1.26`: `goImage` in `dagger/main.go` and `image` in each `demo/scenarios/*/scenario.toml`, all the same. A new digest is how praytel gets each Go patch release.
- [ ] `ghcr.io/charmbracelet/vhs`: `vhsImage` in `dagger/main.go`
- [ ] `golangci/golangci-lint`: `golangciImage` in `dagger/check.go`
- [ ] `ghcr.io/zizmorcore/zizmor`: `zizmorImage` in `dagger/check.go`

## Tools

- [ ] govulncheck: `govulncheckVersion` in `dagger/check.go` ([releases](https://github.com/golang/vuln/tags))
- [ ] benchstat: `benchstatVersion` in `dagger/bench.go` (`go list -m golang.org/x/perf@latest`)

## Dagger

- [ ] Dagger itself ([releases](https://github.com/dagger/dagger/releases)). To move to a new version, change all three together:
  - `engineVersion` in `dagger.json`
  - `DAGGER_VERSION` and `DAGGER_SHA256` in `.github/actions/setup-dagger/action.yml`, with the SHA-256 from `curl -fsSL https://dl.dagger.io/dagger/releases/<version>/checksums.txt | grep linux_amd64`
  - then `dagger develop`, which regenerates the SDK and its pinned dependencies in `dagger/go.mod`

## Then

- [ ] `make lint vulncheck zizmor` pass with the new versions
- [ ] Open Dependabot pull requests reviewed and merged, or closed with a reason
- [ ] Security tab: no new Dependabot, code scanning or Scorecard alerts left unexplained
