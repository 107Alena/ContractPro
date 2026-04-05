# Configuration Package — CLAUDE.md

Environment-based configuration loading for Document Management service.

## Main Components

- **config.go** — `Config` struct (root) + `Load()` function. Loads `DM_`-prefixed env vars with `.env` fallback via godotenv. Validates required fields and returns typed Config with 18 nested sub-configs. Aggregated error listing all missing/invalid fields at once
- **sub_configs.go** — 18 nested config structs: DatabaseConfig, BrokerConfig (25 topic names), StorageConfig, KVStoreConfig, HTTPConfig, ConsumerConfig, IngestionConfig, IdempotencyConfig, RetryConfig, TimeoutConfig, CircuitBreakerConfig, RateLimitConfig, DLQConfig, OutboxConfig, RetentionConfig, WatchdogConfig, OrphanCleanupConfig, ObservabilityConfig

## Load Behavior

1. Loads `.env` file if present (ignores if missing)
2. Reads `DM_`-prefixed env vars, applying defaults for optional fields
3. Validates **required** fields:
   - `DM_DB_DSN` — PostgreSQL connection string
   - `DM_BROKER_ADDRESS` — RabbitMQ address
   - `DM_STORAGE_ENDPOINT`, `DM_STORAGE_BUCKET`, `DM_STORAGE_ACCESS_KEY`, `DM_STORAGE_SECRET_KEY`
   - `DM_KVSTORE_ADDRESS` — Redis address
4. Validates constraints: port collisions, positive durations, concurrency ≥ 1, etc.
5. Returns aggregated validation error listing all issues

## Usage

```go
cfg, err := config.Load()  // reads env, returns *Config or error
if err != nil {
    // err lists all missing required fields and constraint violations
    log.Fatal(err)
}
```

## See Also

Full env var reference (94 variables, 18 groups): `architecture/configuration.md`
