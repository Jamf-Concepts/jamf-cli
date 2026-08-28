.PHONY: audit-coverage build test clean generate sync-specs sync-spec sync-platform-specs sync-platform-specs-from-sdk sync-security-specs install lint lint-dead verify-generated verify-platform-specs verify-security-specs verify-site verify-site-output smoke smoke-seed smoke-cleanup release-check site

# Build variables
VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE            ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
# Sanitized at the read site, not just at the write site. This value is spliced
# into LDFLAGS below, and make hands LDFLAGS to a shell, so a stray quote or
# semicolon in the file would run as a command. tr -cd keeps only the characters
# a version can contain, which also collapses the newlines that tr -d '[:space:]'
# used to strip. The two sync targets guard what they write, but this file is
# also editable by hand and by a rebase.
SPEC_PRO_VERSION := $(shell cat specs/.spec-version 2>/dev/null | tr -cd '0-9A-Za-z.-' || echo "unknown")
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
# The directory actually scanned for specs. Derived from JAMF_SERVER_PATH for a
# normal side-by-side checkout; override it directly when the server tree is not
# nested under a jamf-pro-server/ directory — .github/workflows/sync-specs.yaml
# does this, since actions/checkout puts the tree straight into jss/.
JAMF_SERVER_ROOT ?= $(JAMF_SERVER_PATH)/jamf-pro-server

# The sync targets write JAMF_PRO_VERSION into specs/.spec-version and echo it.
# Export it so the recipes below can read it from the environment as
# $$JAMF_PRO_VERSION. Make substitutes $(JAMF_PRO_VERSION) textually, so a value
# that contains a double quote closes the string and the rest runs as a command.
# The environment reference is not substituted, so the shell sees one value.
export JAMF_PRO_VERSION

# check_jamf_pro_version rejects a value that is not a Jamf Pro version number,
# optionally with a build suffix (11.31.0 or 11.31.0-t1785774933693). It reads
# the environment copy, so a hostile value cannot escape the test itself.
# $$1 is the usage line of the calling target.
#
# Two checks, because one is not enough. The character class runs first and
# rejects every character outside the accepted set, which is what excludes a
# newline. A regex alone cannot do this: grep anchors ^ and $$ per LINE, so it
# accepts a multi-line value as soon as any single line matches, and the
# remaining lines then travel unchecked into specs/.spec-version. From there
# SPEC_PRO_VERSION strips the newline and splices the rest into LDFLAGS, which
# go build hands to a shell. The regex then only confirms the shape.
define check_jamf_pro_version
@if [ -z "$$JAMF_PRO_VERSION" ]; then \
	echo "Error: JAMF_PRO_VERSION is required — specify the Jamf Pro version the specs came from."; \
	echo "$(1)"; \
	exit 1; \
fi
@case "$$JAMF_PRO_VERSION" in \
	*[!0-9A-Za-z.-]*) \
		echo "Error: JAMF_PRO_VERSION must not contain whitespace or punctuation other than . and -"; \
		echo "$(1)"; \
		exit 1;; \
esac
@printf '%s' "$$JAMF_PRO_VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$$' || { \
	echo "Error: JAMF_PRO_VERSION must be three dot-separated numbers, with an optional build suffix."; \
	echo "  Accepted: 11.31.0 or 11.31.0-t1785774933693"; \
	echo "$(1)"; \
	exit 1; \
}
endef

# Sync OpenAPI specs from jamf-pro-server repo and regenerate commands
sync-specs:
	@if [ ! -d "$(JAMF_SERVER_ROOT)" ]; then \
		echo "Error: jamf-pro-server spec tree not found at $(JAMF_SERVER_ROOT)"; \
		echo "Set JAMF_SERVER_PATH to the repo's parent, or JAMF_SERVER_ROOT to the tree itself."; \
		echo "Usage: make sync-specs JAMF_SERVER_PATH=/path/to/jss JAMF_PRO_VERSION=<version>"; \
		exit 1; \
	fi
	$(call check_jamf_pro_version,Usage: make sync-specs JAMF_SERVER_PATH=/path/to/jss JAMF_PRO_VERSION=<version> (e.g. JAMF_PRO_VERSION=11.31.0))
	@echo "Syncing OpenAPI specs from $(JAMF_SERVER_ROOT)..."
	@# Duplicate basenames are checked against the SOURCE tree, before anything
	@# is copied: two upstream modules shipping the same filename collapse into
	@# one file the moment cp runs, so a post-copy `ls specs/ | uniq -d` can
	@# never see them. Validating first also means a rejected sync leaves the
	@# committed specs/ untouched.
	@# One find pass covers what used to be two (all uapi/**.yaml except
	@# hiddenapi, which includes common/) so the check sees the same set that
	@# gets copied.
	@set -e; \
	sources=$$(find $(JAMF_SERVER_ROOT) -path "*/swagger_docs/uapi/*.yaml" \
		-not -path "*/uapi/hiddenapi/*"); \
	if [ -z "$$sources" ]; then \
		echo "Error: no specs found under $(JAMF_SERVER_ROOT)/**/swagger_docs/uapi/"; \
		exit 1; \
	fi; \
	dupes=$$(printf '%s\n' "$$sources" | xargs -n1 basename | sort | uniq -d); \
	if [ -n "$$dupes" ]; then \
		echo "Error: duplicate spec filenames in $(JAMF_SERVER_ROOT) (last-write-wins risk):"; \
		printf '%s\n' "$$dupes" | sed 's/^/  /'; \
		exit 1; \
	fi; \
	rm -f specs/*.yaml; \
	printf '%s\n' "$$sources" | while IFS= read -r spec; do cp "$$spec" specs/; done
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
	@echo "Copied specs:"
	@ls specs/*.yaml | wc -l | xargs echo "  Total files:"
	@echo "Regenerating commands..."
	@$(MAKE) generate
	@# Stamped last: a mid-run failure must not leave .spec-version (and hence
	@# the version baked into the next build) claiming a sync that didn't finish.
	@printf '%s\n' "$$JAMF_PRO_VERSION" > specs/.spec-version
	@echo "Spec version: $$JAMF_PRO_VERSION (written to specs/.spec-version)"
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
	$(call check_jamf_pro_version,Usage: make sync-spec JAMF_MONOLITH_SPEC=<url> JAMF_PRO_VERSION=<version> (e.g. JAMF_PRO_VERSION=11.14.0))
	@case "$(JAMF_MONOLITH_SPEC)" in \
		http://*|https://*) ;; \
		*) \
			if [ ! -f "$(JAMF_MONOLITH_SPEC)" ]; then \
				echo "Error: $(JAMF_MONOLITH_SPEC) not found"; \
				exit 1; \
			fi ;; \
	esac
	@echo "Ingesting monolith spec: $(JAMF_MONOLITH_SPEC)"
	@$(MAKE) generate JAMF_MONOLITH_SPEC="$(JAMF_MONOLITH_SPEC)"
	@# Stamped after generate, not before — see sync-specs.
	@printf '%s\n' "$$JAMF_PRO_VERSION" > specs/.spec-version
	@echo "Spec version: $$JAMF_PRO_VERSION (written to specs/.spec-version)"
	@echo "Done! Review changes with: git diff specs internal/commands/pro/generated"

# PLATFORM_SDK_SPECS is the set of Platform Gateway specs this CLI generates
# from, and it is authoritative: sync-platform-specs copies exactly these and
# nothing else.
#
# It is a list rather than a wildcard for two reasons. The SDK's api/ also holds
# pro_api.json, the Classic documentation and the app-installer specs — Jamf Pro
# APIs generated here from specs/*.yaml, which would emit bogus platform commands
# from Pro paths. And specs/.platform-source/ is gitignored, so its contents are
# whatever a developer last left there and differ per working tree; a wildcard
# made `make verify-platform-specs` depend on that, and an unrelated spec sitting
# in the directory would silently join the build on any branch.
PLATFORM_SDK_SPECS = \
	blueprints_api.json \
	compliance_benchmark_engine.json \
	declaration_reporting_service.json \
	device_group_inventory_api.json \
	device_inventory_api.json \
	device_management_action_api.json \
	securitycloud_categories_api.json \
	securitycloud_device_groups_api.json \
	securitycloud_dns_api.json \
	securitycloud_uem_connect_api.json \
	securitycloud_ztna_api.json

# Platform Gateway specs are sourced from jamfplatform-go-sdk's published api/
# directory — see sync-platform-specs-from-sdk below, which is the supported
# route. The SDK is the only place the specs are normalised and wire-verified
# against a live tenant, so taking them from anywhere else is how they drift:
# a stale copy of the compliance-benchmark spec left `pro rules list` sending a
# query parameter the server had renamed, returning 0 rules for every baseline.
#
# specs/platform/ filenames therefore match the SDK's api/ filenames exactly, so
# a refresh is a copy with no mapping to keep in step.
#
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
	@for f in $(PLATFORM_SDK_SPECS); do \
		if [ ! -f "specs/.platform-source/$$f" ]; then \
			echo "Error: specs/.platform-source/$$f not found"; \
			echo "Fetch it with: make sync-platform-specs-from-sdk JAMFPLATFORM_SDK_PATH=/path/to/jamfplatform-go-sdk"; \
			exit 1; \
		fi; \
		cp "specs/.platform-source/$$f" specs/platform/; \
	done
	@echo "Copied $$(ls specs/platform/*.json | wc -l | tr -d ' ') platform spec(s) to specs/platform/"
	@$(MAKE) generate
	@echo "Done! Review changes with: git diff specs/platform internal/commands/platform/generated"

# Copy the Platform Gateway specs from a jamfplatform-go-sdk checkout into the
# drop directory, then run the normal sync. This is the supported way to refresh
# them: the SDK publishes api/*.json from its own generator, having applied its
# schema corrections and validated them with acceptance tests against a live
# tenant, so it is the only source that is both normalised and verified.
#
#   make sync-platform-specs-from-sdk JAMFPLATFORM_SDK_PATH=../jamfplatform-go-sdk
sync-platform-specs-from-sdk:
	@if [ -z "$(JAMFPLATFORM_SDK_PATH)" ]; then \
		echo "Error: JAMFPLATFORM_SDK_PATH is required"; \
		echo "Usage: make sync-platform-specs-from-sdk JAMFPLATFORM_SDK_PATH=/path/to/jamfplatform-go-sdk"; \
		exit 1; \
	fi
	@if [ ! -d "$(JAMFPLATFORM_SDK_PATH)/api" ]; then \
		echo "Error: $(JAMFPLATFORM_SDK_PATH)/api not found — is that a jamfplatform-go-sdk checkout?"; \
		exit 1; \
	fi
	@mkdir -p specs/.platform-source
	@for f in $(PLATFORM_SDK_SPECS); do \
		if [ ! -f "$(JAMFPLATFORM_SDK_PATH)/api/$$f" ]; then \
			echo "Error: $$f missing from $(JAMFPLATFORM_SDK_PATH)/api"; \
			exit 1; \
		fi; \
		cp "$(JAMFPLATFORM_SDK_PATH)/api/$$f" "specs/.platform-source/$$f"; \
	done
	@echo "Copied $$(echo $(PLATFORM_SDK_SPECS) | wc -w | tr -d ' ') spec(s) from $(JAMFPLATFORM_SDK_PATH)/api"
	@$(MAKE) sync-platform-specs

# Copy private Jamf Security Cloud specs (Risk, Device Lifecycle, Shared
# Signals & Events) from the gitignored specs/.security-source/ drop location
# into the committed specs/security/ directory. Like platform, these ARE fed
# into the code generator — generator/parser/security.go hand-maps each of
# the twelve known operations (too few and irregular for tag/family
# auto-detection) via the securityOpsByFile map, and generator/security emits
# internal/commands/security/generated/. Only "security setup" is
# hand-written (internal/commands/security_setup.go); adding a new endpoint
# needs both an updated spec here and a new securityOpsByFile entry, then
# `make generate`.
sync-security-specs:
	@if [ ! -d "specs/.security-source" ]; then \
		echo "Error: specs/.security-source/ not found"; \
		echo "Drop Jamf Security Cloud *.json specs into specs/.security-source/ first."; \
		exit 1; \
	fi
	@mkdir -p specs/security
	@rm -f specs/security/*.json
	@cp specs/.security-source/*.json specs/security/
	@echo "Copied $$(ls specs/security/*.json | wc -l | tr -d ' ') security spec(s) to specs/security/"
	@echo "Done! Review changes with: git diff specs/security"

# Generate CLI commands from OpenAPI specs and Classic API manifest.
# If JAMF_MONOLITH_SPEC is set, the monolith is split into per-resource spec
# files before parsing, preserving the existing filename layout.
generate:
	@echo "Generating commands from OpenAPI specs and Classic API manifest..."
ifneq ($(JAMF_MONOLITH_SPEC),)
	go run ./generator --specs ./specs --output ./internal/commands/pro/generated --monolith "$(JAMF_MONOLITH_SPEC)"
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
		--binary ./bin/jamf-cli \
		--site-dir docs/site

# Verify generated code is up to date (CI-safe)
# Report which mutating gateway requests this CLI sends are covered by a Tyk
# audit rule. Needs a jamf/tyk-gateway-management checkout, and a ref: a local
# checkout is easily behind the deployed state.
#
#   make audit-coverage TYK_PATH=/path/to/tyk-gateway-management TYK_REF=origin/main
TYK_PATH ?= /Users/Shared/GitHub/jamf/tyk-gateway-management
TYK_REF ?= HEAD
audit-coverage:
	@python3 scripts/audit-coverage.py "$(TYK_PATH)" "$(TYK_REF)"

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

# Verify security specs and generated security code are in sync (CI-safe; a
# no-op pass when specs/.security-source/ isn't present, same as
# verify-platform-specs)
verify-security-specs:
	@$(MAKE) sync-security-specs > /dev/null 2>&1; true
	@if ! git diff --quiet -- specs/security/; then \
		echo "Error: specs/security/ is stale — run 'make sync-security-specs' and commit"; \
		git diff --stat -- specs/security/; \
		exit 1; \
	fi
	@ls internal/commands/security/generated/*.go | grep -v '_test\.go' | xargs rm -f
	@$(MAKE) generate > /dev/null
	@if ! git diff --quiet -- internal/commands/security/generated/; then \
		echo "Error: generated security code is stale — run 'make generate' and commit"; \
		git diff --stat -- internal/commands/security/generated/; \
		exit 1; \
	fi
	@echo "Security specs and generated code are up to date."

# Smoke test against a real Jamf Pro instance (reads from default config profile)
# Read-only sweep: every GET across all resources. The pattern is deliberately
# TestSmoke_Tier and not TestSmoke — the latter also matches TestSmoke_Seed,
# which CREATES objects on the target tenant, so `make smoke` used to mutate the
# instance it was asked to inspect. Seeding is `make smoke-seed`, explicitly.
smoke:
	JAMF_SMOKE_TEST=1 go test -v -run 'TestSmoke_Tier' -timeout 10m -count=1 ./internal/commands/...

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
