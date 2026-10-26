.DEFAULT_GOAL := all

CLI_OUTPUT ?= bin/gelato

# CGO_ENABLED=0 makes the build fail if any dependency pulls in cgo.
build:
	@CGO_ENABLED=0 go build -o "$(CLI_OUTPUT)" ./cmd/gelato
.PHONY: build

test:
	@go test -race ./...
.PHONY: test

coverage:
	@go test -race -coverprofile=coverage.out ./...
.PHONY: coverage

show: coverage
	@go tool cover -html=coverage.out
.PHONY: show

lint:
	@pre-commit run --all-files golangci-lint-full
.PHONY: lint

verify:
	@pre-commit run --all-files golangci-lint-config-verify
.PHONY: verify

fmt:
	@pre-commit run --all-files golangci-lint-fmt
.PHONY: fmt

lint-all:
	@pre-commit run --all-files
.PHONY: lint-all

all: test lint
.PHONY: all
