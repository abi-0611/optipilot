# OptiPilot forecaster convenience targets
# Usage: make help

SHELL := /usr/bin/env bash

UV ?= uv
PY ?= python

# Optional seed parameters:
#   make seed SEED_MINUTES=4320 SEED_SERVICES=api-gateway,order-service SEED_DB=/tmp/forecaster_metrics.db
SEED_MINUTES ?=
SEED_SERVICES ?=
SEED_DB ?=
SEED_SEED ?=

SEED_FLAGS :=
ifneq ($(strip $(SEED_MINUTES)),)
	SEED_FLAGS += --minutes $(SEED_MINUTES)
endif
ifneq ($(strip $(SEED_SERVICES)),)
	SEED_FLAGS += --services $(SEED_SERVICES)
endif
ifneq ($(strip $(SEED_DB)),)
	SEED_FLAGS += --metrics-db $(SEED_DB)
endif
ifneq ($(strip $(SEED_SEED)),)
	SEED_FLAGS += --seed $(SEED_SEED)
endif

.PHONY: help clean clean-dbs clean-models seed proto run

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "%-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

clean: clean-dbs clean-models ## Remove forecaster runtime artifacts (dbs + models)

clean-dbs: ## Remove SQLite databases and WAL/SHM files
	@rm -f \
		forecaster_metrics.db forecaster_metrics.db-wal forecaster_metrics.db-shm \
		forecaster_registry.db forecaster_registry.db-wal forecaster_registry.db-shm

clean-models: ## Remove trained model artifacts
	@rm -rf models

seed: ## Seed synthetic metrics into forecaster_metrics.db (uses scripts/seed_test_data.py)
	@$(UV) run $(PY) scripts/seed_test_data.py --clear-existing $(SEED_FLAGS)

proto: ## Regenerate shared Python proto stubs under ../gen/python
	@bash scripts/sync_proto.sh

run: ## Run the forecaster gRPC server
	@$(UV) run forecaster
