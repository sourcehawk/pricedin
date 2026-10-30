# PricedIn: the one entry point for tools, tests, builds and the local stack.
# CI calls these targets and nothing else. Run `make` for the list. Make
# prints each command before it runs it.

# Optional local settings (host ports, credentials). Compose reads .env too.
-include .env

# --- Tools --------------------------------------------------------------------
# asdf installs the versions in .tool-versions. Another copy of a tool earlier
# on PATH must not win, so prefer the asdf one when asdf knows it.
asdf_which = $(or $(shell asdf which $(1) 2>/dev/null),$(1))

GO            := $(or $(GO),$(call asdf_which,go))
PNPM          := $(or $(PNPM),$(call asdf_which,pnpm))
GOLANGCI_LINT := $(or $(GOLANGCI_LINT),$(call asdf_which,golangci-lint))
ACTIONLINT    := $(or $(ACTIONLINT),$(call asdf_which,actionlint))
SHELLCHECK    := $(or $(SHELLCHECK),$(call asdf_which,shellcheck))
NODE          := $(or $(NODE),$(call asdf_which,node))
DOCKER        ?= docker
COMPOSE       ?= $(DOCKER) compose

# A GOROOT from the shell profile can point at another Go version than the
# one asdf selects, and then every build fails.
unexport GOROOT

# --- Settings -----------------------------------------------------------------
SERVER_DIR := server
PRICE_FIXTURES_DIR := tools/price-fixtures
IMAGE_TAG  ?= dev
SERVER_IMAGE ?= pricedin-server:$(IMAGE_TAG)
WEB_IMAGE    ?= pricedin-web:$(IMAGE_TAG)
FIXTURES_IMAGE ?= pricedin-fixtures:$(IMAGE_TAG)

# The stack against the fixture server, which answers at the SEC and Massive
# paths from testdata, so it never calls the real services.
COMPOSE_FIXTURES = $(COMPOSE) -f docker-compose.yml -f docker-compose.fixtures.yml --profile fixtures
# The jobs that load the fixture companies, in order. --wait makes each one
# wait for the same job of the scheduler instead of skipping it.
SEED_JOBS := sec-tickers sec-bulk price-reference prices-backfill "fundamentals-rebuild all"
# Extra flags for `docker buildx build`, such as the cache flags CI passes.
DOCKER_BUILD_FLAGS ?=
# Extra flags for `playwright install`; CI adds --with-deps.
PLAYWRIGHT_INSTALL_FLAGS ?=

# The end-to-end stack is its own Compose project with its own host ports,
# so it runs beside the stack of `make up` and its teardown never removes
# that stack's database.
PRICEDIN_E2E_PROJECT      ?= pricedin-e2e
PRICEDIN_E2E_WEB_PORT     ?= 3100
PRICEDIN_E2E_API_PORT     ?= 8180
PRICEDIN_E2E_METRICS_PORT ?= 9190
PRICEDIN_E2E_DB_PORT      ?= 5532
PRICEDIN_E2E_BASE_URL     ?= http://localhost:$(PRICEDIN_E2E_WEB_PORT)
PRICEDIN_E2E_API_URL      ?= http://localhost:$(PRICEDIN_E2E_API_PORT)
# The owner account of the end-to-end stack; the tests log in with it.
PRICEDIN_E2E_OWNER_EMAIL    ?= e2e-owner@example.com
PRICEDIN_E2E_OWNER_PASSWORD ?= e2e-owner-password

SHELL_SCRIPTS := $(shell git ls-files --cached --others --exclude-standard '*.sh' 2>/dev/null)

.DEFAULT_GOAL := help

.PHONY: help
help: ## List the targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# --- Setup --------------------------------------------------------------------
.PHONY: tools
tools: plugins ## Install the tool versions in .tool-versions with asdf
	asdf install

.PHONY: plugins
plugins: ## Add the asdf plugin of each tool in .tool-versions that asdf lacks
	installed="$$(asdf plugin list 2>/dev/null || true)"; \
	for plugin in $$(awk '!/^#/ && NF { print $$1 }' .tool-versions); do \
		if ! printf '%s\n' "$$installed" | grep -qx "$$plugin"; then asdf plugin add "$$plugin" || exit 1; fi; \
	done

.PHONY: deps
deps: deps-go deps-node browsers ## Install Go modules, npm packages and the Playwright browser

.PHONY: deps-go
deps-go: ## Download Go modules
	cd $(SERVER_DIR) && $(GO) mod download

.PHONY: deps-node
deps-node: ## Install npm packages from the lockfile
	$(PNPM) install --frozen-lockfile

.PHONY: browsers
browsers: ## Install the Playwright browser (chromium)
	$(PNPM) --filter @pricedin/e2e exec playwright install $(PLAYWRIGHT_INSTALL_FLAGS) chromium

.PHONY: generate
generate: generate-price-fixtures generate-engine-cases generate-api-types ## Regenerate every generated file

.PHONY: generate-price-fixtures
generate-price-fixtures: generate-price-anchors ## Regenerate the synthetic Massive fixtures in testdata/prices/massive
	cd $(PRICE_FIXTURES_DIR) && $(GO) run .

# The anchors tie the synthetic closes to the earnings in the SEC fixtures,
# through the server's quarter derivation.
.PHONY: generate-price-anchors
generate-price-anchors: ## Write the price anchors of tools/price-fixtures from the SEC fixtures
	cd $(SERVER_DIR) && $(GO) run ./cmd/price-anchors

# The oracle runs the prototype's own math and writes the expected values
# that both engines test against.
ENGINE_CASES_DIR := testdata/engine

.PHONY: generate-engine-cases
generate-engine-cases: ## Regenerate the shared engine cases from the prototype oracle
	$(NODE) tools/engine-oracle/generate.mjs $(ENGINE_CASES_DIR)

# The web app's API types come from the OpenAPI document of the server.
OPENAPI_DOC := server/api/openapi.yaml
API_TYPES   := web/src/api/schema.d.ts

.PHONY: generate-api-types
generate-api-types: ## Regenerate the web app's API types from the OpenAPI document
	$(PNPM) --filter @pricedin/web exec openapi-typescript ../$(OPENAPI_DOC) --output ../$(API_TYPES)

# --- Format and lint ----------------------------------------------------------
.PHONY: fmt
fmt: ## Format Go and web code in place
	cd $(SERVER_DIR) && $(GOLANGCI_LINT) fmt ./...
	cd $(PRICE_FIXTURES_DIR) && $(GOLANGCI_LINT) fmt --config ../../$(SERVER_DIR)/.golangci.yml ./...
	$(PNPM) exec prettier --write .

.PHONY: lint
lint: lint-go lint-web lint-actions lint-shell lint-engine-cases lint-api-types ## Run every linter and type check

.PHONY: lint-go
lint-go:
	cd $(SERVER_DIR) && $(GOLANGCI_LINT) run ./...
	cd $(SERVER_DIR) && $(GOLANGCI_LINT) fmt --diff ./...
	cd $(PRICE_FIXTURES_DIR) && $(GOLANGCI_LINT) run --config ../../$(SERVER_DIR)/.golangci.yml ./...
	cd $(PRICE_FIXTURES_DIR) && $(GOLANGCI_LINT) fmt --diff --config ../../$(SERVER_DIR)/.golangci.yml ./...

.PHONY: lint-web
lint-web:
	$(PNPM) -r lint
	$(PNPM) -r typecheck
	$(PNPM) exec prettier --check .

.PHONY: lint-actions
lint-actions:
	$(ACTIONLINT)

# Fails when the committed engine cases differ from a fresh oracle run.
.PHONY: lint-engine-cases
lint-engine-cases:
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && \
	echo "$(NODE) tools/engine-oracle/generate.mjs $$tmp" && \
	$(NODE) tools/engine-oracle/generate.mjs "$$tmp" && \
	diff -q "$$tmp/cases.json" $(ENGINE_CASES_DIR)/cases.json && \
	diff -q "$$tmp/evaluation-example.json" $(ENGINE_CASES_DIR)/evaluation-example.json || \
	{ echo 'lint-engine-cases: the engine cases are stale; run make generate-engine-cases' >&2; exit 1; }

# Fails when the committed API types differ from a fresh generation.
.PHONY: lint-api-types
lint-api-types:
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && \
	echo "$(PNPM) --filter @pricedin/web exec openapi-typescript ../$(OPENAPI_DOC) --output $$tmp/schema.d.ts" && \
	$(PNPM) --filter @pricedin/web exec openapi-typescript ../$(OPENAPI_DOC) --output "$$tmp/schema.d.ts" >/dev/null && \
	diff -q "$$tmp/schema.d.ts" $(API_TYPES) || \
	{ echo 'lint-api-types: the API types are stale; run make generate-api-types' >&2; exit 1; }

.PHONY: lint-shell
lint-shell:
ifneq ($(SHELL_SCRIPTS),)
	$(SHELLCHECK) $(SHELL_SCRIPTS)
else
	@echo 'lint-shell: no shell scripts'
endif

# --- Tests --------------------------------------------------------------------
.PHONY: test
test: test-server test-web test-scripts ## Run the Go, web and shell script unit tests

.PHONY: test-server
test-server: ## Run the Go unit tests with the race detector
	cd $(SERVER_DIR) && $(GO) test -race ./...
	cd $(PRICE_FIXTURES_DIR) && $(GO) test -race ./...

.PHONY: test-scripts
test-scripts: ## Run the tests of the shell scripts in scripts/
	scripts/deploy-gate_test.sh
	scripts/ci-changes_test.sh
	scripts/ci-ok_test.sh

.PHONY: test-integration
test-integration: ## Run the Go integration tests (build tag integration)
	cd $(SERVER_DIR) && $(GO) test -race -tags integration ./...

.PHONY: test-web
test-web: ## Run the web unit tests
	$(PNPM) --filter @pricedin/web test

E2E_ENV = COMPOSE='$(COMPOSE_FIXTURES)' DOCKER='$(DOCKER)' PNPM='$(PNPM)' \
	PRICEDIN_E2E_PROJECT='$(PRICEDIN_E2E_PROJECT)' \
	PRICEDIN_SERVER_IMAGE='$(SERVER_IMAGE)' PRICEDIN_WEB_IMAGE='$(WEB_IMAGE)' \
	PRICEDIN_FIXTURES_IMAGE='$(FIXTURES_IMAGE)' \
	PRICEDIN_WEB_PORT='$(PRICEDIN_E2E_WEB_PORT)' PRICEDIN_API_PORT='$(PRICEDIN_E2E_API_PORT)' \
	PRICEDIN_METRICS_PORT='$(PRICEDIN_E2E_METRICS_PORT)' PRICEDIN_DB_PORT='$(PRICEDIN_E2E_DB_PORT)' \
	PRICEDIN_E2E_BASE_URL='$(PRICEDIN_E2E_BASE_URL)' PRICEDIN_E2E_API_URL='$(PRICEDIN_E2E_API_URL)' \
	PRICEDIN_E2E_OWNER_EMAIL='$(PRICEDIN_E2E_OWNER_EMAIL)' PRICEDIN_E2E_OWNER_PASSWORD='$(PRICEDIN_E2E_OWNER_PASSWORD)'

.PHONY: test-e2e
test-e2e: images ## Start a separate Compose stack against the fixture server, run Playwright, remove that stack
	$(E2E_ENV) scripts/e2e.sh

# For work on the tests: start the stack once, run the tests often, remove it.
.PHONY: e2e-up
e2e-up: images ## Start and seed the end-to-end stack, and keep it
	$(E2E_ENV) scripts/e2e.sh up

.PHONY: e2e-run
e2e-run: ## Run Playwright against the stack of make e2e-up (ARGS=playwright arguments)
	$(E2E_ENV) $(PNPM) --filter @pricedin/e2e exec playwright test $(ARGS)

.PHONY: e2e-down
e2e-down: ## Remove the stack of make e2e-up with its volumes
	$(E2E_ENV) scripts/e2e.sh down

# --- Build --------------------------------------------------------------------
.PHONY: build
build: ## Build the Go binary and the Next.js app
	cd $(SERVER_DIR) && CGO_ENABLED=0 $(GO) build -trimpath -o bin/pricedin ./cmd/pricedin
	$(PNPM) --filter @pricedin/web build

.PHONY: images
images: ## Build the server, fixture server and web Docker images
	$(DOCKER) buildx build --load $(DOCKER_BUILD_FLAGS) --tag $(SERVER_IMAGE) $(SERVER_DIR)
	$(DOCKER) buildx build --load $(DOCKER_BUILD_FLAGS) --tag $(FIXTURES_IMAGE) --target fixtureserver $(SERVER_DIR)
	$(DOCKER) buildx build --load $(DOCKER_BUILD_FLAGS) --tag $(WEB_IMAGE) --file web/Dockerfile .

# --- Local stack --------------------------------------------------------------
.PHONY: up
up: ## Build and start the Compose stack in the background
	PRICEDIN_SERVER_IMAGE='$(SERVER_IMAGE)' PRICEDIN_WEB_IMAGE='$(WEB_IMAGE)' \
	$(COMPOSE) up --detach --build --wait

.PHONY: up-fixtures
up-fixtures: ## Build and start the Compose stack against the fixture server, with the scheduler
	PRICEDIN_SERVER_IMAGE='$(SERVER_IMAGE)' PRICEDIN_WEB_IMAGE='$(WEB_IMAGE)' PRICEDIN_FIXTURES_IMAGE='$(FIXTURES_IMAGE)' \
	$(COMPOSE_FIXTURES) up --detach --build --wait

.PHONY: seed
seed: ## Load the fixture companies into the stack of make up-fixtures
	@for job in $(SEED_JOBS); do \
		echo "+ $(COMPOSE_FIXTURES) exec server /pricedin jobs run --wait $$job"; \
		$(COMPOSE_FIXTURES) exec -T server /pricedin jobs run --wait $$job || exit 1; \
	done

.PHONY: down
down: ## Stop the Compose stack (keeps the database volume)
	$(COMPOSE) down

.PHONY: down-fixtures
down-fixtures: ## Stop the stack of make up-fixtures (keeps the database volume)
	$(COMPOSE_FIXTURES) down

.PHONY: logs
logs: ## Follow the logs of the Compose stack
	$(COMPOSE) logs --follow

# `make migrate MIGRATE_ARGS=status` lists the migrations; `down` rolls one back.
MIGRATE_ARGS ?= up

# The migrate service of the Compose stack runs the command, so the
# database settings come from .env as Compose reads it, and the command
# line that make prints holds no password.
.PHONY: migrate
migrate: ## Run the migrations on the Compose database (MIGRATE_ARGS=status|down)
	$(COMPOSE) run --rm --build migrate migrate $(MIGRATE_ARGS)

# --- CI -----------------------------------------------------------------------
.PHONY: ci
ci: ## Run everything CI runs, in CI order
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) test-integration
	$(MAKE) build
	$(MAKE) images
	$(MAKE) test-e2e
