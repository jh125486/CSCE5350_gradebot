.PHONY: help build test lint clean modernize
.DEFAULT_GOAL := help

# Variables
BINARY_NAME := gradebot
BUILD_DIR := bin

## help: Show this help message
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'

## init: Initialize development environment (install git hooks)
init:
	@echo "Initializing development environment..."
	@bash .githooks/install-hooks.sh
	@echo "Development environment initialized ✓"

build:
	@echo "Building $(BINARY_NAME)"
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "Build completed successfully: $(BUILD_DIR)/$(BINARY_NAME)"

## test: Run all tests with coverage
test:
	@echo "Running tests..."
	@go test -race -coverprofile=coverage.out ./...

## lint: Run all linting tools
lint: golangci-lint modernize
	@echo "All linting completed ✓"

## golangci-lint: Run golangci-lint
golangci-lint:
	@echo "Running golangci-lint..."
	@go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run --fix ./...

## modernize: Check for outdated Go patterns and suggest improvements
modernize:
	@echo "Running go mod tidy..."
	@go mod tidy
	@echo "Checking for vulnerabilities..."
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	@echo "Running modernize analysis..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...
	@echo "Checking for outdated dependencies..."
	@go list -u -m all | grep -v "=>"

## fmt: Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@go run golang.org/x/tools/cmd/goimports@latest -w .

## vet: Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

## check: Run all checks (format, vet, lint, test)
check: fmt vet lint test
	@echo "All checks completed ✓"

## run-client: Run gradebot in client mode (requires --type flag)
run-client: build
	@echo "Usage: make run-client TYPE=project1"
	@if [ -z "$(TYPE)" ]; then \
		echo "Error: TYPE parameter is required"; \
		echo "Example: make run-client TYPE=project1"; \
		exit 1; \
	fi
	@$(BUILD_DIR)/$(BINARY_NAME) check --type $(TYPE)

## run-server: Run gradebot in server mode
run-server: build
	@echo "Starting gradebot server..."
	@$(BUILD_DIR)/$(BINARY_NAME) -server

## docker-build: Build Docker image (if Dockerfile exists)
docker-build:
	@if [ -f Dockerfile ]; then \
		echo "Building Docker image..."; \
		docker build -t $(BINARY_NAME):latest .; \
	else \
		echo "No Dockerfile found"; \
	fi

## deps: Download and verify dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod verify

## proto: Regenerate protobuf Go files (protoc required)
proto:
	@echo "Generating protobuf Go files..."
	@which protoc >/dev/null 2>&1 || (echo "protoc not found; please install protoc" && exit 1)
	@protoc --version
	@protoc --go_out=. --go_opt=paths=source_relative \
		--connect-go_out=. --connect-go_opt=paths=source_relative \
		pkg/proto/quality.proto
	@protoc --go_out=. --go_opt=paths=source_relative \
		--connect-go_out=. --connect-go_opt=paths=source_relative \
		pkg/proto/rubric.proto
	@echo "Protobuf generation completed."
