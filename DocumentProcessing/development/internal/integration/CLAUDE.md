# Integration Package — CLAUDE.md

End-to-end integration tests for Document Processing pipelines (build: `//go:build integration`).

## Main Components

- **testinfra.go** — Test infrastructure factories creating in-memory fakes for all ports: `captureBroker` (message capture/delivery), `recordingPublisher` (event recording), `memoryKVStore`, `memoryObjectStorage`, `fakeOCRClient`. Provides helpers to build a complete app stack for testing without external dependencies.
- **processing_pipeline_test.go** — End-to-end tests for processing pipeline: submit `ProcessDocumentCommand`, verify all stages execute (validate → fetch → OCR/text → structure → semantic tree), artifacts sent to DM, status/completion events published.
- **comparison_pipeline_test.go** — End-to-end tests for comparison pipeline: submit `CompareVersionsCommand`, simulate DM responses with two semantic trees, verify diff (text + structural), diff result and events published.

## Run Tests

```bash
# All tests (including integration):
make test

# Integration tests only:
go test ./internal/integration/ -tags=integration
```

## Test Pattern

1. Use `testinfra` factories to build in-memory app
2. Inject commands via `captureBroker.deliverToTopic()`
3. Verify published events in `recordingPublisher`
4. Assert state changes in `recordingPublisher`

## No External Dependencies

All integration tests use in-memory fakes: no RabbitMQ, Redis, S3, or Yandex Cloud required.
