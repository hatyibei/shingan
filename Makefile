GOA     ?= $(HOME)/go/bin/goa
DESIGN  := github.com/hatyibei/shingan/design
BIN_DIR := /tmp

.PHONY: gen test build-all bench bench-rules

## gen: regenerate goa HTTP handlers and OpenAPI spec from design/design.go
gen:
	$(GOA) gen $(DESIGN)

## test: run all tests with race detector
test:
	go test -race ./...

## build-all: build CLI and API binaries
build-all:
	go build -o $(BIN_DIR)/shingan       ./cmd/shingan
	go build -o $(BIN_DIR)/shingan-api   ./cmd/api

## bench: run all benchmarks (excludes normal tests)
bench:
	go test -bench=. -benchmem -run=^$$ ./...

## bench-rules: run only domain/rules benchmarks
bench-rules:
	go test -bench=. -benchmem -run=^$$ ./domain/rules/...
