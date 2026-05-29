.PHONY: build test clean generate sync-specs sync-spec sync-platform-specs install lint lint-dead verify-generated verify-platform-specs verify-site verify-site-output smoke smoke-seed smoke-cleanup release-check site

# Build variables
VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE            ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
SPEC_PRO_VERSION := $(shell cat specs/.spec-version 2>/dev/null | tr -d '[:space:]' || echo "unknown")
LDFLAGS         := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.specProVersion=$(SPEC_PRO_VERSION)"

# Default target
all: build

# Build the CLI
build:
	go build $(LDFLAGS) -o bin/jamf-cli ./cmd/jamf-cli

# Install locally
install:
	go install $(LDFLAGS) ./cmd/jamf-cli

# Run tests (with race detection for concurrent code)
test:
	go test -race -v ./...

# Run tests with coverage
test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Lint
lint:
	golangci-lint run

# Detect dead Cobra flag bindings and unexported helpers (warn-only for now)
lint-dead:
	go run ./scripts/lint-dead-code/

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Path to jamf-pro-server repo (override with: make sync-specs JAMF_SERVER_PATH=/path/to/repo)
# Specs are scattered across module directories under jamf-pro-server/.
JAMF_SERVER_PATH ?= ../jamf-pro-server
JAMF_SERVER_ROOT := $(JAMF_SERVER_PATH)/jamf-pro-server

# Sync OpenAPI specs from jamf-pro-server repo and regenerate commands
sync-specs:
	@if [ ! -d "$(JAMF_SERVER_ROOT)" ]; then \
		echo "Error: jamf-pro-server not found at $(JAMF_SERVER_PATH)"; \
		echo "Expected $(JAMF_SERVER_ROOT) to exist."; \
		echo "Usage: make sync-specs JAMF_SERVER_PATH=/path/to/jss"; \
		exit 1; \
	fi
	@echo "Syncing OpenAPI specs from $(JAMF_SERVER_ROOT)..."
	@rm -f specs/*.yaml
	@find $(JAMF_SERVER_ROOT) -path "*/swagger_docs/uapi/*.yaml" \
		-not -path "*/uapi/hiddenapi/*" -not -path "*/uapi/common/*" \
		-exec cp {} specs/ +
	@find $(JAMF_SERVER_ROOT) -path "*/swagger_docs/uapi/common/*.yaml" \
		-exec cp {} specs/ +
	@# Normalise upstream naming where the spec filename doesn't match its paths.
	@# MobileDeviceExtensionAttribute.yaml upstream holds only the legacy preview
	@# endpoint (/devices/extensionAttributes — tag mobile-device-extension-attributes-preview)
	@# which returns just names and is redundant with the full /v1 CRUD. Drop it
	@# and rename DeviceExtensionAttribute.yaml (which actually contains the
	@# /v1/mobile-device-extension-attributes CRUD) into its place so the parser
	@# derives the correct resource name from the filename.
	@if [ -f specs/DeviceExtensionAttribute.yaml ]; then \
		rm -f specs/MobileDeviceExtensionAttribute.yaml; \
		mv specs/DeviceExtensionAttribute.yaml specs/MobileDeviceExtensionAttribute.yaml; \
		echo "  Renamed DeviceExtensionAttribute.yaml → MobileDeviceExtensionAttribute.yaml (legacy preview dropped)"; \
	fi
	@dupes=$$(ls specs/*.yaml | xargs -n1 basename | sort | uniq -d); \
		if [ -n "$$dupes" ]; then \
			echo "Error: duplicate spec filenames (last-write-wins risk):"; \
			echo "$$dupes"; \
			exit 1; \
		fi
	@echo "Copied specs:"
	@ls specs/*.yaml | wc -l | xargs echo "  Total files:"
	@echo "Regenerating commands..."
	@$(MAKE) generate
	@echo "Done! Review changes with: git diff"

# Path to, or URL of, a consolidated Jamf Pro OpenAPI document (e.g.
# https://<instance>/api/schema/). When set, sync-spec splits it into per-
# resource spec files under specs/ before regenerating.
JAMF_MONOLITH_SPEC ?=

# Sync OpenAPI specs from a single consolidated document and regenerate commands.
# JAMF_MONOLITH_SPEC accepts a local path or an http(s):// URL, e.g.:
#   make sync-spec JAMF_MONOLITH_SPEC=/path/to/monolith-schema.json
#   make sync-spec JAMF_MONOLITH_SPEC=https://<instance>/api/schema/
# Preserved spec files (see generator/monolith/overrides.go PreservedSpecs) and
# the classic/ subdirectory is left untouched.
sync-spec:
	@if [ -z "$(JAMF_MONOLITH_SPEC)" ]; then \
		echo "Error: JAMF_MONOLITH_SPEC is not set."; \
		echo "Usage: make sync-spec JAMF_MONOLITH_SPEC=<url> JAMF_PRO_VERSION=<version>"; \
		echo "  e.g. JAMF_MONOLITH_SPEC=https://<instance>/api/schema/ JAMF_PRO_VERSION=11.14.0"; \
		exit 1; \
	fi
	@if [ -z "$(JAMF_PRO_VERSION)" ]; then \
		echo "Error: JAMF_PRO_VERSION is required — specify the Jamf Pro version the spec was fetched from."; \
		echo "Usage: make sync-spec JAMF_MONOLITH_SPEC=<url> JAMF_PRO_VERSION=<version>"; \
		echo "  e.g. JAMF_MONOLITH_SPEC=https://<instance>/api/schema/ JAMF_PRO_VERSION=11.14.0"; \
		exit 1; \
	fi
	@case "$(JAMF_MONOLITH_SPEC)" in \
		http://*|https://*) ;; \
		*) \
			if [ ! -f "$(JAMF_MONOLITH_SPEC)" ]; then \
				echo "Error: $(JAMF_MONOLITH_SPEC) not found"; \
				exit 1; \
			fi ;; \
	esac
	@echo "Ingesting monolith spec: $(JAMF_MONOLITH_SPEC)"
	@echo "$(JAMF_PRO_VERSION)" > specs/.spec-version
	@$(MAKE) generate JAMF_MONOLITH_SPEC=$(JAMF_MONOLITH_SPEC)
	@echo "Spec version: $(JAMF_PRO_VERSION) (written to specs/.spec-version)"
	@echo "Done! Review changes with: git diff specs internal/commands/pro/generated"

# Sync Jamf Platform Gateway specs from the gitignored .platform-source/
# directory into the published specs/platform/ tree, then regenerate. Drop
# updated *.json specs into specs/.platform-source/ and run this target —
# the spec layout, structure, and content remain unchanged on copy; the
# generator (generator/platform/) does the rest.
sync-platform-specs:
	@if [ ! -d "specs/.platform-source" ]; then \
		echo "Error: specs/.platform-source/ not found"; \
		echo "Drop Platform Gateway *.json specs into specs/.platform-source/ first."; \
		exit 1; \
	fi
	@mkdir -p specs/platform
	@rm -f specs/platform/*.json
	@cp specs/.platform-source/*.json specs/platform/
	@echo "Copied $$(ls specs/platform/*.json | wc -l | tr -d ' ') platform spec(s) to specs/platform/"
	@$(MAKE) generate
	@echo "Done! Review changes with: git diff specs/platform internal/commands/platform/generated"

# Generate CLI commands from OpenAPI specs and Classic API manifest.
# If JAMF_MONOLITH_SPEC is set, the monolith is split into per-resource spec
# files before parsing, preserving the existing filename layout.
generate:
	@echo "Generating commands from OpenAPI specs and Classic API manifest..."
ifneq ($(JAMF_MONOLITH_SPEC),)
	go run ./generator --specs ./specs --output ./internal/commands/pro/generated --monolith $(JAMF_MONOLITH_SPEC)
else
	go run ./generator --specs ./specs --output ./internal/commands/pro/generated
endif
	@go fmt ./internal/commands/pro/generated/...
	@-go fmt ./internal/blueprintcomponents/... 2>/dev/null
	@echo "Generated commands:"
	@ls internal/commands/pro/generated/*.go | grep -v registry | grep -v classic_ | wc -l | xargs echo "  Modern API resource files:"
	@ls internal/commands/pro/generated/classic_*.go 2>/dev/null | grep -v registry | wc -l | xargs echo "  Classic API resource files:"

# Development helpers
dev: build
	./bin/jamf-cli

# Update dependencies
deps:
	go mod tidy
	go mod download

# Generate site data and serve locally
site: build
	go run ./generator/site/main.go --binary ./bin/jamf-cli --output ./docs/site/commands.json
	@echo "Serving at http://localhost:8080 — press Ctrl+C to stop"
	@cd docs/site && python3 -m http.server 8080

# Verify site has support for all product namespaces (CI-safe)
verify-site: build
	@./scripts/verify-site-products.sh

# Verify generator output is well-formed and the static site is consistent
# with it: commands.json schema, llms.txt llmstxt.org compliance, JSON-LD
# in index.html parses, every Pro group is assigned to a pillar in
# catalog.js. Runs the generator end-to-end into /tmp so the working tree
# stays clean. (CI-safe.)
verify-site-output: build
	@mkdir -p /tmp/jamf-cli-site
	@go run ./generator/site \
		--binary ./bin/jamf-cli \
		--output /tmp/jamf-cli-site/commands.json \
		--llms-output /tmp/jamf-cli-site/llms.txt \
		--llms-full-output /tmp/jamf-cli-site/llms-full.txt
	@./scripts/verify-site-output.sh \
		--commands-json /tmp/jamf-cli-site/commands.json \
		--llms-txt /tmp/jamf-cli-site/llms.txt \
		--llms-full-txt /tmp/jamf-cli-site/llms-full.txt \
		--site-dir docs/site

# Verify generated code is up to date (CI-safe)
verify-generated:
	@ls internal/commands/pro/generated/*.go | grep -v '_test\.go' | xargs rm -f
	@$(MAKE) generate
	@if ! git diff --quiet -- internal/commands/pro/generated/; then \
		echo "Error: generated code is out of date"; \
		git diff --stat -- internal/commands/pro/generated/; \
		exit 1; \
	fi
	@echo "Generated code is up to date."

# Verify platform specs and generated platform code are in sync (CI-safe)
verify-platform-specs:
	@$(MAKE) sync-platform-specs > /dev/null 2>&1; true
	@if ! git diff --quiet -- specs/platform/; then \
		echo "Error: specs/platform/ is stale — run 'make sync-platform-specs' and commit"; \
		git diff --stat -- specs/platform/; \
		exit 1; \
	fi
	@ls internal/commands/platform/generated/*.go | grep -v '_test\.go' | xargs rm -f
	@$(MAKE) generate > /dev/null
	@if ! git diff --quiet -- internal/commands/platform/generated/; then \
		echo "Error: generated platform code is stale — run 'make generate' and commit"; \
		git diff --stat -- internal/commands/platform/generated/; \
		exit 1; \
	fi
	@echo "Platform specs and generated code are up to date."

# Smoke test against a real Jamf Pro instance (reads from default config profile)
smoke:
	JAMF_SMOKE_TEST=1 go test -v -run 'TestSmoke' -timeout 10m -count=1 ./internal/commands/...

# Seed test instance with minimal _smoke-test resources
smoke-seed:
	JAMF_SMOKE_TEST=1 go test -v -run 'TestSmoke_Seed' -timeout 5m -count=1 ./internal/commands/...

# Remove all _smoke-test resources from the test instance
smoke-cleanup:
	JAMF_SMOKE_TEST=1 go test -v -run 'TestSmoke_Cleanup' -timeout 5m -count=1 ./internal/commands/...

# Pre-release verification: unit tests + smoke tests
release-check: test smoke

# Format code
fmt:
	go fmt ./...
	gofumpt -w .
