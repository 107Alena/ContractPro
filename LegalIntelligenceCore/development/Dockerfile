# Skeleton Dockerfile — LIC-TASK-001 scaffolding.
#
# The full distroless/nonroot multi-stage image (CA-certs, fixed ldflags, sub-50MB,
# distroless/static-debian12:nonroot base) is the deliverable of LIC-TASK-003.
# This stub only proves that `make docker-build` is wired correctly and produces
# a runnable image consistent with the rest of the repo (DP).

FROM golang:1.26.1-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

COPY go.mod ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -trimpath \
    -o /lic-service \
    ./cmd/lic-service/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S licservice \
    && adduser -S -G licservice -H -D -s /sbin/nologin licservice

COPY --from=builder /lic-service /usr/local/bin/lic-service

# Health/metrics port (LIC_HTTP_PORT, default 8080). Wired up by LIC-TASK-009.
EXPOSE 8080

USER licservice:licservice

ENTRYPOINT ["/usr/local/bin/lic-service"]
