# LocalSend Monitor Makefile

BINARY_NAME=localsend-monitor
BINARY_DIR=dist
VERSION?=$(shell date '+%y.%m.%d')
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

# Docker
DOCKER_REPO=imwww/localsend-monitor
DOCKER_TAG?=latest
DOCKER_PLATFORMS=linux/amd64,linux/arm64,linux/arm

# 单架构构建（当前平台，向后兼容）
docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(DOCKER_REPO):$(DOCKER_TAG) .

# 多架构本地构建（不推送，仅验证编译）
docker-buildx:
	docker buildx build \
		--platform $(DOCKER_PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(DOCKER_REPO):$(DOCKER_TAG) .

# 多架构构建并推送（latest 标签）
docker-pushx:
	docker buildx build \
		--platform $(DOCKER_PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(DOCKER_REPO):$(DOCKER_TAG) \
		--push .

# 多架构构建并推送（latest + version 标签）
docker-release:
	docker buildx build \
		--platform $(DOCKER_PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(DOCKER_REPO):$(DOCKER_TAG) \
		-t $(DOCKER_REPO):$(VERSION) \
		--push .
