GOA     ?= $(HOME)/go/bin/goa
DESIGN  := github.com/hatyibei/shingan/design
BIN_DIR := /tmp

.PHONY: gen test build-all bench bench-rules gen-cli sample-% check-reason lint

## gen: regenerate goa HTTP handlers and OpenAPI spec from design/design.go
gen:
	$(GOA) gen $(DESIGN)

## test: run all tests with race detector
test:
	go test -race ./...

## check-reason: fail when a domain.Finding literal in domain/rules omits ConfidenceReason (ADR-008)
check-reason:
	@./scripts/check_confidence_reason.sh

## lint: run go vet and the ConfidenceReason check together
lint: check-reason
	go vet ./...

## build-all: build CLI and API binaries
build-all:
	go build -o $(BIN_DIR)/shingan       ./cmd/shingan
	go build -o $(BIN_DIR)/shingan-api   ./cmd/api

## gen-cli: build shingan-gen sample generator CLI
gen-cli:
	go build -o $(BIN_DIR)/shingan-gen ./cmd/shingan-gen

## sample-%: generate a sample workflow (e.g. make sample-buggy, make sample-clean)
sample-%: gen-cli
	@$(BIN_DIR)/shingan-gen --pattern $* --seed 42

## bench: run all benchmarks (excludes normal tests)
bench:
	go test -bench=. -benchmem -run=^$$ ./...

## bench-rules: run only domain/rules benchmarks
bench-rules:
	go test -bench=. -benchmem -run=^$$ ./domain/rules/...
