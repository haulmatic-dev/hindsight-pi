.PHONY: build clean test run lint fmt

# Variables
BINARY_NAME=hydra
WORKER_BINARY=hydra-worker
GO=go
GOFLAGS=-v

# Build commands
build:
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME) cmd/hydra/main.go
	$(GO) build $(GOFLAGS) -o bin/$(WORKER_BINARY) cmd/worker/main.go

build-hydra:
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME) cmd/hydra/main.go

build-worker:
	$(GO) build $(GOFLAGS) -o bin/$(WORKER_BINARY) cmd/worker/main.go

# Run commands
run:
	$(GO) run cmd/hydra/main.go

run-debug:
	$(GO) run cmd/hydra/main.go -debug

run-worker:
	HYDRA_BEAD_ID=test-001 HYDRA_PROJECT=test HYDRA_REPO= \
	HYDRA_AGENT_TYPE=orchestrator $(GO) run cmd/worker/main.go

# Test commands
test:
	$(GO) test -v ./...

test-coverage:
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

# Development commands
dev:
	air -c .air.toml

fmt:
	$(GO) fmt ./...

lint:
	golangci-lint run

vet:
	$(GO) vet ./...

# Clean
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Install dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

# Initialize
init: deps
	mkdir -p bin

# Help
help:
	@echo "Available targets:"
	@echo "  build        - Build both hydra and worker binaries"
	@echo "  build-hydra  - Build hydra binary only"
	@echo "  build-worker - Build worker binary only"
	@echo "  run          - Run hydra in development mode"
	@echo "  run-debug    - Run hydra with debug logging"
	@echo "  test         - Run all tests"
	@echo "  fmt          - Format Go code"
	@echo "  lint         - Run linter"
	@echo "  clean        - Clean build artifacts"
	@echo "  deps         - Download and tidy dependencies"
	@echo "  init         - Initialize project (deps + mkdir)"
