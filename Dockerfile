# Multi-stage Dockerfile for prism-monitor (Zero-CGO, minimal image)
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w -X main.version=docker" -o /prism-monitor ./cmd/prism-monitor

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /prism-monitor /app/prism-monitor

# 暴露默认端口
EXPOSE 21120

# 挂载点说明：
# -v /:/host:ro 配合 PRISM_PROC_ROOT=/host 时可监控宿主机
# -v /app/data:/data 配合 PRISM_DB=/data/prism.db 时持久化历史
ENV PRISM_PORT=21120 \
    PRISM_HOST=0.0.0.0 \
    PRISM_DB=/data/prism-monitor.db \
    PRISM_PROC_ROOT=/

VOLUME ["/data"]

ENTRYPOINT ["/app/prism-monitor"]
