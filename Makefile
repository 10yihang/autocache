# AutoCache Makefile

.PHONY: all build build-clipboard build-clipboard-frontend build-operator run run-clipboard test test-clipboard test-unit test-integration test-benchmark lint fmt vet docker-build docker-run kind-create kind-delete kind-load generate manifests install-crd clean redis-benchmark help

# Variables
BINARY_NAME=autocache
DOCKER_IMAGE=autocache:latest
GO=go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS=-s -w -X github.com/10yihang/autocache/internal/version.Version=$(VERSION)
GOFLAGS=-ldflags="$(LDFLAGS)"

# Default target
all: build

# Build
build:
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME) ./cmd/server

build-clipboard: build-clipboard-frontend
	$(GO) build $(GOFLAGS) -o bin/clipboard ./cmd/clipboard

build-clipboard-frontend:
	npm --prefix clipboard/frontend install
	npm --prefix clipboard/frontend run build
	rm -rf clipboard/backend/embed/*
	cp -R clipboard/frontend/dist/. clipboard/backend/embed/

build-operator:
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME)-operator ./cmd/operator

# Run
run:
	$(GO) run ./cmd/server

run-clipboard: build-clipboard-frontend
	$(GO) run ./cmd/clipboard

# Test
test:
	$(GO) test -v -race -cover ./...

test-clipboard:
	$(GO) test -v -race ./clipboard/backend/... ./cmd/clipboard/...

test-unit:
	$(GO) test -v -race -cover ./internal/...

test-integration:
	$(GO) test -v -tags=integration ./test/integration/...

test-benchmark:
	$(GO) test -bench=. -benchmem ./...

# Code quality
lint:
	golangci-lint run ./...

fmt:
	$(GO) fmt ./...
	goimports -w .

vet:
	$(GO) vet ./...

# Docker
docker-build:
	docker build -t $(DOCKER_IMAGE) -f deploy/docker/Dockerfile .

docker-run:
	docker run -p 6379:6379 $(DOCKER_IMAGE)

# Kind
kind-create:
	kind create cluster --name autocache-dev --config scripts/kind-config.yaml

kind-delete:
	kind delete cluster --name autocache-dev

kind-load:
	kind load docker-image $(DOCKER_IMAGE) --name autocache-dev

# Operator
generate:
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."

manifests:
	controller-gen crd rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

install-crd:
	kubectl apply -f config/crd/bases

deploy: manifests
	kubectl apply -k config/default

undeploy:
	kubectl delete -k config/default

# Clean
clean:
	rm -rf bin/
	$(GO) clean

# Redis benchmark test
redis-benchmark:
	redis-benchmark -h 127.0.0.1 -p 6379 -n 100000 -c 50 -t get,set -q

# Help
help:
	@echo "Available targets:"
	@echo "  build           - Build the server binary"
	@echo "                    version=$(VERSION) (override with 'make build VERSION=vX.Y.Z')"
	@echo "  build-clipboard - Build the clipboard binary"
	@echo "  build-clipboard-frontend - Build and copy frontend assets"
	@echo "  build-operator  - Build the operator binary"
	@echo "  run             - Run the server"
	@echo "  run-clipboard   - Run the clipboard app"
	@echo "  test            - Run all tests"
	@echo "  test-clipboard  - Run clipboard app tests"
	@echo "  test-unit       - Run unit tests"
	@echo "  test-benchmark  - Run benchmark tests"
	@echo "  lint            - Run linter"
	@echo "  fmt             - Format code"
	@echo "  docker-build    - Build Docker image"
	@echo "  kind-create     - Create Kind cluster"
	@echo "  redis-benchmark - Run redis-benchmark"
	@echo "  deploy          - Deploy operator to cluster"
	@echo "  undeploy        - Remove operator from cluster"
	@echo "  clean           - Clean build artifacts"
