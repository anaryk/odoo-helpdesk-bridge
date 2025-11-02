# Makefile for odoo-helpdesk-bridge

.PHONY: help build test clean run docker-build docker-run lint security deps

# Default target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

# Build configuration
BUILD_DIR := ./build
BINARY_NAME := helpdesk-bridge
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build: ## Build the application
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/helpdesk-bridge

test: ## Run tests
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...

test-coverage: test ## Run tests and show coverage
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

bench: ## Run benchmarks
	go test -bench=. -benchmem ./...

clean: ## Clean build artifacts
	@echo "Cleaning up..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	rm -f $(BINARY_NAME)

run: build ## Build and run the application
	@echo "Running $(BINARY_NAME)..."
	$(BUILD_DIR)/$(BINARY_NAME)

run-dev: ## Run with development config
	@echo "Running $(BINARY_NAME) with config.yaml..."
	go run ./cmd/helpdesk-bridge config.yaml

deps: ## Download and tidy dependencies
	@echo "Managing dependencies..."
	go mod download
	go mod tidy
	go mod verify

lint: ## Run linting
	@echo "Running linters..."
	go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not installed, run: go install honnef.co/go/tools/cmd/staticcheck@latest"; \
	fi
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, see: https://golangci-lint.run/usage/install/"; \
	fi

security: ## Run security checks
	@echo "Running security checks..."
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "govulncheck not installed, run: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
		echo "Installing govulncheck..."; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
		govulncheck ./...; \
	fi

format: ## Format code
	@echo "Formatting code..."
	go fmt ./...
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w .; \
	else \
		echo "goimports not installed, run: go install golang.org/x/tools/cmd/goimports@latest"; \
	fi

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t odoo-helpdesk-bridge:latest .
	docker build -t odoo-helpdesk-bridge:$(VERSION) .

docker-run: ## Run Docker container
	@echo "Running Docker container..."
	docker run --rm -v $(PWD)/config.yaml:/app/config/config.yaml \
		-v $(PWD)/data:/app/data \
		odoo-helpdesk-bridge:latest

docker-compose-up: ## Start with docker-compose
	docker-compose up -d

docker-compose-down: ## Stop docker-compose
	docker-compose down

docker-compose-logs: ## View docker-compose logs
	docker-compose logs -f

install-tools: ## Install development tools
	@echo "Installing development tools..."
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "Tools installed successfully!"

install: build ## Install binary to GOPATH/bin
	@echo "Installing $(BINARY_NAME) to GOPATH/bin..."
	go install $(LDFLAGS) ./cmd/helpdesk-bridge

release-build: ## Build release binaries for multiple platforms
	@echo "Building release binaries..."
	@mkdir -p $(BUILD_DIR)/release
	
	# Linux AMD64
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-linux-amd64 ./cmd/helpdesk-bridge
	
	# Linux ARM64
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-linux-arm64 ./cmd/helpdesk-bridge
	
	# Windows AMD64
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-windows-amd64.exe ./cmd/helpdesk-bridge
	
	# Darwin AMD64
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-darwin-amd64 ./cmd/helpdesk-bridge
	
	# Darwin ARM64 (M1/M2)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-darwin-arm64 ./cmd/helpdesk-bridge
	
	@echo "Release binaries built in $(BUILD_DIR)/release/"

generate-checksums: release-build ## Generate checksums for release binaries
	@echo "Generating checksums..."
	@cd $(BUILD_DIR)/release && sha256sum * > checksums.txt
	@echo "Checksums generated: $(BUILD_DIR)/release/checksums.txt"

ci: deps lint test security ## Run CI pipeline locally
	@echo "CI pipeline completed successfully!"

all: clean deps lint test security build ## Run full build pipeline

watch: ## Watch for changes and rebuild (requires entr)
	@if command -v entr >/dev/null 2>&1; then \
		find . -name '*.go' | entr -r make run-dev; \
	else \
		echo "entr not installed. Install with: brew install entr (macOS) or apt-get install entr (Ubuntu)"; \
	fi

# Development helpers
dev-setup: install-tools ## Set up development environment
	@echo "Development environment setup complete!"
	@echo "Run 'make help' to see available commands."