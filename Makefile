.PHONY: build test clean generate sync-specs release install lint

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

# Default target
all: build

# Build the CLI
build:
	go build $(LDFLAGS) -o bin/jamfpro-cli ./cmd/jamfpro-cli

# Install locally
install:
	go install $(LDFLAGS) ./cmd/jamfpro-cli

# Run tests
test:
	go test -v ./...

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
JAMF_SERVER_PATH ?= ../jamf-pro-server
SPECS_SRC := $(JAMF_SERVER_PATH)/api/api-impl/src/main/resources/swagger_docs/uapi

# Sync OpenAPI specs from jamf-pro-server repo and regenerate commands
sync-specs:
	@if [ ! -d "$(SPECS_SRC)" ]; then \
		echo "Error: jamf-pro-server not found at $(JAMF_SERVER_PATH)"; \
		echo "Usage: make sync-specs JAMF_SERVER_PATH=/path/to/jamf-pro-server"; \
		exit 1; \
	fi
	@echo "Syncing OpenAPI specs from $(SPECS_SRC)..."
	@rm -f specs/*.yaml
	@cp $(SPECS_SRC)/*.yaml specs/ 2>/dev/null || true
	@cp $(SPECS_SRC)/common/*.yaml specs/ 2>/dev/null || true
	@echo "Copied specs:"
	@ls specs/*.yaml | wc -l | xargs echo "  Total files:"
	@echo "Regenerating commands..."
	@$(MAKE) generate
	@echo "Formatting generated code..."
	@go fmt ./internal/commands/generated/...
	@echo "Done! Review changes with: git diff"

# Generate CLI commands from OpenAPI specs
generate:
	@echo "Generating commands from OpenAPI specs..."
	go run ./generator/main.go --specs ./specs --output ./internal/commands/generated
	@echo "Generated commands:"
	@ls internal/commands/generated/*.go | grep -v registry | wc -l | xargs echo "  Resource files:"

# Release (requires goreleaser)
release:
	goreleaser release --clean

# Release snapshot (for testing)
release-snapshot:
	goreleaser release --snapshot --clean

# Development helpers
dev: build
	./bin/jamfpro-cli

# Update dependencies
deps:
	go mod tidy
	go mod download

# Format code
fmt:
	go fmt ./...
	gofumpt -w .
