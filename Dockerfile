# ---- 构建阶段 ----
FROM golang:1.26-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build

# ---- 运行阶段 ----
FROM debian:bookworm-slim
COPY --from=builder /app/localsend-monitor /usr/local/bin/localsend-monitor

ENTRYPOINT ["localsend-monitor"]