# Configuration Package — CLAUDE.md

Environment-based configuration loading for Document Processing service.

## Main Components

- **config.go** — `Config` struct (root) + `Load()` function. Loads `DP_`-prefixed env vars with `.env` fallback. Validates required fields and returns typed Config with nested sub-configs.
- **sub_configs.go** — Nested config structs: `BrokerConfig`, `StorageConfig`, `OCRConfig`, `KVStoreConfig`, `LimitsConfig`, `ConcurrencyConfig`, `IdempotencyConfig`, `RetryConfig`, `HTTPConfig`, `ObservabilityConfig`.

## Load Behavior

1. Loads `.env` file if present (ignores if missing)
2. Reads `DP_`-prefixed env vars, applying defaults for optional fields
3. Validates **required** fields:
   - `DP_BROKER_ADDRESS` — RabbitMQ broker address
   - `DP_STORAGE_ENDPOINT`, `DP_STORAGE_BUCKET`, `DP_STORAGE_ACCESS_KEY`, `DP_STORAGE_SECRET_KEY`
   - `DP_OCR_ENDPOINT`, `DP_OCR_API_KEY`, `DP_OCR_FOLDER_ID`
   - `DP_KVSTORE_ADDRESS` — Redis address
4. Returns aggregated validation error listing all missing fields

## Usage

```go
cfg, err := Load()  // reads env, returns *Config or error
if err != nil {
    // err lists all missing required fields at once
    log.Fatal(err)
}
```

## See Also

Detailed env var reference (defaults, descriptions): `architecture/configuration.md`
