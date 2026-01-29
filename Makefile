.PHONY: build build-control-plane build-regional-client build-agent clean run-control-plane run-regional-client test deps

# Variables
BINARY_DIR=bin
CONTROL_PLANE_BINARY=$(BINARY_DIR)/control-plane
REGIONAL_CLIENT_BINARY=$(BINARY_DIR)/regional-client
AGENT_BINARY=$(BINARY_DIR)/agent

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build all binaries
build: build-control-plane build-regional-client build-agent

# Build control plane
build-control-plane:
	@echo "Building control plane..."
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -o $(CONTROL_PLANE_BINARY) cmd/control-plane/main.go
	@echo "Control plane built: $(CONTROL_PLANE_BINARY)"

# Build regional client
build-regional-client:
	@echo "Building regional client..."
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -o $(REGIONAL_CLIENT_BINARY) cmd/regional-client/main.go
	@echo "Regional client built: $(REGIONAL_CLIENT_BINARY)"

# Build agent
build-agent:
	@echo "Building agent..."
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -o $(AGENT_BINARY) cmd/agent/main.go
	@echo "Agent built: $(AGENT_BINARY)"

# Build static binaries for Linux deployment
build-static:
	@echo "Building static binaries for Linux..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -a -ldflags '-extldflags "-static"' -o $(CONTROL_PLANE_BINARY)-linux cmd/control-plane/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -a -ldflags '-extldflags "-static"' -o $(REGIONAL_CLIENT_BINARY)-linux cmd/regional-client/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -a -ldflags '-extldflags "-static"' -o $(AGENT_BINARY)-linux cmd/agent/main.go
	@echo "Static binaries built for Linux"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BINARY_DIR)
	@echo "Clean complete"

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "Dependencies downloaded"

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -cover ./...
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run control plane
run-control-plane:
	@echo "Starting control plane..."
	ETCD_ENDPOINTS=localhost:2379 API_PORT=8080 $(GOCMD) run cmd/control-plane/main.go

# Run regional client (dc1)
run-regional-client:
	@echo "Starting regional client (dc1)..."
	REGION_ID=dc1 ETCD_ENDPOINTS=localhost:2379 API_PORT=8081 $(GOCMD) run cmd/regional-client/main.go

# Run regional client (dc2)
run-regional-client-dc2:
	@echo "Starting regional client (dc2)..."
	REGION_ID=dc2 ETCD_ENDPOINTS=localhost:2379 API_PORT=8082 $(GOCMD) run cmd/regional-client/main.go

# Run agent
run-agent:
	@echo "Starting agent..."
	REGIONAL_CLIENT_URL=http://localhost:8081 $(GOCMD) run cmd/agent/main.go

# Start etcd with Docker
start-etcd:
	@echo "Starting etcd..."
	docker run -d --name lpmos-etcd \
		-p 2379:2379 \
		-p 2380:2380 \
		quay.io/coreos/etcd:v3.5.12 \
		/usr/local/bin/etcd \
		--advertise-client-urls http://0.0.0.0:2379 \
		--listen-client-urls http://0.0.0.0:2379
	@echo "etcd started on localhost:2379"

# Stop etcd
stop-etcd:
	@echo "Stopping etcd..."
	docker stop lpmos-etcd || true
	docker rm lpmos-etcd || true
	@echo "etcd stopped"

# Full demo setup
demo: start-etcd
	@echo "Waiting for etcd to be ready..."
	@sleep 3
	@echo ""
	@echo "================================================"
	@echo "LPMOS Demo Environment Ready!"
	@echo "================================================"
	@echo ""
	@echo "Next steps:"
	@echo "1. Terminal 1: make run-control-plane"
	@echo "2. Terminal 2: make run-regional-client"
	@echo "3. Create a task with: curl -X POST http://localhost:8080/api/v1/tasks ..."
	@echo ""
	@echo "To stop: make stop-etcd"
	@echo ""

# Format code
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run ./...

# Generate mocks (if using mockgen)
mocks:
	@echo "Generating mocks..."
	mockgen -source=pkg/etcd/client.go -destination=pkg/etcd/mocks/client_mock.go

# Help
help:
	@echo "LPMOS Makefile Commands:"
	@echo ""
	@echo "  make build                 - Build all binaries"
	@echo "  make build-control-plane   - Build control plane only"
	@echo "  make build-regional-client - Build regional client only"
	@echo "  make build-agent          - Build agent only"
	@echo "  make build-static         - Build static Linux binaries"
	@echo ""
	@echo "  make clean                - Remove build artifacts"
	@echo "  make deps                 - Download dependencies"
	@echo "  make test                 - Run tests"
	@echo "  make test-coverage        - Run tests with coverage"
	@echo ""
	@echo "  make run-control-plane    - Run control plane"
	@echo "  make run-regional-client  - Run regional client (dc1)"
	@echo "  make run-agent            - Run agent"
	@echo ""
	@echo "  make start-etcd           - Start etcd with Docker"
	@echo "  make stop-etcd            - Stop etcd"
	@echo "  make demo                 - Setup demo environment"
	@echo ""
	@echo "  make fmt                  - Format code"
	@echo "  make lint                 - Lint code"
	@echo ""
