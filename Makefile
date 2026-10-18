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
	$(shell find cmd internal demo/workloads -type f -not -name '*.md' -not -name '*.map')

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

# Architecture map targets. The demo shop's tests are mapped by the praytel
# built from this checkout, and the map is committed in SHOP_ARCH; CI fails
# when it is out of date. See docs/guides/maps.md.
#
#   make map                       stamp the shop's map from scratch
#   make map-diff                  draw how it differs from main's
#   make map-diff BASE=v0.5.0 MAP_FORMAT=mermaid

SHOP       := demo/workloads/shop
SHOP_ARCH  := $(SHOP)/arch
BUILD      := $(CURDIR)/.build
PRAYTEL    := $(BUILD)/praytel
BASE       ?= origin/main
MAP_FORMAT ?= text

.PHONY: map map-diff $(PRAYTEL)

$(PRAYTEL):
	go build -o $@ ./cmd/praytel

# The gift-card test fails on purpose (it is what the demos find), and
# praytel won't map a failing run, so it is left out.
map: $(PRAYTEL)
	rm -rf $(SHOP_ARCH)
	cd $(SHOP) && $(PRAYTEL) --map arch -- go test -count=1 -skip TestGiftCard .

# Exits 0 whether or not the maps differ: this target is for looking.
map-diff: $(PRAYTEL)
	@rm -rf $(BUILD)/base && mkdir -p $(BUILD)/base/$(SHOP_ARCH)
	@git archive $(BASE) $(SHOP_ARCH) 2>/dev/null | tar -x -C $(BUILD)/base 2>/dev/null || true
	@$(PRAYTEL) map --format $(MAP_FORMAT) $(BUILD)/base/$(SHOP_ARCH) $(SHOP_ARCH); test $$? -le 1
