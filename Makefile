BINARY := sptfy
BUILD_TAGS := keyring_nopassage
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-s -w \
	-X github.com/open-cli-collective/spotify-cli/internal/version.Version=$(VERSION) \
	-X github.com/open-cli-collective/spotify-cli/internal/version.Commit=$(COMMIT) \
	-X github.com/open-cli-collective/spotify-cli/internal/version.Date=$(DATE)"

.PHONY: build test test-cover lint fmt tidy deps check install clean

build:
	go build -tags $(BUILD_TAGS) $(LDFLAGS) -o bin/$(BINARY) ./cmd/sptfy

test:
	go test -tags $(BUILD_TAGS) ./...

test-cover:
	go test -tags $(BUILD_TAGS) -coverprofile=coverage.out ./...

lint:
	golangci-lint run --build-tags $(BUILD_TAGS)

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

tidy:
	go mod tidy
	git diff --exit-code go.mod go.sum

deps:
	go mod download
	go mod verify

check: tidy fmt lint test build

install:
	go install -tags $(BUILD_TAGS) ./cmd/sptfy

clean:
	rm -rf bin/ dist/ coverage.out
