# ============================================================================
# Stage 1: Build
# ============================================================================
FROM golang:1.26.1-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Cache dependency download as a separate layer.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source and build a fully static binary.
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -trimpath \
    -o /dp-worker \
    ./cmd/dp-worker/

# ============================================================================
# Stage 2: Production
# ============================================================================
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S dpworker \
    && adduser -S -G dpworker -H -D -s /sbin/nologin dpworker

COPY --from=builder /dp-worker /usr/local/bin/dp-worker

# Health probe port (configurable via DP_HTTP_PORT, default 8080).
EXPOSE 8080
# Metrics port (configurable via DP_METRICS_PORT, default 9090).
EXPOSE 9090

USER dpworker:dpworker

HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/readyz || exit 1

ENTRYPOINT ["/usr/local/bin/dp-worker"]
