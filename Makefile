# LocalSend Monitor Makefile

BINARY_NAME=localsend-monitor
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
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME) .

clean:
	rm -f $(BINARY_NAME)
	go clean

test:
	go test ./...

run: build
	./$(BINARY_NAME)

# Cross-compilation targets
build-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 .

build-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME)-linux-arm64 .

build-linux-arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME)-linux-arm .

build-darwin-amd64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME)-darwin-amd64 .

build-darwin-arm64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME)-darwin-arm64 .

build-windows-amd64:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe .

# Build all platforms
build-all: build-linux-amd64 build-linux-arm64 build-linux-arm build-darwin-amd64 build-darwin-arm64 build-windows-amd64