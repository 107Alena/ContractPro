# Configuration — Legal Intelligence Core

Полный справочник env-переменных LIC. Все имена с префиксом `LIC_`. Конфигурация загружается через `godotenv` из `.env` (для local dev) и через стандартный `os.Getenv` (production / staging — env injection из Yandex Lockbox / Kubernetes secrets).

---

## 1. Общие принципы

- **Все** env-переменные с префиксом `LIC_`.
- Required переменные — без default; при их отсутствии сервис **не стартует** (fail-fast).
- Optional переменные — с разумным default'ом.
- Boolean: `true` / `false` (case-insensitive).
- Duration: Go format (`30s`, `5m`, `24h`).
- Списки: comma-separated.
- Numbers: десятичное представление.

---

## 2. Категории переменных

### 2.1 Application

| Var | Required | Default | Описание |
|-----|----------|---------|----------|
| `LIC_LOG_LEVEL` | no | `info` | Уровень логирования: `debug`, `info`, `warn`, `error` |
| `LIC_ENV` | no | `local` | Среда: `local`, `dev`, `staging`, `production` |
| `LIC_HTTP_PORT` | no | `8080` | Порт для health/readyz/metrics |
| `LIC_SHUTDOWN_TIMEOUT` | no | `120s` | Graceful shutdown deadline |

### 2.2 RabbitMQ

| Var | Required | Default | Описание |
|-----|----------|---------|----------|
| `LIC_BROKER_URL` | yes | — | AMQP URL (`amqps://user:pass@host:5671/vhost`) |
| `LIC_BROKER_EXCHANGE_EVENTS` | no | `contractpro.events` | Topic exchange для events |
| `LIC_BROKER_EXCHANGE_RESPONSES` | no | `contractpro.responses` | Topic exchange для responses |
| `LIC_BROKER_EXCHANGE_COMMANDS` | no | `contractpro.commands` | Topic exchange для commands |
| `LIC_BROKER_EXCHANGE_DLX` | no | `contractpro.dlx` | DLX exchange |
| `LIC_CONSUMER_PREFETCH` | no | `10` | RabbitMQ consumer prefetch count |
| `LIC_CONSUMER_MAX_REDELIVERIES` | no | `3` | Max сообщение redeliveries до DLQ |
| `LIC_PUBLISHER_CONFIRM_TIMEOUT` | no | `5s` | Publisher confirm timeout |
| `LIC_PUBLISH_BUFFER_SIZE` | no | `100` | In-memory публикационный buffer (при отказе broker) |

### 2.3 Redis

| Var | Required | Default | Описание |
|-----|----------|---------|----------|
| `LIC_REDIS_URL` | yes | — | Redis connection URL (`redis://...` или `rediss://...`) |
| `LIC_REDIS_DB` | no | `0` | Logical DB index (0–15) |
| `LIC_REDIS_PASSWORD` | no | `` | AUTH password (если не в URL) |
| `LIC_REDIS_TLS` | no | `false` | Use TLS |
| `LIC_REDIS_POOL_SIZE` | no | `10` | Connection pool size |
| `LIC_REDIS_DIAL_TIMEOUT` | no | `2s` | Connection timeout |

### 2.4 Idempotency

| Var | Required | Default | Описание |
|-----|----------|---------|----------|
| `LIC_IDEMPOTENCY_TTL` | no | `24h` | TTL ключей идемпотентности |
| `LIC_IDEMPOTENCY_PROCESSING_TTL` | no | `90s` | TTL для статуса `PROCESSING` |
| `LIC_IDEMPOTENCY_FALLBACK_ENABLED` | no | `false` | Fallback на DB-based проверку при недоступности Redis (DB у LIC нет — фактический эффект: ack без проверки + alert) |

### 2.5 Pipeline orchestration

| Var | Required | Default | Описание |
|-----|----------|---------|----------|
| `LIC_PIPELINE_CONCURRENCY` | no | `5` | Max параллельных jobs в инстансе |
| `LIC_JOB_TIMEOUT` | no | `90s` | Job-level overall timeout |
| `LIC_DM_REQUEST_TIMEOUT` | no | `30s` | Timeout async-запроса артефактов у DM |
| `LIC_DM_PERSIST_CONFIRM_TIMEOUT` | no | `30s` | Timeout ожидания DM-подтверждения после публикации artifacts |
| `LIC_PENDING_CONFIRMATION_TTL` | no | `25h` | TTL pending-state в Redis (ASSUMPTION-LIC-05) |

### 2.6 LLM providers — общее

| Var | Required | Default | Описание |
|-----|----------|---------|----------|
| `LIC_PROVIDER_FALLBACK_ORDER` | no | `claude,openai,gemini` | Порядок fallback провайдеров |
| `LIC_LLM_REQUEST_TIMEOUT` | no | `60s` | Per-request HTTP timeout |
| `LIC_LLM_CONCURRENCY_PER_PROVIDER` | no | `10` | Max concurrent calls per provider |

### 2.7 LLM providers — Claude

| Var | Required | Default | Описание |
|-----|----------|---------|----------|
| `LIC_CLAUDE_API_KEY` | yes (если Claude в FALLBACK) | — | Anthropic API key |
| `LIC_CLAUDE_API_BASE_URL` | no | `https://api.anthropic.com` | API endpoint |
| `LIC_CLAUDE_MODEL` | no | `claude-sonnet-4-6` | Default model для всех агентов на Claude |
| `LIC_CLAUDE_RPS` | no | `10` | Token bucket RPS |
| `LIC_CLAUDE_BURST` | no | `20` | Token bucket burst |
| `LIC_CLAUDE_PROMPT_CACHE_ENABLED` | no | `true` | Использовать Anthropic Prompt Caching |

### 2.8 LLM providers — OpenAI

| Var | Required | Default | Описание |
|-----|----------|---------|----------|
| `LIC_OPENAI_API_KEY` | yes (если OpenAI в FALLBACK) | — | OpenAI API key |
| `LIC_OPENAI_API_BASE_URL` | no | `https://api.openai.com` | API endpoint |
| `LIC_OPENAI_MODEL` | no | `gpt-4.1` | Default model |
| `LIC_OPENAI_RPS` | no | `20` | Token bucket RPS |
| `LIC_OPENAI_BURST` | no | `40` | Token bucket burst |

### 2.9 LLM providers — Gemini

| Var | Required | Default | Описание |
|-----|----------|---------|----------|
| `LIC_GEMINI_API_KEY` | yes (если Gemini в FALLBACK) | — | Google AI Studio key |
| `LIC_GEMINI_API_BASE_URL` | no | `https://generativelanguage.googleapis.com` | API endpoint |
| `LIC_GEMINI_MODEL` | no | `gemini-2.5-pro` | Default model |
| `LIC_GEMINI_RPS` | no | `20` | Token bucket RPS |
| `LIC_GEMINI_BURST` | no | `40` | Token bucket burst |

### 2.10 Per-agent provider override

Per-agent выбор primary провайдера (см. ADR-LIC-03):

| Var | Default | Описание |
|-----|---------|----------|
| `LIC_AGENT_TYPE_CLASSIFIER_PROVIDER` | `claude` | |
| `LIC_AGENT_KEY_PARAMS_PROVIDER` | `claude` | |
| `LIC_AGENT_PARTY_CONSISTENCY_PROVIDER` | `claude` | |
| `LIC_AGENT_MANDATORY_CONDITIONS_PROVIDER` | `claude` | |
| `LIC_AGENT_RISK_DETECTION_PROVIDER` | `claude` | |
| `LIC_AGENT_RECOMMENDATION_PROVIDER` | `claude` | |
| `LIC_AGENT_SUMMARY_PROVIDER` | `claude` | |
| `LIC_AGENT_DETAILED_REPORT_PROVIDER` | `claude` | |
| `LIC_AGENT_RISK_DELTA_PROVIDER` | `claude` | |

### 2.11 Per-agent timeouts

| Var | Default | Описание |
|-----|---------|----------|
| `LIC_AGENT_TYPE_CLASSIFIER_TIMEOUT` | `5s` | |
| `LIC_AGENT_KEY_PARAMS_TIMEOUT` | `8s` | |
| `LIC_AGENT_PARTY_CONSISTENCY_TIMEOUT` | `6s` | |
| `LIC_AGENT_MANDATORY_CONDITIONS_TIMEOUT` | `8s` | |
| `LIC_AGENT_RISK_DETECTION_TIMEOUT` | `12s` | |
| `LIC_AGENT_RECOMMENDATION_TIMEOUT` | `10s` | |
| `LIC_AGENT_SUMMARY_TIMEOUT` | `6s` | |
| `LIC_AGENT_DETAILED_REPORT_TIMEOUT` | `12s` | |
| `LIC_AGENT_RISK_DELTA_TIMEOUT` | `8s` | |

### 2.12 Classification и pipeline poltics

| Var | Default | Описание |
|-----|---------|----------|
| `LIC_CONFIDENCE_THRESHOLD` | `0.75` | Порог confidence для запроса подтверждения типа (FR-2.1.3) |
| `LIC_MAX_INPUT_TOKENS` | `150000` | Глобальный лимит токенов на входе пайплайна |
| `LIC_MAX_AGENT_INPUT_TOKENS` | `120000` | Лимит на агентский вызов (с усечением выше) |

### 2.13 Aggregate score weights

```env
LIC_SCORE_WEIGHT_HIGH=25
LIC_SCORE_WEIGHT_MEDIUM=10
LIC_SCORE_WEIGHT_LOW=3
LIC_SCORE_WEIGHT_MISSING_MANDATORY=15
LIC_SCORE_WEIGHT_AMBIGUOUS_MANDATORY=5
LIC_SCORE_LABEL_LOW_THRESHOLD=0.75
LIC_SCORE_LABEL_MEDIUM_THRESHOLD=0.45
```

См. high-architecture §6.11.

### 2.14 Observability

| Var | Default | Описание |
|-----|---------|----------|
| `LIC_OTEL_EXPORTER_OTLP_ENDPOINT` | (none) | OTLP gRPC endpoint (напр., `otel-collector:4317`) |
| `LIC_OTEL_EXPORTER_OTLP_INSECURE` | `false` | Без TLS (для dev) |
| `LIC_OTEL_TRACES_SAMPLER` | `parentbased_traceidratio` | Sampler |
| `LIC_OTEL_TRACES_SAMPLER_ARG` | `0.1` | 10% sampling в production |
| `LIC_OTEL_SERVICE_NAME` | `lic-service` | Имя сервиса в trace |
| `LIC_METRICS_PATH` | `/metrics` | Prometheus scrape path |

### 2.15 Pricing table

| Var | Default | Описание |
|-----|---------|----------|
| `LIC_PRICING_TABLE_PATH` | `/etc/lic/pricing.yaml` | Путь к YAML с per-model ценой ($/M tokens) |

Пример `pricing.yaml`:

```yaml
claude-sonnet-4-6:
  input_per_m_token_usd: 3.00
  output_per_m_token_usd: 15.00
gpt-4.1:
  input_per_m_token_usd: 2.50
  output_per_m_token_usd: 10.00
gemini-2.5-pro:
  input_per_m_token_usd: 1.25
  output_per_m_token_usd: 5.00
```

### 2.16 Опциональные кэши

| Var | Default | Описание |
|-----|---------|----------|
| `LIC_LLM_CACHE_ENABLED` | `false` | Кэширование результатов LLM (см. ASSUMPTION-LIC-15) |
| `LIC_VERSION_META_CACHE_TTL` | `24h` | TTL кэша origin_type + parent_version_id |

---

## 3. Validation startup

При старте LIC валидирует конфиг:
1. Required переменные присутствуют (см. таблицы выше).
2. URLs валидны (`url.Parse`).
3. Provider в `LIC_PROVIDER_FALLBACK_ORDER` имеют API keys (если хоть один из агентов их использует).
4. Per-agent provider override указывает на provider из FALLBACK_ORDER.
5. Timeouts > 0.
6. Веса AGGREGATE_SCORE — числовые.
7. `LIC_CONFIDENCE_THRESHOLD` ∈ [0.0, 1.0].
8. `LIC_PIPELINE_CONCURRENCY` ≥ 1.

Любая ошибка → `FATAL log + exit 1` (fail-fast).

---

## 4. Пример `.env` для local dev

```env
# Application
LIC_LOG_LEVEL=debug
LIC_ENV=local
LIC_HTTP_PORT=8080

# RabbitMQ (local docker-compose)
LIC_BROKER_URL=amqp://contractpro:contractpro@rabbitmq:5672/contractpro
LIC_CONSUMER_PREFETCH=5
LIC_CONSUMER_MAX_REDELIVERIES=3

# Redis (local docker-compose)
LIC_REDIS_URL=redis://redis:6379
LIC_REDIS_DB=2
LIC_REDIS_TLS=false

# Pipeline
LIC_PIPELINE_CONCURRENCY=2
LIC_JOB_TIMEOUT=90s
LIC_CONFIDENCE_THRESHOLD=0.75
LIC_MAX_INPUT_TOKENS=150000

# LLM — Claude (default)
LIC_CLAUDE_API_KEY=sk-ant-***-replace-with-real-key***
LIC_CLAUDE_MODEL=claude-sonnet-4-6
LIC_CLAUDE_RPS=10
LIC_CLAUDE_BURST=20
LIC_CLAUDE_PROMPT_CACHE_ENABLED=true

# LLM — OpenAI (fallback)
LIC_OPENAI_API_KEY=sk-***-replace-with-real-key***
LIC_OPENAI_MODEL=gpt-4.1
LIC_OPENAI_RPS=20
LIC_OPENAI_BURST=40

# LLM — Gemini (fallback)
LIC_GEMINI_API_KEY=AIza***-replace-with-real-key***
LIC_GEMINI_MODEL=gemini-2.5-pro
LIC_GEMINI_RPS=20
LIC_GEMINI_BURST=40

# Provider strategy
LIC_PROVIDER_FALLBACK_ORDER=claude,openai,gemini
LIC_LLM_REQUEST_TIMEOUT=60s
LIC_LLM_CONCURRENCY_PER_PROVIDER=5

# Per-agent overrides (all default to claude in this example)
LIC_AGENT_TYPE_CLASSIFIER_PROVIDER=claude
LIC_AGENT_KEY_PARAMS_PROVIDER=claude
LIC_AGENT_PARTY_CONSISTENCY_PROVIDER=claude
LIC_AGENT_MANDATORY_CONDITIONS_PROVIDER=claude
LIC_AGENT_RISK_DETECTION_PROVIDER=claude
LIC_AGENT_RECOMMENDATION_PROVIDER=claude
LIC_AGENT_SUMMARY_PROVIDER=claude
LIC_AGENT_DETAILED_REPORT_PROVIDER=claude
LIC_AGENT_RISK_DELTA_PROVIDER=claude

# Aggregate score
LIC_SCORE_WEIGHT_HIGH=25
LIC_SCORE_WEIGHT_MEDIUM=10
LIC_SCORE_WEIGHT_LOW=3
LIC_SCORE_WEIGHT_MISSING_MANDATORY=15
LIC_SCORE_WEIGHT_AMBIGUOUS_MANDATORY=5
LIC_SCORE_LABEL_LOW_THRESHOLD=0.75
LIC_SCORE_LABEL_MEDIUM_THRESHOLD=0.45

# Observability
LIC_OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
LIC_OTEL_EXPORTER_OTLP_INSECURE=true
LIC_OTEL_TRACES_SAMPLER=parentbased_traceidratio
LIC_OTEL_TRACES_SAMPLER_ARG=1.0
LIC_OTEL_SERVICE_NAME=lic-service-local
```

---

## 5. Production / staging

В production / staging:
- `LIC_ENV=production` или `staging`.
- `LIC_LOG_LEVEL=info` (production) / `debug` (staging).
- `LIC_BROKER_URL` через `amqps://`.
- `LIC_REDIS_URL` через `rediss://`.
- API keys — из Yandex Lockbox (CSI inject).
- `LIC_OTEL_TRACES_SAMPLER_ARG=0.1` в production.
- `LIC_PIPELINE_CONCURRENCY=5` (per pod) × N pods (горизонтально).

---

## 6. Self-check

- [x] Все env-переменные с префиксом `LIC_`.
- [x] Required vs optional разделены.
- [x] Defaults — разумные.
- [x] Конфигурация LLM-провайдеров и per-agent параметров покрыта.
- [x] Pricing table вынесен в YAML.
- [x] Aggregate score веса конфигурируемы.
- [x] Validation на startup — fail-fast.
- [x] Пример `.env` для local dev.
