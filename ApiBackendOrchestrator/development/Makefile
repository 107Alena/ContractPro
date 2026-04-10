.PHONY: build test lint docker-build generate-jwt compose-up compose-down compose-clean compose-logs compose-ps compose-restart

IMAGE_NAME  := contractpro/orch-api
IMAGE_TAG   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# ---------------------------------------------------------------------------
# Build & Test
# ---------------------------------------------------------------------------

build:
	go build ./cmd/orch-api/

test:
	go test ./...

lint:
	go vet ./...

# ---------------------------------------------------------------------------
# Docker Image
# ---------------------------------------------------------------------------

docker-build:
	docker build \
		--build-arg VERSION=$(IMAGE_TAG) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		-t $(IMAGE_NAME):latest \
		.

# ---------------------------------------------------------------------------
# Local Development (Docker Compose)
# ---------------------------------------------------------------------------

## Generate RSA key pair for local JWT validation (run once).
generate-jwt:
	@bash scripts/generate-dev-jwt.sh

## Copy .env.example to .env if .env does not exist.
.env:
	@cp .env.example .env
	@echo "Created .env from .env.example — review and adjust if needed."

## Build images and start all services (rabbitmq, redis, minio, orch-api).
compose-up: .env generate-jwt
	docker compose up --build -d

## Stop and remove containers (preserves volumes).
compose-down:
	docker compose down

## Stop and remove containers AND volumes (clean slate).
compose-clean:
	docker compose down -v

## Follow orch-api logs.
compose-logs:
	docker compose logs -f orch-api

## Show container status.
compose-ps:
	docker compose ps

## Rebuild and restart only orch-api (infrastructure stays running).
compose-restart:
	docker compose up --build -d --no-deps orch-api
