.PHONY: build test lint docker-build

IMAGE_NAME  := contractpro/orch-api
IMAGE_TAG   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build ./cmd/orch-api/

test:
	go test ./...

lint:
	go vet ./...

docker-build:
	docker build \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		-t $(IMAGE_NAME):latest \
		.
