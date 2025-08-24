# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
BINARY_NAME=innodb-parser
BINARY_UNIX=$(BINARY_NAME)_unix

# Test parameters
TEST_TIMEOUT=30s
TEST_COVERAGE_FILE=coverage.out
TEST_COVERAGE_HTML=coverage.html

.PHONY: all build clean test test-coverage test-race deps generate mock lint fmt vet

all: deps generate test build

build:
	$(GOBUILD) -o bin/$(BINARY_NAME) -v ./cmd/innodb-parser

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY_UNIX) -v ./cmd/innodb-parser

clean:
	$(GOCLEAN)
	rm -f bin/$(BINARY_NAME)
	rm -f bin/$(BINARY_UNIX)
	rm -f $(TEST_COVERAGE_FILE) $(TEST_COVERAGE_HTML)

test:
	$(GOTEST) -v -timeout $(TEST_TIMEOUT) ./...

test-short:
	$(GOTEST) -short -v ./...

test-coverage:
	$(GOTEST) -coverprofile=$(TEST_COVERAGE_FILE) -covermode=atomic ./...
	$(GOCMD) tool cover -html=$(TEST_COVERAGE_FILE) -o $(TEST_COVERAGE_HTML)

test-race:
	$(GOTEST) -race -short ./...

test-integration:
	$(GOTEST) -tags=integration -v ./test/integration/...

deps:
	$(GOMOD) download
	$(GOMOD) tidy

generate:
	$(GOCMD) generate ./...

mock:
	mockgen -source=internal/reader/interfaces.go -destination=internal/reader/mocks/reader_mock.go
	mockgen -source=internal/parser/interfaces.go -destination=internal/parser/mocks/parser_mock.go

lint:
	golangci-lint run

fmt:
	$(GOCMD) fmt ./...

vet:
	$(GOCMD) vet ./...

install:
	$(GOCMD) install ./cmd/innodb-parser

# TDD helpers
tdd-watch:
	@echo "Starting TDD watch mode..."
	@while true; do \
		make test-short; \
		inotifywait -qre modify --exclude='.*\.swp' .; \
	done

red:
	@echo "=== RED phase: Writing failing test ==="
	@make test || true

green:
	@echo "=== GREEN phase: Making tests pass ==="
	@make test

refactor:
	@echo "=== REFACTOR phase: Improving code ==="
	@make fmt vet lint test

help:
	@echo "Available targets:"
	@echo "  build          - Build the binary"
	@echo "  test           - Run all tests"
	@echo "  test-coverage  - Run tests with coverage"
	@echo "  test-race      - Run tests with race detection"
	@echo "  deps           - Download dependencies"
	@echo "  generate       - Run go generate"
	@echo "  mock           - Generate mocks"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run linter"
	@echo "  tdd-watch      - Start TDD watch mode"
	@echo "  red/green/refactor - TDD cycle helpers"