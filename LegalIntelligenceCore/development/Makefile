.PHONY: build test lint docker-build

IMAGE_NAME  := contractpro/lic-service
IMAGE_TAG   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VERSION     ?= $(IMAGE_TAG)

build:
	mkdir -p bin
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/lic-service ./cmd/lic-service/

test:
	go test ./...

lint:
	go vet ./...

docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		-t $(IMAGE_NAME):latest \
		.
