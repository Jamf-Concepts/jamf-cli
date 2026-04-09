.PHONY: build test clean generate sync-specs install lint verify-generated smoke smoke-seed smoke-cleanup release-check site

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

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

# Generate CLI commands from OpenAPI specs and Classic API manifest
generate:
	@echo "Generating commands from OpenAPI specs and Classic API manifest..."
	go run ./generator/main.go --specs ./specs --output ./internal/commands/pro/generated
	@go fmt ./internal/commands/pro/generated/...
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
