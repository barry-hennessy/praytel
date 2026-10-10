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

DAGGER ?= dagger
ASSETS ?= docs/assets
FRESH  ?=

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
