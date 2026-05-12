# Deployment — Legal Intelligence Core

Документ описывает Docker multi-stage build, Docker Compose для local dev, health checks, graceful shutdown, горизонтальное масштабирование и управление секретами.

---

## 1. Артефакт сборки

### 1.1 Что сборка производит

Один Go-бинарник `lic-service` (статический, без CGO). Архитектура — `linux/amd64` (production), также собирается `linux/arm64` (Apple Silicon dev).

### 1.2 Module layout

```
LegalIntelligenceCore/development/
├── cmd/lic-service/main.go        — entrypoint
├── internal/
│   ├── app/                       — wiring, lifecycle, graceful shutdown
│   ├── config/                    — env-based configuration loader (godotenv)
│   ├── domain/
│   │   ├── model/                 — AnalysisJob, AgentResult, etc.
│   │   └── port/                  — LLMProviderPort, ArtifactPersistencePort, etc.
│   ├── application/
│   │   ├── pipeline/              — Pipeline Orchestrator
│   │   ├── pendingconfirmation/   — Pending Type Confirmation Manager
│   │   ├── dmawaiter/             — DM Artifact Awaiter / DM Confirmation Awaiter
│   │   └── aggregator/            — Result Aggregator + RISK_PROFILE / AGGREGATE_SCORE calc
│   ├── agents/
│   │   ├── typeclassifier/        — Agent 1
│   │   ├── keyparams/             — Agent 2
│   │   ├── partyconsistency/      — Agent 3
│   │   ├── mandatoryconditions/   — Agent 4
│   │   ├── riskdetection/         — Agent 5
│   │   ├── recommendation/        — Agent 6
│   │   ├── summary/               — Agent 7
│   │   ├── detailedreport/        — Agent 8
│   │   ├── riskdelta/             — Agent 9
│   │   ├── prompts/               — embedded system prompts (embed.FS)
│   │   ├── schemas/               — embedded JSON schemas (embed.FS)
│   │   ├── promptbuilder/         — Prompt Builder
│   │   ├── schemavalidator/       — JSON Schema validator + Repair Loop
│   │   └── tokenestimator/        — Input length check
│   ├── llm/
│   │   ├── router/                — Provider Router + healthcheck registry
│   │   ├── claude/                — ClaudeProvider adapter
│   │   ├── openai/                — OpenAIProvider adapter
│   │   ├── gemini/                — GeminiProvider adapter
│   │   ├── ratelimit/             — Token bucket (Redis)
│   │   ├── cost/                  — Cost & Usage Tracker
│   │   └── pricing/               — Pricing table loader
│   ├── ingress/
│   │   ├── consumer/              — RabbitMQ event consumer (deserialize + validate)
│   │   ├── idempotency/           — Idempotency Guard (Redis)
│   │   └── router/                — Event Router
│   ├── egress/
│   │   ├── publisher/             — Status / Uncertainty / DM publishers
│   │   └── dlq/                   — DLQ Publisher
│   ├── infra/
│   │   ├── broker/                — RabbitMQ client (publish + subscribe + reconnect)
│   │   ├── kvstore/               — Redis client
│   │   ├── observability/         — structured logger, OTel tracer, Prometheus
│   │   ├── concurrency/           — Semaphore-based concurrency limiter
│   │   └── health/                — /healthz, /readyz, /metrics handlers
│   └── integration/               — End-to-end tests with in-memory fakes
├── Dockerfile
├── Makefile
└── go.mod
```

> Структура соответствует hexagonal-подходу DP/DM (порты в `domain/port`, инфраструктура в `infra/`, application services в `application/`).

### 1.3 Build commands

```makefile
.PHONY: build test lint docker-build docker-run

GIT_TAG := $(shell git describe --always --dirty)
DOCKER_IMG := contractpro/lic-service:$(GIT_TAG)

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags="-s -w -X main.version=$(GIT_TAG)" \
		-o bin/lic-service ./cmd/lic-service/

test:
	go test ./...

lint:
	go vet ./...
	golangci-lint run

docker-build:
	docker build -t $(DOCKER_IMG) .
	docker tag $(DOCKER_IMG) contractpro/lic-service:latest
```

---

## 2. Dockerfile (multi-stage)

```dockerfile
# Stage 1: build
FROM golang:1.26-alpine AS builder

WORKDIR /workspace
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/lic-service ./cmd/lic-service/

# Stage 2: runtime
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/lic-service /lic-service
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/lic-service"]
```

**Ключевые решения:**
- Distroless `nonroot` базовый образ — no shell, no package manager, минимальная attack surface.
- CGO выключен — статический бинарь.
- TLS CA-bundle копируется в runtime stage (нужно для исходящих HTTPS к LLM провайдерам).
- `EXPOSE 8080` — порт `/healthz`, `/readyz`, `/metrics`.

### 2.1 Размер образа

Ожидаемый размер: ~30 МБ (Go binary ~25 МБ + distroless ~5 МБ).

### 2.2 Build args

```bash
docker build \
  --build-arg VERSION=$(git describe --always --dirty) \
  -t contractpro/lic-service:$(git describe --always --dirty) \
  .
```

---

## 3. Docker Compose (local dev)

LIC интегрируется в общий `docker-compose.yaml` проекта (по аналогии с DP/DM/Orchestrator). Контейнерная декомпозиция (relevant fragments):

```yaml
services:
  rabbitmq:
    image: rabbitmq:3.13-management
    ports: ["5672:5672","15672:15672"]
    environment:
      RABBITMQ_DEFAULT_USER: contractpro
      RABBITMQ_DEFAULT_PASS: contractpro
      RABBITMQ_DEFAULT_VHOST: contractpro
    healthcheck:
      test: rabbitmq-diagnostics ping
      interval: 10s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
    healthcheck:
      test: redis-cli ping
      interval: 5s
      timeout: 3s
      retries: 3

  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    command: ["--config=/etc/otelcol-contrib/config.yaml"]
    volumes:
      - ./observability/otel-collector-config.yaml:/etc/otelcol-contrib/config.yaml
    ports: ["4317:4317","4318:4318"]

  lic-service:
    build:
      context: ./LegalIntelligenceCore/development
      dockerfile: Dockerfile
      args:
        VERSION: ${GIT_TAG:-dev}
    ports: ["8081:8080"]
    environment:
      LIC_LOG_LEVEL: debug
      LIC_ENV: local
      LIC_BROKER_URL: amqp://contractpro:contractpro@rabbitmq:5672/contractpro
      LIC_REDIS_URL: redis://redis:6379
      LIC_REDIS_DB: 2
      LIC_PIPELINE_CONCURRENCY: 2
      LIC_CONFIDENCE_THRESHOLD: "0.75"
      LIC_PROVIDER_FALLBACK_ORDER: claude,openai,gemini
      LIC_CLAUDE_API_KEY: ${LIC_CLAUDE_API_KEY:?Set LIC_CLAUDE_API_KEY in .env}
      LIC_OPENAI_API_KEY: ${LIC_OPENAI_API_KEY:-}
      LIC_GEMINI_API_KEY: ${LIC_GEMINI_API_KEY:-}
      LIC_OTEL_EXPORTER_OTLP_ENDPOINT: otel-collector:4317
      LIC_OTEL_EXPORTER_OTLP_INSECURE: "true"
    depends_on:
      rabbitmq: {condition: service_healthy}
      redis:    {condition: service_healthy}
    healthcheck:
      test: ["CMD","/lic-service","--healthcheck"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 30s
    restart: unless-stopped
```

> Существующий проектный `docker-compose.yaml` уже включает RabbitMQ, Redis, Prometheus, OTel. Сервис `lic-service` добавляется как новый блок.

### 3.1 Запуск

```bash
# Загрузить API keys в окружение
echo "LIC_CLAUDE_API_KEY=sk-ant-..." >> .env

# Запустить весь стек
docker compose up --build lic-service

# Только LIC + зависимости
docker compose up rabbitmq redis otel-collector lic-service
```

---

## 4. Kubernetes (production / staging)

### 4.1 Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: lic-service
  namespace: contractpro
  labels: {app: lic-service}
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0
      maxSurge: 1
  selector:
    matchLabels: {app: lic-service}
  template:
    metadata:
      labels: {app: lic-service}
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/path: "/metrics"
        prometheus.io/port: "8080"
    spec:
      serviceAccountName: lic-service
      terminationGracePeriodSeconds: 130
      containers:
      - name: lic-service
        image: cr.yandex/contractpro/lic-service:${GIT_TAG}
        imagePullPolicy: IfNotPresent
        ports:
        - {name: http, containerPort: 8080}
        env:
        - {name: LIC_ENV, value: production}
        - {name: LIC_LOG_LEVEL, value: info}
        - {name: LIC_BROKER_URL, valueFrom: {secretKeyRef: {name: lic-secrets, key: broker_url}}}
        - {name: LIC_REDIS_URL,  valueFrom: {secretKeyRef: {name: lic-secrets, key: redis_url}}}
        - {name: LIC_CLAUDE_API_KEY, valueFrom: {secretKeyRef: {name: lic-llm-keys, key: claude_api_key}}}
        - {name: LIC_OPENAI_API_KEY, valueFrom: {secretKeyRef: {name: lic-llm-keys, key: openai_api_key}}}
        - {name: LIC_GEMINI_API_KEY, valueFrom: {secretKeyRef: {name: lic-llm-keys, key: gemini_api_key}}}
        - {name: LIC_PIPELINE_CONCURRENCY, value: "5"}
        - {name: LIC_OTEL_EXPORTER_OTLP_ENDPOINT, value: "otel-collector.observability:4317"}
        - {name: LIC_OTEL_TRACES_SAMPLER_ARG, value: "0.1"}
        envFrom:
        - {configMapRef: {name: lic-config}}
        readinessProbe:
          httpGet: {path: /readyz, port: http}
          initialDelaySeconds: 10
          periodSeconds: 5
          failureThreshold: 3
        livenessProbe:
          httpGet: {path: /healthz, port: http}
          initialDelaySeconds: 30
          periodSeconds: 30
          failureThreshold: 3
        resources:
          requests: {cpu: 200m, memory: 256Mi}
          limits:   {cpu: 1000m, memory: 1Gi}
        volumeMounts:
        - {name: pricing-config, mountPath: /etc/lic, readOnly: true}
      volumes:
      - name: pricing-config
        configMap: {name: lic-pricing}
```

### 4.2 Resource sizing

**Per pod:**
- Requests: 200m CPU, 256 MiB RAM.
- Limits: 1 CPU, 1 GiB RAM.

**Memory budget:**
- Go runtime + pools: ~50 MiB.
- Per active job: ~10 MiB (semantic_tree + extracted_text + intermediate AgentResult).
- 5 concurrent jobs (`LIC_PIPELINE_CONCURRENCY=5`): ~50 MiB.
- Pending state buffer (Redis client buffer): ~50 MiB.
- Buffer: ~100 MiB.
- **Total: ~250 MiB resident** (укладывается в request).

**CPU budget:**
- LIC сам по себе CPU-light (большинство времени — wait для LLM HTTP).
- Под нагрузкой 1000 договоров/день в 8 рабочих часов = ~2 jobs/min — каждый в среднем 30 секунд CPU activity → ~10% CPU при concurrent 1.
- 5 concurrent jobs → пик ~200m CPU (укладывается в request).

### 4.3 HorizontalPodAutoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: lic-service, namespace: contractpro}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: lic-service
  minReplicas: 3
  maxReplicas: 20
  metrics:
  - type: Resource
    resource: {name: cpu, target: {type: Utilization, averageUtilization: 70}}
  - type: Pods
    pods:
      metric: {name: lic_pipeline_concurrent_jobs}
      target: {type: AverageValue, averageValue: "4"}
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies: [{type: Pods, value: 2, periodSeconds: 60}]
    scaleDown:
      stabilizationWindowSeconds: 300
      policies: [{type: Percent, value: 25, periodSeconds: 60}]
```

Скейлинг по CPU + custom metric `lic_pipeline_concurrent_jobs` (через Prometheus Adapter).

### 4.4 PodDisruptionBudget

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata: {name: lic-service}
spec:
  minAvailable: 2
  selector:
    matchLabels: {app: lic-service}
```

---

## 5. Health checks

### 5.1 `/healthz` (liveness)

- Простой `200 OK`.
- Используется kubelet livenessProbe.

### 5.2 `/readyz` (readiness)

- Проверяет:
  - RabbitMQ connection alive.
  - Redis PING < 100ms.
  - Хотя бы один LLM-провайдер healthy.
- Возвращает `503 Service Unavailable` при failure.
- Используется kubelet readinessProbe + LB.

### 5.3 `/metrics`

- Prometheus exposition format.
- Эндпоинт scraped Prometheus / VictoriaMetrics.

### 5.4 Docker healthcheck CLI

`/lic-service --healthcheck` — встроенный subcommand, возвращает exit 0 при liveness OK, exit 1 при failure. Используется в docker-compose.

---

## 6. Graceful shutdown

См. `error-handling.md` §12. Sequence:

1. `SIGTERM` от kubelet.
2. `/readyz` → 503.
3. Endpoint удаляется из Service (через 5s — readinessProbe failure detection).
4. RabbitMQ consumer останавливает приём новых сообщений (`channel.Cancel`).
5. Ждать завершения in-flight pipelines (`LIC_SHUTDOWN_TIMEOUT=120s`).
6. NACK с requeue для оставшихся (если deadline истёк).
7. Закрыть connection RabbitMQ → Redis → flush OTel.
8. Exit 0.

`terminationGracePeriodSeconds: 130s` (kube) > `LIC_SHUTDOWN_TIMEOUT=120s` + 10s buffer.

---

## 7. Горизонтальное масштабирование

LIC stateless → добавление pods даёт линейное масштабирование throughput.

### 7.1 Контурное планирование

| Нагрузка | Replicas | Notes |
|---------|----------|-------|
| 1 000 договоров/день (текущая цель) | 3 | 95% времени idle |
| 10 000 / день | 5 | Пиковая нагрузка ~10 jobs/min |
| 100 000 / день | 20 | Hits HPA max — нужно поднять |

Bottleneck — НЕ LIC, а LLM rate limits провайдеров. При нагрузке 100К/день — нужно увеличить квоты у Anthropic/OpenAI.

### 7.2 Идемпотентность при scale

Все ключи идемпотентности — в shared Redis. Нет проблем при одновременной обработке одного сообщения двумя инстансами:
- Idempotency Guard (atomic SETNX) гарантирует, что только один инстанс будет обрабатывать сообщение.
- Дублирующие инстансы получают `PROCESSING` статус → ACK без обработки.

### 7.3 Pending state при scale

Pending state хранится в Redis (shared). Любой инстанс может resume пайплайн при получении `UserConfirmedType`. Это работает out of the box.

---

## 8. Управление секретами

### 8.1 LIC API keys для LLM

| Среда | Источник |
|-------|----------|
| Local dev | `.env` (gitignore) |
| Staging | `Secret` Kubernetes (синхронизирован через ESO из Yandex Lockbox) |
| Production | `Secret` через ESO + KMS-encrypted volume |

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata: {name: lic-llm-keys, namespace: contractpro}
spec:
  secretStoreRef:
    name: yandex-lockbox-store
    kind: SecretStore
  target:
    name: lic-llm-keys
    creationPolicy: Owner
  data:
  - {secretKey: claude_api_key, remoteRef: {key: lic-llm-keys, property: claude_api_key}}
  - {secretKey: openai_api_key, remoteRef: {key: lic-llm-keys, property: openai_api_key}}
  - {secretKey: gemini_api_key, remoteRef: {key: lic-llm-keys, property: gemini_api_key}}
  refreshInterval: 1h
```

### 8.2 Хот-реролл

При обновлении `lic-llm-keys` (через rotation):
- ESO обновляет `Secret` (refreshInterval: 1h).
- Pod **не** перезапускается (Kubernetes не reloadit secret в env automaticaly).
- Реакция через SIGHUP (см. `llm-provider-abstraction.md` §6.3) — отдельный mechanism для key rotation без restarts.
- В v1 — простой rolling restart deployment'а каждые 90 дней при rotation. SIGHUP — для emergency rotation.

### 8.3 RabbitMQ / Redis credentials

В `lic-secrets` (отдельный Secret), сценарий аналогичен.

---

## 9. CI/CD (контурно)

### 9.1 Build pipeline

1. `git push` → trigger CI.
2. `make lint && make test` (unit tests).
3. Integration tests (in-memory fakes for DM, RabbitMQ, mockProvider).
4. `make docker-build`, push to registry (Yandex Container Registry).
5. Manifest update в Helm chart / Kustomize overlay.
6. ArgoCD / Flux sync.

### 9.2 Smoke tests post-deploy

- `/readyz` 200 OK.
- Тестовый `version-artifacts-ready` сообщение → expected `lic.events.status-changed.COMPLETED`.
- Прямой LLM-вызов (минимальный test case) — успешно.

### 9.3 Rollback

- Автоматический rollback при `readinessProbe` failure для > 50% pods за 5 мин.
- Manual rollback: ArgoCD UI → previous revision.

---

## 10. Multi-region (out of scope для v1)

В v1 — single region (`ru-central1` в Yandex Cloud). Multi-region не требуется (SLA 98% достижим в single region).

> Multi-region active-active требует distributed Redis / synchronization pending state + обсуждение с DM — выходит за рамки v1.

---

## 11. Self-check

- [x] Multi-stage Dockerfile с distroless runtime.
- [x] Docker Compose для local dev.
- [x] Kubernetes Deployment с readinessProbe / livenessProbe / HPA / PDB.
- [x] Resource sizing с обоснованием.
- [x] Graceful shutdown sequence.
- [x] Horizontal scaling возможен благодаря stateless-природе.
- [x] Управление секретами через ESO + Yandex Lockbox.
- [x] CI/CD pipeline.
