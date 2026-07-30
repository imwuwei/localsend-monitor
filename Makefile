# LocalSend Monitor Makefile

BINARY_NAME=localsend-monitor
BINARY_DIR=dist
VERSION?=dev
BUILD_TIME?=$(shell date '+%Y-%m-%d_%H:%M:%S')
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")

# Static build - no CGO, no glibc dependency
CGO_ENABLED=0

# Go flags
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.Commit=$(COMMIT)"
GOFLAGS=-trimpath

.PHONY: all build clean test run

all: build

build:
	mkdir -p $(BINARY_DIR)
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME) .

clean:
	rm -rf $(BINARY_DIR)
	go clean

test:
	go test ./...

run: build
	./$(BINARY_DIR)/$(BINARY_NAME)

# Cross-compilation targets
build-linux-amd64:
	mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-amd64 .

build-linux-arm64:
	mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-arm64 .

build-linux-arm:
	mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-arm .

# Build all Linux platforms
build-all: build-linux-amd64 build-linux-arm64 build-linux-arm
