# Observability — Legal Intelligence Core

Документ описывает structured logging, Prometheus metrics, OpenTelemetry tracing, алёрты и operational dashboards LIC.

---

## 1. Принципы

1. **Three pillars:** logs (что произошло), metrics (числовые тренды), traces (связь событий в одном flow).
2. **Allowlist для PII.** В лог попадает только то, что явно перечислено (см. `security.md` §6).
3. **Correlation ID везде.** `correlation_id`, `job_id`, `version_id`, `organization_id` — обязательные поля логов и attributes spans.
4. **Standard fields:** все логи структурированные (JSON), все метрики — Prometheus exposition format на `/metrics`, traces — OpenTelemetry OTLP.

---

## 2. Structured logging

### 2.1 Logger

Используется `zerolog` (или `zap` — interchangeable; критерий — нативный structured JSON, низкий overhead).

```go
log.Info().
    Str("correlation_id", corrID).
    Str("job_id", jobID).
    Str("version_id", versionID).
    Str("organization_id", orgID).
    Str("agent_id", "AGENT_RISK_DETECTION").
    Str("provider", "claude").
    Str("model", "claude-sonnet-4-6").
    Int("input_tokens", 15234).
    Int("output_tokens", 2891).
    Int64("latency_ms", 4321).
    Str("outcome", "success").
    Msg("agent invocation completed")
```

### 2.2 Уровни

| Level | Когда |
|-------|-------|
| `DEBUG` | Промежуточные шаги, переходы между стадиями (выкл. в production по умолчанию) |
| `INFO` | Старт/завершение pipeline, агентов, LLM-вызовов, публикация событий |
| `WARN` | Repair triggered, fallback to other provider, degradation, prompt_injection_detected |
| `ERROR` | Failed pipeline, fatal LLM errors (после fallback), DLQ-публикация |
| `FATAL` | Только при unrecoverable boot-time errors |

### 2.3 Структура лога pipeline

При успешном пайплайне ожидаемый набор лог-записей (в порядке):

```
INFO  pipeline.start          {correlation_id, job_id, version_id, organization_id, mode:"INITIAL|RE_CHECK", origin_type:"<DM enum, opaque>"}
INFO  dm.artifacts.requested  {correlation_id, types:[...]}
INFO  dm.artifacts.received   {correlation_id, types:[...], received_size_bytes}
INFO  agent.invocation        {agent_id, provider, model, outcome:"success"}
... (по одной записи на агента)
INFO  pipeline.completed      {correlation_id, total_duration_ms, total_cost_usd, total_input_tokens, total_output_tokens}
```

При FAILED — добавляется `ERROR pipeline.failed {error_code, stage, is_retryable}`.

### 2.4 Allowlist полей (PII redaction)

Allowed in logs:
- IDs: `correlation_id`, `job_id`, `document_id`, `version_id`, `organization_id`, `created_by_user_id`, `confirmed_by_user_id`, `agent_id`, `provider_id`, `message_id`.
- Metadata: `model`, `outcome`, `tokens_in`, `tokens_out`, `latency_ms`, `cost_usd`, `stage`, `status`, `error_code`, `error_message` (без user-data), `is_retryable`, `prompt_injection_detected: bool`.
- Sizes: `input_size_bytes`, `output_size_bytes`, `payload_size_bytes`.
- Counts: `risks_count`, `findings_count`, `warnings_count`.
- Hashes: `raw_response_hash` (sha256 first-1024-chars).

NOT allowed in logs:
- Полный текст `EXTRACTED_TEXT`, `SEMANTIC_TREE`.
- Полные ответы LLM.
- Содержимое `key_parameters`, `risks[].description`, `parties`, `subject`, `price`.
- Сами API-ключи или их фрагменты.

### 2.5 Sampling

DEBUG — выключено в production. INFO — без sampling. ERROR / FATAL — без sampling. WARN — без sampling.

---

## 3. Prometheus metrics

### 3.1 Соглашения по именованию

- Префикс: `lic_`.
- Тип: `*_total` (counter), `*_seconds` (histogram), `*_count`/`*_size` (gauge).
- Labels: low cardinality (см. cardinality budget).

### 3.2 Pipeline metrics

```
lic_pipeline_started_total{mode}                              counter
lic_pipeline_total_duration_seconds{mode,outcome}             histogram (buckets: 1,5,10,15,20,30,45,60,90,120)
lic_pipeline_outcome_total{mode,outcome,error_code}           counter (outcome ∈ success|failed|timeout)
lic_pipeline_concurrent_jobs                                  gauge (текущее число in-flight jobs в инстансе)
lic_pipeline_stage_duration_seconds{stage}                    histogram
```

`mode` ∈ `INITIAL | RE_CHECK` — бинарный label, вычисляется в LIC: `mode = parent_version_id != null ? "RE_CHECK" : "INITIAL"`. Конкретное DM-значение `origin_type` (5 enum) в Prometheus labels не публикуется (cardinality budget; при необходимости детализации использовать OTel span attribute `lic.pipeline.origin_type`).
`outcome` ∈ `success | failed | timeout`.

> **Single source of truth для enum-значений всех `outcome` labels.** Этот документ (observability.md) — авторитетный источник enum-значений для всех Prometheus-меток LIC. Другие файлы (error-handling.md, llm-provider-abstraction.md, high-architecture.md, ai-agents-pipeline.md) ссылаются сюда; при расхождении приоритет за observability.md.

### 3.3 Agent metrics

```
lic_agent_invocations_total{agent,outcome}                    counter
lic_agent_duration_seconds{agent}                              histogram (buckets: 0.5,1,2,5,8,12,20)
lic_agent_input_tokens{agent}                                  histogram (buckets: 1k,4k,8k,16k,32k,64k)
lic_agent_output_tokens{agent}                                 histogram (buckets: 100,500,1k,2k,4k,8k)
lic_agent_repair_attempts_total{agent,provider}                counter
lic_agent_repair_outcome_total{agent,provider,outcome}         counter (outcome ∈ repaired_ok|repair_failed|repair_provider_error)
```

`agent` ∈ `AGENT_TYPE_CLASSIFIER | AGENT_KEY_PARAMS | AGENT_PARTY_CONSISTENCY | AGENT_MANDATORY_CONDITIONS | AGENT_RISK_DETECTION | AGENT_RECOMMENDATION | AGENT_SUMMARY | AGENT_DETAILED_REPORT | AGENT_RISK_DELTA`.
`outcome` (для `lic_agent_invocations_total`) ∈ `success | repair_success | invalid_output | provider_error | timeout`.
`outcome` (для `lic_agent_repair_outcome_total`) ∈ `repaired_ok | repair_failed | repair_provider_error`. Label `provider` для repair-метрик = `used_provider` (тот, на котором был исходный успешный response — sticky per OQ-10); это даёт чёткий signal для operator'а при выборе primary через `LIC_AGENT_*_PROVIDER`.

### 3.4 LLM metrics

```
lic_llm_calls_total{provider,model,agent,outcome}              counter
lic_llm_latency_seconds{provider,model,agent}                  histogram
lic_llm_input_tokens_total{provider,model,agent}               counter  (billable uncached input tokens)
lic_llm_cached_tokens_total{provider,model,agent}              counter  (tokens served from prompt-cache; 0 для OpenAI/Gemini в v1)
lic_llm_output_tokens_total{provider,model,agent}              counter
lic_llm_cost_usd_total{provider,model,agent}                   counter
lic_llm_provider_fallback_total{from,to,agent}                 counter
lic_llm_provider_skipped_unhealthy_total{provider}             counter
lic_llm_provider_failed_total{provider,code}                   counter
lic_llm_provider_health_status{provider,state}                  gauge (state ∈ healthy|unhealthy|permanent — см. llm-provider-abstraction §2.3)
lic_llm_provider_circuit_state{provider}                       gauge (0=closed, 1=half_open, 2=open)
lic_llm_rate_limited_total{provider}                            counter
```

`code` ∈ `TIMEOUT | RATE_LIMIT | SERVER_ERROR | NETWORK | OVERLOADED | INVALID_API_KEY | QUOTA_EXCEEDED | CONTENT_POLICY | CONTEXT_TOO_LONG | MALFORMED_REQUEST` (см. `llm-provider-abstraction.md` §1.2). Label `code` заменяет более общий `reason` старой версии — даёт точное соответствие типу `LLMProviderError`.

`outcome` для `lic_llm_calls_total` ∈ `success | repair | fail | fallback`. `repair` инкрементируется при `CompleteRepair`-вызове (вне зависимости от его outcome — отдельная метрика `lic_agent_repair_outcome_total` даёт granularity).

`lic_llm_cached_tokens_total` критична для cost-метрики: без её учёта расчёт стоимости был бы завышен до **10× на cache-hit запросах** Anthropic. См. `llm-provider-abstraction.md` §4.1 для формулы cost-расчёта.

### 3.5 DM interaction metrics

```
lic_dm_request_duration_seconds{op}                            histogram (op ∈ get_artifacts|persist_artifacts)
lic_dm_request_outcome_total{op,outcome}                       counter (outcome ∈ success|timeout|persist_failed|missing)
lic_dm_artifacts_received_size_bytes                           histogram
lic_dm_artifacts_published_size_bytes                          histogram
```

### 3.6 Idempotency metrics

```
lic_idempotency_lookups_total{result}                          counter (result ∈ new|in_progress|completed|fallback_db)
lic_idempotency_fallback_total                                  counter (Redis недоступен → fallback path)
```

### 3.7 Pending type confirmation metrics

```
lic_pending_state_count                                         gauge (текущее число pending записей в Redis)
lic_pending_state_age_seconds_max                               gauge (max возраст самой старой pending записи)
lic_user_confirmation_received_total{outcome}                   counter (outcome ∈ resumed|expired|invalid)
```

### 3.8 DLQ metrics

```
lic_dlq_published_total{topic,reason}                           counter
```

`topic` ∈ `lic.dlq.invalid-message | lic.dlq.consumer-failed | lic.dlq.publish-failed | lic.dlq.agent-output-invalid`.

### 3.9 Other / cross-cutting

```
lic_prompt_injection_detected_total{agent}                       counter
lic_party_validation_total{type,valid}                            counter  (type ∈ inn|ogrn; valid ∈ true|false; см. high-architecture §6.7.2)
lic_consumer_messages_total{topic,outcome}                       counter
lic_publisher_messages_total{topic,outcome}                      counter
lic_circuit_breaker_state{component}                              gauge
lic_build_info{version,commit,go_version}                          gauge (always 1)
```

`lic_prompt_injection_detected_total` — per-agent counter без `severity` label (C-lite policy per OQ-13: severity не вычисляется в v1). Cardinality: 9 agents = 9 series, ничтожно. Алёрт см. §6.

### 3.10 Cardinality budget

Estimated max series:
- pipeline: 2 × 9 × 4 = 72 / instance.
- agent: 9 × 5 = 45.
- LLM: 3 providers × ~3 models × 9 agents × 5 outcomes ≈ 405.
- DM: 2 × 4 = 8.
- DLQ: 4 × 3 = 12.
- Total: ≤ 1500 series / instance. Acceptable.

`organization_id` в labels — **НЕ ДОБАВЛЯЕТСЯ** в Prometheus метрики (cardinality blowup при 10К tenants × 1500 series = 15M серий — недопустимо). `organization_id` есть только в логах и OTel attributes (там retention короче и вычислительный механизм отличный).

---

## 4. OpenTelemetry tracing

### 4.1 Provider

OpenTelemetry SDK для Go. Exporter: OTLP (gRPC) на коллектор (Tempo / Jaeger / Yandex Tracing).

### 4.2 Иерархия spans

```
lic.pipeline                                  (root для одного pipeline run)
├── lic.dm.artifacts.request
├── lic.dm.artifacts.await
├── lic.stage.s1.parallel
│   ├── lic.agent.type_classifier
│   │   └── lic.llm.call (provider=claude, model=...)
│   └── lic.agent.key_params
│       └── lic.llm.call
├── lic.stage.s2.party_consistency
│   └── lic.agent.party_consistency
│       └── lic.llm.call
├── lic.stage.s3.parallel
│   ├── lic.agent.mandatory_conditions
│   └── lic.agent.risk_detection
├── lic.stage.s4.recommendation
├── lic.stage.s5.parallel
│   ├── lic.agent.summary
│   └── lic.agent.detailed_report
├── lic.stage.s6.risk_delta              (опц.)
├── lic.calc.risk_profile
├── lic.calc.aggregate_score
├── lic.dm.publish.analysis_ready
└── lic.dm.persist.await
```

### 4.3 Span attributes

Каждый span содержит:
- `correlation_id`, `job_id`, `version_id`, `document_id`, `organization_id`, `created_by_user_id`.
- На root: `lic.pipeline.mode` (INITIAL/RE_CHECK — бинарный, вычисляется по `parent_version_id`), `lic.pipeline.origin_type` (опционально, исходное DM-значение для детализации), `lic.pipeline.outcome`, `lic.pipeline.prompt_injection.detected: bool`, `lic.pipeline.prompt_injection.detection_count: int`, `lic.pipeline.prompt_injection.detected_by_agents: [string]` (присутствуют только при `detected=true`).
- На agent spans: `lic.agent.id`, `lic.agent.outcome`, `lic.agent.repair_attempts`, `lic.agent.prompt_injection_detected: bool` (если агент имеет это поле в схеме).
- На llm spans: `lic.llm.provider`, `lic.llm.model`, `lic.llm.input_tokens`, `lic.llm.cached_tokens`, `lic.llm.output_tokens`, `lic.llm.latency_ms`, `lic.llm.cost_usd`, `lic.llm.fallback_used: bool`.

### 4.4 Trace context propagation

LIC получает `traceparent` из RabbitMQ message headers (W3C Trace Context). При публикации исходящих — пробрасывает `traceparent` в headers.

DP, DM, RE, Orchestrator также используют W3C Trace Context — единый trace проходит сквозь всю цепочку: upload → DP → DM → LIC → DM → RE → DM.

### 4.5 Sampling

- Production: head sampling 10% (`OTEL_TRACES_SAMPLER=parentbased_traceidratio`, ratio 0.1).
- Errors / FAILED pipelines: всегда сэмплируются (custom sampler с upgrade при `outcome=failed`).
- Staging: 100%.

### 4.6 Retention

OTel backend retention: 14 дней (зависит от инфраструктуры).

---

## 5. Дашборды

### 5.1 «LIC Health»

KPI-дашборд для on-call и менеджмента.

**Панели:**
- Pipelines/sec (rate `lic_pipeline_started_total[5m]`)
- Success rate (`rate(lic_pipeline_outcome_total{outcome="success"}[5m]) / rate(lic_pipeline_started_total[5m])`)
- p95 / p99 pipeline duration (`histogram_quantile(0.95, lic_pipeline_total_duration_seconds_bucket)`)
- Concurrent jobs (`lic_pipeline_concurrent_jobs`)
- LLM cost / hour (`rate(lic_llm_cost_usd_total[1h]) * 3600`)
- DLQ depth (`rabbitmq_queue_messages{queue=~"lic.dlq.*"}`)
- Provider fallback rate (`rate(lic_llm_provider_fallback_total[5m])`)

### 5.2 «LIC Agents»

Per-agent breakdown.

**Панели:**
- Per-agent invocation rate (heatmap by `agent`)
- Per-agent latency p95 (table)
- Per-agent token usage (input + output, stacked bar by `agent`)
- Per-agent cost (stacked bar)
- Per-agent repair rate (rate of repair_attempts / invocations)

### 5.3 «LIC LLM Providers»

Анализ провайдеров.

**Панели:**
- Per-provider call rate
- Per-provider latency p50/p95/p99
- Per-provider error rate (by `reason`)
- Per-provider circuit state (timeline)
- Per-provider rate limit hits
- Cost breakdown by provider

### 5.4 «LIC Pipeline Internals»

Для troubleshooting.

**Панели:**
- Stage duration breakdown (stacked area by `stage`)
- Failure breakdown (by `error_code`)
- Idempotency hit rate
- Pending state count + max age
- Prompt injection detection rate

### 5.5 «LIC DLQ»

Detail на DLQ.

**Панели:**
- DLQ messages by topic (rate)
- Per-topic top error codes (table)
- Time series of DLQ depth per topic

---

## 6. Алёрты (Prometheus AlertManager)

См. также `error-handling.md` §11.

```yaml
groups:
- name: lic-pipeline
  rules:
  - alert: LICPipelineFailureRateHigh
    expr: |
      sum(rate(lic_pipeline_outcome_total{outcome="failed"}[5m]))
      / sum(rate(lic_pipeline_started_total[5m])) > 0.10
    for: 5m
    labels: {severity: warning}
    annotations:
      summary: "LIC pipeline failure rate > 10% for 5 minutes"
      runbook: "https://wiki.contractpro.local/runbooks/lic-failures"

  - alert: LICPipelineDurationHigh
    expr: |
      histogram_quantile(0.95, sum(rate(lic_pipeline_total_duration_seconds_bucket[10m])) by (le)) > 60
    for: 10m
    labels: {severity: warning}

  - alert: LICAllProvidersFailing
    expr: |
      sum(rate(lic_llm_calls_total{outcome="fail"}[2m])) by (provider)
      ==
      sum(rate(lic_llm_calls_total[2m])) by (provider)
    for: 2m
    labels: {severity: critical}
    annotations:
      summary: "LLM provider {{ $labels.provider }}: 100% failure rate"

  - alert: LICCostSpike
    expr: rate(lic_llm_cost_usd_total[1h]) * 3600 > 100
    for: 30m
    labels: {severity: warning}

  - alert: LICDLQGrowth
    expr: increase(lic_dlq_published_total[1h]) > 100
    for: 5m
    labels: {severity: warning}

  - alert: LICAgentOutputInvalidRate
    expr: |
      sum(rate(lic_agent_repair_outcome_total{outcome="repair_failed"}[30m])) by (agent)
      / sum(rate(lic_agent_invocations_total[30m])) by (agent) > 0.05
    for: 30m
    labels: {severity: warning}

  - alert: LICPromptInjectionSurge
    expr: sum(rate(lic_prompt_injection_detected_total[1h])) * 3600 > 50
    for: 30m
    labels: {severity: warning}
    annotations:
      summary: "Высокий rate prompt-injection detection (> 50/час за последний час)"
      description: "Возможна волна adversarial-договоров или drift LLM detector. Проверить корреляцию по organization_id через OTel traces; если concentrated в одной — security review. C-lite policy: detection не блокирует pipeline (warning only) — это monitoring-only alert."
      runbook: "https://wiki.contractpro.local/runbooks/lic-prompt-injection-surge"

  - alert: LICRedisDown
    expr: up{job="redis"} == 0  # на самом деле проверка идёт через /readyz
    for: 1m
    labels: {severity: critical}

  - alert: LICRabbitMQDown
    expr: rabbitmq_up{instance="contractpro-rmq"} == 0
    for: 1m
    labels: {severity: critical}

  - alert: LICStuckPendingState
    expr: lic_pending_state_age_seconds_max > 79200  # 22h
    for: 5m
    labels: {severity: info}
    annotations:
      summary: "Pending type confirmation state is close to TTL expiration"
```

---

## 7. Operational runbook (контурно)

### 7.1 «Pipelines failing rapidly»

1. Проверить дашборд «LIC Health» — какой провайдер/агент падает?
2. `lic_llm_provider_failed_total` by `provider, reason` — возможно, проблема с одним провайдером (тогда circuit breaker должен был сработать).
3. Если все провайдеры failed → проверить `LIC_*_API_KEY` (rotation? quota?), сетевая связность.
4. Если только один агент failed → проверить, не изменилась ли модель / промпт; возможно, нужен rollback.

### 7.2 «Cost spike»

1. Дашборд «LIC LLM Providers» — какой провайдер/агент даёт прирост?
2. Проверить, не вырос ли `lic_agent_input_tokens` p95 (большие документы → большие prompts).
3. Если spike коррелирует с увеличением jobs/sec — нормальная нагрузка, проверить с менеджментом.
4. Если spike без увеличения jobs — анализ: возможен infinite loop через repair; включить DEBUG логи.

### 7.3 «DLQ growing»

1. Дашборд «LIC DLQ» — какой топик растёт?
2. `lic.dlq.agent-output-invalid` → проверить недавние изменения промптов / моделей; возможен regression.
3. `lic.dlq.invalid-message` → проверить, не нарушил ли кто-то upstream-контракт (DM или Orch). Скорее всего bug в чужом сервисе.
4. `lic.dlq.publish-failed` → проверить connectivity с RabbitMQ.

### 7.4 «Pending type confirmation накапливается»

1. `lic_pending_state_count` высокий → пользователи не подтверждают.
2. Проверить frontend — отображается ли запрос подтверждения корректно?
3. Проверить SSE delivery в Orchestrator.
4. Орг-вопрос: возможно, пользователи в outage / weekend.

---

## 8. Логи как audit trail

См. `security.md` §10. Кратко:

- LIC сам в БД ничего не пишет.
- Audit достигается комбинацией: structured logs (90 дней) + Prometheus metrics (30 дней) + DM AuditRecord (за каждое сохранение `lic.artifacts.analysis-ready`) + OTel traces (14 дней).
- Compliance с NFR-3.4 / NFR-3.5 — обеспечивается на уровне infrastructure (Loki/Elasticsearch retention).

---

## 9. Self-check

- [x] Structured logs с allowlist полей (PII redaction).
- [x] Prometheus metrics: pipeline, agent, LLM, DM, idempotency, pending, DLQ — с разумной cardinality.
- [x] OpenTelemetry tracing с W3C Trace Context propagation.
- [x] Дашборды: Health, Agents, Providers, Internals, DLQ.
- [x] Алёрты на pipeline, providers, cost, DLQ, dependencies.
- [x] Runbook для основных инцидентов.
- [x] Audit trail через комбинацию logs + metrics + DM + OTel.
