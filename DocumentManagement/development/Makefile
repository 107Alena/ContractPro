.PHONY: build build-migrate test lint docker-build compose-up compose-down

IMAGE_NAME  := contractpro/dm-service
IMAGE_TAG   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build ./cmd/dm-service/

build-migrate:
	go build ./cmd/dm-migrate/

test:
	go test ./...

lint:
	go vet ./...

docker-build:
	docker build \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		-t $(IMAGE_NAME):latest \
		.

compose-up:
	docker compose up --build

compose-down:
	docker compose down
