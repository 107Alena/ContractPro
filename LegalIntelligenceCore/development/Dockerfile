# ============================================================================
# Stage 1: Build
# ============================================================================
FROM golang:1.26.1-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /workspace

# Cache dependency layer: copy manifests first, then download + verify checksums.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source and build a fully static, stripped, reproducible binary.
COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -trimpath \
    -o /out/lic-service \
    ./cmd/lic-service/

# ============================================================================
# Stage 2: Runtime (distroless, non-root)
# ============================================================================
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

# CA bundle for outbound HTTPS to LLM providers (Claude/OpenAI/Gemini).
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=builder /out/lic-service /lic-service

# HTTP port for /healthz, /readyz, /metrics (LIC_HTTP_PORT, default 8080).
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/lic-service"]
