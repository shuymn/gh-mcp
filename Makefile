# gh-mcp Makefile
BINARY_NAME := gh-mcp
MAIN_PACKAGE := ./cmd/gh-mcp
COVERAGE_FILE := coverage.out

# Default target
.DEFAULT_GOAL := help

# Build the binary
.PHONY: build
build: ## Build the binary
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)

# Run tests with race detection and shuffle
.PHONY: test
test: ## Run tests with race detection and shuffle (10 times)
	go test -race -shuffle=on -count=10 ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	go test -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	go tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests verbosely
.PHONY: test-verbose
test-verbose: ## Run tests with verbose output
	go test -v -race ./...

# Run linter
.PHONY: lint
lint: fmt ## Run golangci-lint
	golangci-lint run

# Format code
.PHONY: fmt
fmt: ## Format code using golangci-lint
	golangci-lint fmt

# Install the extension locally
.PHONY: install
install: build ## Install gh-mcp as a GitHub CLI extension
	gh extension install .

# Uninstall the extension
.PHONY: uninstall
uninstall: ## Uninstall gh-mcp extension
	gh extension remove mcp

# Clean build artifacts
.PHONY: clean
clean: ## Remove build artifacts and coverage files
	rm -f $(BINARY_NAME)
	rm -f $(COVERAGE_FILE) coverage.html
	go clean

# Run the extension locally for testing
.PHONY: run
run: build ## Build and run the extension
	./$(BINARY_NAME)

# Check for dependency updates
.PHONY: deps-check
deps-check: ## Check for dependency updates
	go list -u -m all

# Update dependencies
.PHONY: deps-update
deps-update: ## Update all dependencies
	go get -u ./...
	go mod tidy

# Verify dependencies
.PHONY: deps-verify
deps-verify: ## Verify dependencies are correct
	go mod verify

# Run all checks (test, lint, build)
.PHONY: check
check: test lint build ## Run all checks (test, lint, build)

# Show help
.PHONY: help
help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'
