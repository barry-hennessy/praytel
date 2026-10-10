# Documentation targets. The recordings embedded in docs/guides are generated
# by the Dagger module in dagger/; see demo/README.md.
#
#   make docs                    record every scenario into docs/assets
#   make docs SCENARIO=traces    record one scenario
#   make docs FRESH=1            record again even if nothing changed
#   make docs-list               list the scenarios
#   make docs-shell SCENARIO=traces   open a shell in a scenario's container

DAGGER   ?= dagger
ASSETS   ?= docs/assets
SCENARIO ?=
FRESH    ?=

fresh_flag := $(if $(FRESH),--fresh,)

.PHONY: docs docs-list docs-shell

docs:
ifeq ($(SCENARIO),)
	$(DAGGER) call record-all $(fresh_flag) export --path $(ASSETS)
else
	$(DAGGER) call record --scenario $(SCENARIO) $(fresh_flag) export --path $(ASSETS)
endif

docs-list:
	@$(DAGGER) call scenarios

docs-shell:
	@test -n "$(SCENARIO)" || { echo "usage: make docs-shell SCENARIO=<name>"; exit 2; }
	$(DAGGER) call recorder --scenario $(SCENARIO) terminal
