.PHONY: audit-coverage build test clean generate sync-specs sync-spec sync-platform-specs sync-platform-specs-from-sdk sync-security-specs sync-gateway-coverage sync-gateway-coverage-from-sdk verify-gateway-coverage verify-classic-schemas install lint lint-dead verify-generated verify-platform-specs verify-security-specs verify-gateway-coverage verify-site verify-site-output smoke smoke-seed smoke-cleanup release-check site

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
	account_licensing_api.json \
	account_partners_api.json \
	account_sso_api.json \
	ai_governance_policies_api.json \
	audit_api.json \
	blueprints_api.json \
	compliance_benchmark_engine.json \
	declaration_reporting_service.json \
	device_group_inventory_api.json \
	device_inventory_api.json \
	device_management_action_api.json \
	securitycloud_categories_api.json \
	securitycloud_device_groups_api.json \
	securitycloud_dns_api.json \
	securitycloud_enrollment_api.json \
	securitycloud_uem_connect_api.json \
	securitycloud_ztna_api.json

# PLATFORM_SDK_COVERAGE_SPECS are the two SDK specs read for gateway *coverage*
# — which Jamf Pro and Classic endpoints the gateway publishes, and the gateway
# scope each requires — and, for the Classic one, for the Classic request-body
# schemas behind --scaffold and --set on classic commands. They are copied into
# the drop directory by sync-platform-specs-from-sdk and consumed by
# sync-gateway-coverage, which derives both specs/gateway/coverage.json and
# specs/classic/schemas.json in one generator run.
#
# They must never join PLATFORM_SDK_SPECS. They describe Jamf Pro APIs this repo
# already generates from specs/*.yaml, so handing them to the platform generator
# emits a second set of Pro commands built from gateway paths.
PLATFORM_SDK_COVERAGE_SPECS = \
	pro_api.json \
	classic_api_resource_documentation.json

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
	@for f in $(PLATFORM_SDK_SPECS) $(PLATFORM_SDK_COVERAGE_SPECS); do \
		if [ ! -f "$(JAMFPLATFORM_SDK_PATH)/api/$$f" ]; then \
			echo "Error: $$f missing from $(JAMFPLATFORM_SDK_PATH)/api"; \
			exit 1; \
		fi; \
		cp "$(JAMFPLATFORM_SDK_PATH)/api/$$f" "specs/.platform-source/$$f"; \
	done
	@echo "Copied $$(echo $(PLATFORM_SDK_SPECS) $(PLATFORM_SDK_COVERAGE_SPECS) | wc -w | tr -d ' ') spec(s) from $(JAMFPLATFORM_SDK_PATH)/api"
	@$(MAKE) sync-platform-specs
	@$(MAKE) sync-gateway-coverage JAMFPLATFORM_SDK_PATH="$(JAMFPLATFORM_SDK_PATH)"

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

# ── Jamf Platform gateway coverage ────────────────────────────────────────
#
# Which Jamf Pro and Classic endpoints the platform gateway publishes. Derived
# into the committed specs/gateway/coverage.json, which the generator reads to
# stamp a jamf:gateway annotation on the commands outside that surface — so
# `pro app-installer-titles list` on a platform profile is refused with an
# explanation instead of the gateway's 403 BAD_PERMISSIONS, which is
# byte-identical to a missing API-role privilege.
#
# The source is jamfplatform-go-sdk's api/, the same place specs/platform/ comes
# from: pro_api.json and classic_api_resource_documentation.json, listed above as
# PLATFORM_SDK_COVERAGE_SPECS. Both are complete as of SDK adb8d7b (528 and 273
# paths, method-for-method identical to the gateway's own drops) and each
# operation carries x-required-privileges in the gateway scope vocabulary.
#
# That last part is why the SDK is now sufficient alone. Coverage was previously
# derived from the jamf/public-apis-oas GitOps bundle, because only its
# _permissions/routes.yaml carried the scope map. The SDK's x-required-privileges
# now reproduces it exactly — 1352 operations, zero disagreements, none on either
# side alone — so the bundle route is gone rather than kept as a second path
# nothing exercises.
#
# The manifest is committed so `make generate` and CI stay hermetic: nobody needs
# an SDK checkout to build, and the drop directory is gitignored.
#
# The same run also derives specs/classic/schemas.json, the Classic API request-body
# schemas behind --scaffold, --set and the required/enum help on classic write
# commands (see generator/classicschema). Same source spec, same drop directory,
# same hermetic-artifact reasoning — so one flag, not two: a second flag that has
# to carry the same value is a code path nothing exercises on its own.
sync-gateway-coverage:
	@for f in $(PLATFORM_SDK_COVERAGE_SPECS); do \
		if [ ! -f "specs/.platform-source/$$f" ]; then \
			echo "Error: specs/.platform-source/$$f not found"; \
			echo "Fetch it with: make sync-gateway-coverage-from-sdk JAMFPLATFORM_SDK_PATH=/path/to/jamfplatform-go-sdk"; \
			exit 1; \
		fi; \
	done
	@# Record the SDK revision only when we were actually given an SDK checkout.
	@# `cd ''` is a no-op that SUCCEEDS, so the obvious one-liner
	@#   cd '$(JAMFPLATFORM_SDK_PATH)' && git rev-parse --short HEAD
	@# stays in THIS repo when the variable is empty and stamps jamfpro-cli's own
	@# commit into the manifest as the SDK's. A manifest whose provenance is
	@# confidently wrong is worse than one that admits it does not know, so the
	@# path is tested before it is used.
	@rm -f .gateway-sdk-rev
	@if [ -n "$(JAMFPLATFORM_SDK_PATH)" ] && [ -d "$(JAMFPLATFORM_SDK_PATH)/.git" ]; then \
		git -C "$(JAMFPLATFORM_SDK_PATH)" rev-parse --short HEAD > .gateway-sdk-rev 2>/dev/null || true; \
	fi
	go run ./generator --specs ./specs --output ./internal/commands/pro/generated \
		--gateway-source ./specs/.platform-source \
		--gateway-sdk-rev "$$(cat .gateway-sdk-rev 2>/dev/null || true)"
	@rm -f .gateway-sdk-rev
	@go fmt ./internal/commands/pro/generated/... ./internal/gateway/... > /dev/null
	@echo "Done! Review changes with: git diff specs/gateway specs/classic internal/gateway internal/commands"

# Copy the two coverage specs from an SDK checkout, then derive. Recording the
# SDK revision is the point of routing through here: a manifest that cannot say
# which SDK it came from is one nobody can check for staleness.
#
#   make sync-gateway-coverage-from-sdk JAMFPLATFORM_SDK_PATH=../jamfplatform-go-sdk
sync-gateway-coverage-from-sdk:
	@if [ -z "$(JAMFPLATFORM_SDK_PATH)" ]; then \
		echo "Error: JAMFPLATFORM_SDK_PATH is required"; \
		echo "Usage: make sync-gateway-coverage-from-sdk JAMFPLATFORM_SDK_PATH=/path/to/jamfplatform-go-sdk"; \
		exit 1; \
	fi
	@if [ ! -d "$(JAMFPLATFORM_SDK_PATH)/api" ]; then \
		echo "Error: $(JAMFPLATFORM_SDK_PATH)/api not found — is that a jamfplatform-go-sdk checkout?"; \
		exit 1; \
	fi
	@mkdir -p specs/.platform-source
	@for f in $(PLATFORM_SDK_COVERAGE_SPECS); do \
		if [ ! -f "$(JAMFPLATFORM_SDK_PATH)/api/$$f" ]; then \
			echo "Error: $$f missing from $(JAMFPLATFORM_SDK_PATH)/api"; \
			exit 1; \
		fi; \
		cp "$(JAMFPLATFORM_SDK_PATH)/api/$$f" "specs/.platform-source/$$f"; \
	done
	@$(MAKE) sync-gateway-coverage JAMFPLATFORM_SDK_PATH="$(JAMFPLATFORM_SDK_PATH)"

# Verify the committed manifest and the generated table are in sync (CI-safe).
# A no-op pass when the coverage specs are absent, like verify-security-specs —
# CI has no SDK checkout, and the committed manifest is the contract.
# git status --porcelain rather than git diff, because git diff cannot see an
# untracked file: the first run after adding specs/gateway/ regenerated the
# manifest with different provenance and still reported clean. Porcelain covers
# modified, staged and untracked alike, which is the honest reading of "this is
# not what is committed".
verify-gateway-coverage:
	@if [ -n "$(JAMFPLATFORM_SDK_PATH)" ] || [ -f "specs/.platform-source/pro_api.json" ]; then \
		$(MAKE) sync-gateway-coverage JAMFPLATFORM_SDK_PATH="$(JAMFPLATFORM_SDK_PATH)" > /dev/null; \
	else \
		$(MAKE) generate > /dev/null; \
	fi
	@for f in specs/gateway/coverage.json internal/gateway/coverage_gen.go; do \
		if [ -n "$$(git status --porcelain -- $$f)" ]; then \
			echo "Error: $$f is not what is committed — run 'make sync-gateway-coverage' and commit"; \
			git status --short -- $$f; \
			exit 1; \
		fi; \
	done
	@echo "Gateway coverage manifest and table are up to date."

# Verify the committed Classic schema artifact is what the SDK spec implies
# (CI-safe). A no-op pass when the coverage specs are absent, like
# verify-gateway-coverage — CI has no SDK checkout, and the committed artifact is
# the contract that `make verify-generated` then checks the commands against.
#
# git status --porcelain rather than git diff, for the reason the gateway target
# records: git diff cannot see an untracked file, so the first run after adding
# specs/classic/schemas.json reported clean against a file that was not committed
# at all.
verify-classic-schemas:
	@if [ -n "$(JAMFPLATFORM_SDK_PATH)" ] || [ -f "specs/.platform-source/classic_api_resource_documentation.json" ]; then \
		$(MAKE) sync-gateway-coverage JAMFPLATFORM_SDK_PATH="$(JAMFPLATFORM_SDK_PATH)" > /dev/null; \
	else \
		$(MAKE) generate > /dev/null; \
	fi
	@if [ -n "$$(git status --porcelain -- specs/classic/schemas.json)" ]; then \
		echo "Error: specs/classic/schemas.json is not what is committed — run 'make sync-gateway-coverage' and commit"; \
		git status --short -- specs/classic/schemas.json; \
		exit 1; \
	fi
	@echo "Classic schema artifact is up to date."

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
