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
    -o /dm-service \
    ./cmd/dm-service/

# ============================================================================
# Stage 2: Production
# ============================================================================
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S dmservice \
    && adduser -S -G dmservice -H -D -s /sbin/nologin dmservice

COPY --from=builder /dm-service /usr/local/bin/dm-service

# API + health probe port (configurable via DM_HTTP_PORT, default 8080).
EXPOSE 8080
# Metrics port (configurable via DM_METRICS_PORT, default 9090).
EXPOSE 9090

USER dmservice:dmservice

HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/readyz || exit 1

ENTRYPOINT ["/usr/local/bin/dm-service"]
