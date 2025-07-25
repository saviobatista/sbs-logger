# SBS Logger Makefile

.PHONY: help build test clean docker-build docker-push lint fmt deps

# Variables
BINARY_DIR=bin
DOCKER_REGISTRY=ghcr.io
IMAGE_PREFIX=savio/sbs-logger
VERSION=$(shell git describe --tags --always --dirty)
SERVICES=ingestor logger tracker

# Default target
help: ## Show this help message
	@echo "SBS Logger - Available commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Development
deps: ## Download Go dependencies
	go mod download
	go mod tidy

fmt: ## Format Go code
	go fmt ./...
	goimports -w .

lint: ## Run linter
	golangci-lint run

test: ## Run tests
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

test-short: ## Run tests without race detection
	go test -v ./...

# Building
build: ## Build all binaries
	@mkdir -p $(BINARY_DIR)
	@for service in $(SERVICES); do \
		echo "Building $$service..."; \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
			-ldflags="-w -s -X main.Version=$(VERSION)" \
			-o $(BINARY_DIR)/$$service-linux-amd64 ./cmd/$$service; \
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
			-ldflags="-w -s -X main.Version=$(VERSION)" \
			-o $(BINARY_DIR)/$$service-linux-arm64 ./cmd/$$service; \
		CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build \
			-ldflags="-w -s -X main.Version=$(VERSION)" \
			-o $(BINARY_DIR)/$$service-darwin-amd64 ./cmd/$$service; \
	done

build-%: ## Build specific service (e.g., build-ingestor)
	@mkdir -p $(BINARY_DIR)
	@echo "Building $*..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags="-w -s -X main.Version=$(VERSION)" \
		-o $(BINARY_DIR)/$*-linux-amd64 ./cmd/$*
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
		-ldflags="-w -s -X main.Version=$(VERSION)" \
		-o $(BINARY_DIR)/$*-linux-arm64 ./cmd/$*
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build \
		-ldflags="-w -s -X main.Version=$(VERSION)" \
		-o $(BINARY_DIR)/$*-darwin-amd64 ./cmd/$*

# Docker
docker-build: ## Build all Docker images
	@for service in $(SERVICES); do \
		echo "Building Docker image for $$service..."; \
		docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.$$service -t $(IMAGE_PREFIX)/sbs-$$service:$(VERSION) .; \
		docker tag $(IMAGE_PREFIX)/sbs-$$service:$(VERSION) $(IMAGE_PREFIX)/sbs-$$service:latest; \
	done

docker-build-test: ## Test Docker builds locally
	@echo "Testing Docker builds..."
	@./scripts/build-test.sh

docker-build-%: ## Build specific Docker image (e.g., docker-build-ingestor)
	@echo "Building Docker image for $*..."
	docker build -f Dockerfile.$* -t $(IMAGE_PREFIX)/sbs-$*:$(VERSION) .
	docker tag $(IMAGE_PREFIX)/sbs-$*:$(VERSION) $(IMAGE_PREFIX)/sbs-$*:latest

docker-push: ## Push all Docker images
	@for service in $(SERVICES); do \
		echo "Pushing Docker image for $$service..."; \
		docker push $(IMAGE_PREFIX)/sbs-$$service:$(VERSION); \
		docker push $(IMAGE_PREFIX)/sbs-$$service:latest; \
	done

docker-push-%: ## Push specific Docker image (e.g., docker-push-ingestor)
	@echo "Pushing Docker image for $*..."
	docker push $(IMAGE_PREFIX)/sbs-$*:$(VERSION)
	docker push $(IMAGE_PREFIX)/sbs-$*:latest

# Docker Compose
up: ## Start all services with docker-compose
	docker-compose up -d

down: ## Stop all services with docker-compose
	docker-compose down

logs: ## Show logs from all services
	docker-compose logs -f

logs-%: ## Show logs from specific service (e.g., logs-ingestor)
	docker-compose logs -f $*

# Database migrations are now handled automatically by the tracker service

# Utilities
clean: ## Clean build artifacts
	rm -rf $(BINARY_DIR)
	rm -f coverage.out coverage.html

install-tools: ## Install development tools
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest

# Release
release: ## Create a new release (requires VERSION variable)
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: VERSION variable is required. Usage: make release VERSION=v1.0.0"; \
		exit 1; \
	fi
	git tag $(VERSION)
	git push origin $(VERSION)

# Docker Hub

dockerhub-test: ## Test Docker Hub builds locally
	@echo "Testing Docker Hub builds..."
	@for service in $(SERVICES); do \
		echo "Testing build for $$service..."; \
		docker build -f Dockerfile.$$service -t test-sbs-$$service .; \
	done
	@echo "All Docker Hub builds tested successfully!"

dockerhub-push: ## Push to Docker Hub (requires DOCKERHUB_USERNAME and DOCKERHUB_TOKEN)
	@if [ -z "$(DOCKERHUB_USERNAME)" ] || [ -z "$(DOCKERHUB_TOKEN)" ]; then \
		echo "Error: DOCKERHUB_USERNAME and DOCKERHUB_TOKEN environment variables are required"; \
		echo "Usage: make dockerhub-push DOCKERHUB_USERNAME=youruser DOCKERHUB_TOKEN=yourtoken"; \
		exit 1; \
	fi
	@echo "Logging in to Docker Hub..."
	@echo "$(DOCKERHUB_TOKEN)" | docker login -u "$(DOCKERHUB_USERNAME)" --password-stdin
	@for service in $(SERVICES); do \
		echo "Building and pushing $$service to Docker Hub..."; \
		docker build -f Dockerfile.$$service -t $(DOCKERHUB_USERNAME)/sbs-$$service:$(VERSION) .; \
		docker tag $(DOCKERHUB_USERNAME)/sbs-$$service:$(VERSION) $(DOCKERHUB_USERNAME)/sbs-$$service:latest; \
		docker push $(DOCKERHUB_USERNAME)/sbs-$$service:$(VERSION); \
		docker push $(DOCKERHUB_USERNAME)/sbs-$$service:latest; \
	done
	@echo "Successfully pushed all images to Docker Hub!"

# Development workflow
dev-setup: deps install-tools ## Setup development environment
	@echo "Development environment setup complete!"

ci: fmt lint test build ## Run CI pipeline locally
	@echo "CI pipeline completed successfully!" 