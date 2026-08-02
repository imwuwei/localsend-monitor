# Stage 1: 编译阶段（基于 Debian）
# 必须声明 BUILDPLATFORM，否则 --platform=$BUILDPLATFORM 不生效
ARG BUILDPLATFORM
FROM --platform=${BUILDPLATFORM} golang:1.26-bookworm AS builder

WORKDIR /build

# 先复制依赖文件，利用 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 获取版本信息
ARG VERSION=dev
ARG BUILD_TIME
ARG COMMIT
ARG TARGETARCH

RUN if [ -z "$BUILD_TIME" ]; then BUILD_TIME=$(date '+%Y-%m-%d_%H:%M:%S'); fi && \
    if [ -z "$COMMIT" ]; then COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none"); fi && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.Commit=${COMMIT}" \
    -o localsend-monitor .

# Stage 2: 运行阶段（基于 Debian Slim）
FROM debian:bookworm-slim
ENV TZ=Asia/Shanghai
COPY --from=builder /build/localsend-monitor /usr/local/bin/localsend-monitor
ENTRYPOINT ["/usr/local/bin/localsend-monitor"]