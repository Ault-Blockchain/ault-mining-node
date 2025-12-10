.PHONY: all build install clean test run docker-build docker-run help

# Binary name
BINARY_NAME=aultmined
DOCKER_IMAGE=ault-miner

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod

# Build flags
LDFLAGS=-ldflags "-s -w"
BUILD_FLAGS=-trimpath

# Default target
all: build

## help: Show this help message
help:
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## build: Build the miner binary
build:
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o $(BINARY_NAME) ./cmd

## install: Install the miner binary to $GOPATH/bin
install: build
	@echo "Installing $(BINARY_NAME) to $$(go env GOPATH)/bin..."
	@mkdir -p $$(go env GOPATH)/bin
	@mv $(BINARY_NAME) $$(go env GOPATH)/bin/

## clean: Remove build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f miner.log
	rm -rf data/

## test: Run unit tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race ./...

## test-short: Run short tests only
test-short:
	@echo "Running short tests..."
	$(GOTEST) -v -short ./...

## bench: Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

## coverage: Generate test coverage report
coverage:
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

## verify: Verify dependencies
verify:
	@echo "Verifying dependencies..."
	$(GOMOD) verify

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

## vet: Run go vet
vet:
	@echo "Running vet..."
	$(GOCMD) vet ./...

## lint: Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not found, installing..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

## run: Run the miner (requires env vars)
run: build
	@echo "Starting miner..."
	@echo "Required env vars: MINER_OPERATOR_KEY, MINER_VRF_KEY"
	./$(BINARY_NAME) mine

## run-debug: Run the miner in debug mode
run-debug: build
	@echo "Starting miner in debug mode..."
	./$(BINARY_NAME) mine --log-level debug

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):latest .

## docker-run: Run miner in Docker (pass env vars)
docker-run:
	@echo "Running miner in Docker..."
	@echo "Example: docker run -e MINER_OPERATOR_KEY=... -e MINER_VRF_KEY=... $(DOCKER_IMAGE):latest"
	docker run -it --rm $(DOCKER_IMAGE):latest

## docker-push: Push Docker image to registry
docker-push: docker-build
	@echo "Pushing Docker image..."
	docker tag $(DOCKER_IMAGE):latest $(DOCKER_IMAGE):$(shell git rev-parse --short HEAD)
	docker push $(DOCKER_IMAGE):latest
	docker push $(DOCKER_IMAGE):$(shell git rev-parse --short HEAD)

## docker-compose-up: Start miner with docker-compose
docker-compose-up:
	@echo "Starting with docker-compose..."
	docker-compose up -d

## docker-compose-down: Stop miner with docker-compose
docker-compose-down:
	@echo "Stopping docker-compose..."
	docker-compose down

## docker-compose-logs: View docker-compose logs
docker-compose-logs:
	docker-compose logs -f

## local-node: Start local test node
local-node:
	@echo "Starting local test node..."
	cd ../.. && ./local_node.sh -y

## dev: Run miner against local node
dev: build
	@echo "Running against local node..."
	./$(BINARY_NAME) mine --log-level debug

# Check if all tools are installed
check-tools:
	@echo "Checking required tools..."
	@which go > /dev/null || (echo "Go not found" && exit 1)
	@echo "Go found: $$(go version)"
	@which docker > /dev/null || echo "Docker not found (optional)"
	@which golangci-lint > /dev/null || echo "golangci-lint not found (optional)"

# Setup development environment
setup: check-tools deps
	@echo "Setting up development environment..."
	@echo "Setup complete!"
	@echo ""
	@echo "Required environment variables:"
	@echo "  MINER_OPERATOR_KEY  - Operator private key (hex)"
	@echo "  MINER_VRF_KEY       - VRF private key (hex)"
	@echo ""
	@echo "Optional environment variables:"
	@echo "  CHAIN_GRPC          - gRPC endpoint (default: localhost:9090)"
	@echo "  CHAIN_RPC           - RPC endpoint (default: localhost:26657)"
	@echo "  MINER_API_PORT      - API server port (default: 8080)"
	@echo ""
	@echo "Start mining: make run"
