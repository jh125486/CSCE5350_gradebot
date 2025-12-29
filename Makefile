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

upgrade:
	@echo "Upgrading Go modules to latest versions..."
	@go get -u -t ./...
	@go mod tidy
	@echo "Go modules upgraded ✓"

build:
	@echo "Building $(BINARY_NAME)"
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "Build completed successfully: $(BUILD_DIR)/$(BINARY_NAME)"

## test: Run all tests with coverage
test:
	@echo "Running tests..."
	@go test -timeout 30s -race -coverprofile=coverage.out ./...

tidy:
	@echo "Tidying Go modules..."
	@go mod tidy
	@echo "Go modules tidied ✓"

## static: Run all linting tools
static: tidy vet golangci-lint modernize vuln-check outdated
	@echo "All linting completed ✓"

## golangci-lint: Run golangci-lint
golangci-lint:
	@echo "Running $$(go tool golangci-lint version)..."
	@go tool golangci-lint run --fix ./...

vuln-check:
	@echo "Checking for vulnerabilities..."
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...

## modernize: Check for outdated Go patterns and suggest improvements
modernize:
	@echo "Running modernize analysis..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...
	
outdated:
	@echo "Checking for outdated direct dependencies..."
	@go list -u -m -f '{{if not .Indirect}}{{.}}{{end}}' all 2>/dev/null | grep '\[' || echo "All direct dependencies are up to date"

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
check: tidy fmt static test
	@echo "All checks completed ✓"

## run-server: Run gradebot in server mode
run-server: build
	@echo "Starting gradebot server..."
	@$(BUILD_DIR)/$(BINARY_NAME) server

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
