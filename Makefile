# Documentation targets. The recordings embedded in docs/guides are files
# built from the scenarios in demo/scenarios by the Dagger module in dagger/;
# see demo/README.md.
#
#   make docs                      build every recording that is out of date
#   make docs/assets/traces.gif    build one recording (its .mp4 comes with it)
#   make docs/assets/traces        the same, without naming a format
#   make docs FRESH=1              record again even if nothing changed
#   make docs-list                 list the recordings and what they are built from
#   make docs-shell SCENARIO=traces    open a shell in a scenario's container
#
# Benchmark targets. bench-diff runs in the same Dagger module, so CI runs
# exactly what you run locally; its results land in bench-out/.
#
#   make bench                     run the benchmarks here, quickly (-short)
#   make bench-diff                compare the benchmarks with origin/main's, with profiles
#   make bench-diff BASE=HEAD~1    compare with any git ref
#   make bench-diff COUNT=10       more rounds each side, for a steadier comparison
#   make bench-diff FRESH=1        run again even if nothing changed
#   make bench-history REFS="v0.2.0 HEAD~1 HEAD"
#                                  benchmark each ref in the same run, each against the first
#   make bench-report              print the pull request comment for the last bench-diff
#   GITHUB_TOKEN=$(gh auth token) make bench-comment PR=12
#                                  post it to pull request 12, or update the one posted before
#
# Check targets, also run in the Dagger module, by CI on every pull request.
#
#   make lint                      golangci-lint with .golangci.yml, gosec included

DAGGER ?= dagger
ASSETS ?= docs/assets
FRESH  ?=
BASE   ?= origin/main
COUNT  ?=
BENCH  ?= bench-out
REFS   ?=
PR     ?=
REPO   ?=
DETAILS ?=

SCENARIOS  := $(notdir $(wildcard demo/scenarios/*))
RECORDINGS := $(foreach s,$(SCENARIOS),$(ASSETS)/$(s).gif $(ASSETS)/$(s).mp4)

# Everything every recording depends on: praytel itself, the shared recording
# setup, the workloads and the pipeline.
COMMON := go.mod go.sum dagger.json dagger/main.go \
	demo/common.tape demo/tmux.conf demo/bin/demo-caption \
	$(shell find cmd internal demo/workloads -type f -not -name '*.md')

fresh_flag := $(if $(FRESH),--fresh,)

.PHONY: docs docs-list docs-shell $(FRESH:%=FORCE)
FORCE:

docs: $(RECORDINGS)

# One scenario run writes both formats. A recording is rebuilt when its
# scenario directory or anything in COMMON is newer than it.
.SECONDEXPANSION:
$(ASSETS)/%.gif $(ASSETS)/%.mp4: $$(wildcard demo/scenarios/$$*/*) $(COMMON) $(if $(FRESH),FORCE)
	$(DAGGER) call record --scenario $* $(fresh_flag) export --path $(ASSETS)
	@touch $(ASSETS)/$*.gif $(ASSETS)/$*.mp4

# docs/assets/<name> is shorthand for both of that scenario's files.
SHORTHANDS := $(SCENARIOS:%=$(ASSETS)/%)
.PHONY: $(SHORTHANDS)
$(SHORTHANDS): $(ASSETS)/%: $(ASSETS)/%.gif $(ASSETS)/%.mp4

docs-list:
	@for s in $(SCENARIOS); do echo "$(ASSETS)/$$s.gif  <-  demo/scenarios/$$s"; done

docs-shell:
	@test -n "$(SCENARIO)" || { echo "usage: make docs-shell SCENARIO=<name>"; exit 2; }
	$(DAGGER) call recorder --scenario $(SCENARIO) terminal

.PHONY: bench bench-diff bench-history bench-report bench-comment

bench:
	go test -short -run '^$$' -bench . -benchmem ./...

# BASE is checked out into a temporary worktree, so the comparison is
# against that commit as committed, whatever the state of this checkout.
bench-diff:
	@set -e; \
	tmp=$$(mktemp -d); \
	trap 'git worktree remove --force "$$tmp/base" 2>/dev/null; rm -rf "$$tmp"' EXIT; \
	git worktree add --quiet --detach "$$tmp/base" $(BASE); \
	rm -rf $(BENCH); \
	$(DAGGER) call bench-diff --base "$$tmp/base" $(if $(COUNT),--count $(COUNT),) $(fresh_flag) export --path $(BENCH); \
	dirty=$$(test -z "$$(git status --porcelain --untracked-files=no)" || echo ' with uncommitted changes'); \
	echo "$$(git rev-parse --short HEAD)$$dirty against $(BASE) ($$(git rev-parse --short $(BASE)))" > $(BENCH)/title.txt; \
	cat $(BENCH)/diff.txt

# The comment is made from the last bench-diff's results in BENCH.
bench_comment_args = --results $(BENCH) --title "$$(cat $(BENCH)/title.txt)" $(if $(DETAILS),--details-url "$(DETAILS)",)

bench-report:
	@test -f $(BENCH)/title.txt || { echo 'no results in $(BENCH): run make bench-diff first'; exit 2; }
	@$(DAGGER) call bench-report $(bench_comment_args)

# REPO defaults to origin's owner/name. The token is read from GITHUB_TOKEN
# by Dagger as a secret, so it never appears in a command line or a log.
bench-comment:
	@test -n "$(PR)" || { echo 'usage: GITHUB_TOKEN=$$(gh auth token) make bench-comment PR=<number>'; exit 2; }
	@test -n "$$GITHUB_TOKEN" || { echo 'bench-comment needs GITHUB_TOKEN; locally: GITHUB_TOKEN=$$(gh auth token) make bench-comment PR=$(PR)'; exit 2; }
	@test -f $(BENCH)/title.txt || { echo 'no results in $(BENCH): run make bench-diff first'; exit 2; }
	@set -e; \
	repo="$(REPO)"; \
	if [ -z "$$repo" ]; then \
		repo=$$(git remote get-url origin | sed -E -e 's#\.git$$##' -e 's#^.*[:/]([^/:]+/[^/]+)$$#\1#'); \
	fi; \
	$(DAGGER) call bench-comment $(bench_comment_args) --repo "$$repo" --pr $(PR) --token env:GITHUB_TOKEN

# Each of REFS is exported with git archive -- just what the benchmarks
# build from -- and named by its short commit hash.
bench-history:
	@test -n "$(REFS)" || { echo 'usage: make bench-history REFS="<oldest ref> ... <newest ref>"'; exit 2; }
	@set -e; \
	tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	revs=; names=; \
	for ref in $(REFS); do \
		name=$$(git rev-parse --short "$$ref"); \
		mkdir "$$tmp/$$name"; \
		git archive "$$ref" cmd internal go.mod go.sum | tar -x -C "$$tmp/$$name"; \
		revs="$$revs$${revs:+,}$$tmp/$$name"; \
		names="$$names$${names:+,}$$name"; \
	done; \
	rm -rf $(BENCH); \
	$(DAGGER) call bench-history --revs "$$revs" --names "$$names" $(if $(COUNT),--count $(COUNT),) $(fresh_flag) export --path $(BENCH); \
	cat $(BENCH)/diff.txt

.PHONY: lint

lint:
	$(DAGGER) call lint
