# Error Handling и устойчивость Legal Intelligence Core

Документ описывает классификацию ошибок, retry policy, repair loop, provider fallback, timeout policy, graceful degradation, DLQ, формат `DomainError` и health checks.

---

## 1. Внешние и внутренние статусы

См. `high-architecture.md` §2.1.3. Краткое резюме:

### 1.1 Внешние статусы (для Orchestrator)

LIC публикует только три:

| Статус | Описание |
|--------|----------|
| `IN_PROGRESS` | LIC начал / продолжает анализ |
| `COMPLETED` | LIC завершил анализ, артефакты в DM |
| `FAILED` | LIC не смог завершить (вместе с error_code, error_message, is_retryable) |

Маппинг единого набора статусов (см. ТЗ-обязательства):
- `QUEUED` → не публикуется (LIC сразу IN_PROGRESS).
- `COMPLETED_WITH_WARNINGS` → COMPLETED (warnings внутри `DETAILED_REPORT.warnings`).
- `TIMED_OUT` → FAILED + `error_code=ANALYSIS_TIMEOUT`.
- `REJECTED` → FAILED + `error_code=INPUT_REJECTED` (например, артефакт не пришёл от DM).

### 1.2 Внутренние стадии

`STAGE_*` (см. high-architecture §2.1.3) — публикуются опционально в `lic.events.status-changed.stage` для observability, но не определяют внешний статус.

---

## 2. `DomainError` — формат типизированных ошибок

### 2.1 Контракт

```go
package model

type DomainError struct {
    Code        ErrorCode  // машинный код
    UserMessage string     // на русском, для NFR-5.2 — пробрасывается в lic.events.status-changed.error_message
    DevMessage  string     // на английском, для логов
    Retryable   bool       // is_retryable
    Stage       Stage      // на какой стадии произошла ошибка
    Cause       error      // wrapped, для errors.Is/As
    Attributes  map[string]any  // optional: agent_id, provider_id, ...
}

func (e *DomainError) Error() string { return e.DevMessage }
func (e *DomainError) Unwrap() error { return e.Cause }
```

### 2.2 Использование

```go
return nil, &DomainError{
    Code:        ErrAnalysisTimeout,
    UserMessage: "Анализ занял слишком много времени. Запустите повторную проверку.",
    DevMessage:  fmt.Sprintf("pipeline timeout exceeded (job_id=%s)", jobID),
    Retryable:   true,
    Stage:       StageAgentRiskDetection,
    Cause:       ctx.Err(),
    Attributes:  map[string]any{"agent_id": "AGENT_RISK_DETECTION", "elapsed_ms": 90000},
}
```

Pipeline Orchestrator маппит `DomainError` в `LICStatusChangedEvent` с `status=FAILED`.

---

## 3. Каталог error codes

### 3.1 Inbound errors (на стороне consumer)

| `error_code` | Stage | Retryable | UserMessage (RU) |
|--------------|-------|-----------|------------------|
| `INVALID_MESSAGE_SCHEMA` | RECEIVE | false | (только в DLQ; не публикуется в Orch — события из DM считаются доверенными) |
| `INVALID_ORG_ID_MISMATCH` | RECEIVE | false | (DLQ; alert) |

### 3.2 DM-related errors

| `error_code` | Stage | Retryable | UserMessage |
|--------------|-------|-----------|-------------|
| `DM_ARTIFACTS_TIMEOUT` | REQUESTING_ARTIFACTS | true | «Не удалось получить данные документа за отведённое время. Попробуйте ещё раз.» |
| `DM_ARTIFACTS_MISSING` | ARTIFACTS_RECEIVED | true | «Данные документа не найдены. Возможно, обработка ещё не завершилась.» |
| `DM_PERSIST_FAILED` | PUBLISHING_ARTIFACTS | false (если DM is_retryable=false) / true (иначе) | «Не удалось сохранить результат анализа.» (если non-retryable: «Документ был удалён или недоступен.») |
| `DM_PERSIST_TIMEOUT` | AWAITING_DM_CONFIRMATION | true | «Не удалось получить подтверждение сохранения. Попробуйте позже.» |

### 3.3 Agent-related errors

| `error_code` | Stage | Retryable | UserMessage |
|--------------|-------|-----------|-------------|
| `AGENT_OUTPUT_INVALID` | AGENT_* | true | «Не удалось получить корректный анализ. Запустите повторную проверку.» |
| `AGENT_TIMEOUT` | AGENT_* | true | «Один из этапов анализа занял слишком много времени.» |
| `AGENT_INPUT_TOO_LARGE` | AGENT_* | false | «Документ слишком большой для анализа. Разделите его на части.» |
| `AGENT_DEPENDENCY_FAILED` | AGENT_* | (зависит от cause) | «Не удалось завершить анализ из-за сбоя на предыдущем этапе.» |

### 3.4 LLM-provider errors

| `error_code` | Stage | Retryable | UserMessage |
|--------------|-------|-----------|-------------|
| `LLM_ALL_PROVIDERS_FAILED` | AGENT_* | true | «Сервис анализа временно недоступен. Попробуйте позже.» |
| `LLM_QUOTA_EXCEEDED` | AGENT_* | false (escalate) | «Превышен лимит запросов к ИИ-сервису. Обратитесь к администратору.» |
| `LLM_CONTENT_POLICY_VIOLATION` | AGENT_* | false | «Документ содержит контент, который не может быть обработан.» |

### 3.5 Pipeline-level errors

| `error_code` | Stage | Retryable | UserMessage |
|--------------|-------|-----------|-------------|
| `ANALYSIS_TIMEOUT` | (job-level) | true | «Анализ занял слишком много времени. Запустите повторную проверку.» |
| `INPUT_REJECTED` | RECEIVE / ARTIFACTS_RECEIVED | false | «Полученные данные документа повреждены или неполные.» |
| `DOCUMENT_TOO_LARGE` | ARTIFACTS_RECEIVED | false | «Документ слишком большой для юридического анализа.» |

### 3.6 User-confirmation errors

| `error_code` | Stage | Retryable | UserMessage |
|--------------|-------|-----------|-------------|
| `USER_CONFIRMATION_EXPIRED` | AWAITING_USER_CONFIRMATION | false | «Время на подтверждение типа договора истекло. Запустите проверку заново.» |
| `INVALID_CONTRACT_TYPE` | AWAITING_USER_CONFIRMATION | false | «Указан некорректный тип договора. Обновите страницу и попробуйте снова.» |

### 3.7 Internal errors

| `error_code` | Stage | Retryable | UserMessage |
|--------------|-------|-----------|-------------|
| `INTERNAL_ERROR` | * | true | «Произошла внутренняя ошибка. Попробуйте позже.» |
| `IDEMPOTENCY_STORE_UNAVAILABLE` | RECEIVE | true | (NACK, не публикуется в Orch немедленно) |

---

## 4. Retry policy

### 4.1 Уровни retry

| Уровень | Кто | Описание |
|---------|-----|----------|
| 1. LLM HTTP-call retry | `LLMProviderPort` impl | Connection errors, 5xx, 429 — 1 retry с backoff (200ms). |
| 2. Provider fallback | Provider Router | После исчерпания retry в primary — переход к fallback (Claude → OpenAI → Gemini). |
| 3. Repair loop | Agent | Невалидный JSON — 1 repair-вызов с подсказкой. |
| 4. Consumer-level retry | RabbitMQ | NACK с requeue → broker возвращает сообщение в очередь. Конфигурируется `LIC_CONSUMER_MAX_REDELIVERIES=3`. |
| 5. Pipeline retry | (отсутствует) | LIC v1 не реплеит весь пайплайн на job-level. Если job FAILED — Orchestrator принимает решение (показать пользователю кнопку «повторить»). |

### 4.2 Retry budget per job

```
- Per LLM-call: 1 retry on same provider + до 2 fallback providers = до 3 attempts на 1 stage.
- Repair loop: 1 retry на каждом агенте с невалидным JSON.
- Consumer-level: до 3 redeliveries сообщения.
- Job-level (overall): controlled by LIC_JOB_TIMEOUT=90s — глобальный context deadline.
```

Total wall-clock time для одного job: ≤ 90 секунд (включая все retries). При превышении — `ANALYSIS_TIMEOUT`.

### 4.3 Backoff

| Тип ошибки | Backoff |
|-----------|---------|
| LLM 5xx / network | 200ms |
| LLM 429 (rate limit) | `Retry-After` header value, или 1s если отсутствует |
| LLM 529 Anthropic-overloaded | 500ms |
| Repair loop | 0ms (immediate, с обновлённым prompt) |
| Consumer NACK | RabbitMQ default (зависит от broker config; LIC ставит `x-message-ttl` per attempt через DLX-loop) |

### 4.4 Когда retry бессмысленен (non-retryable)

- 401 Unauthorized (API key) — escalate в alerting, не retry.
- 400 Bad Request с content_policy_violation — fail agent.
- 400 context_length_exceeded — fail agent (ввод должен был быть усечён до вызова).
- DM `is_retryable=false` (например, `DOCUMENT_NOT_FOUND`) — не retry.
- `INVALID_CONTRACT_TYPE` от UserConfirmedType — не retry.

---

## 5. Repair loop (детальная логика)

### 5.1 Триггер

Schema Validator детектирует:
- Не-JSON ответ.
- JSON есть, но `additionalProperties: false` нарушено.
- Required поля отсутствуют.
- Type mismatch (например, `confidence` — строка вместо number).
- Enum mismatch (`contract_type: "EXOTIC"` — нет в whitelist).

### 5.2 Repair-prompt

```
Твой предыдущий ответ не прошёл валидацию по схеме.

Ошибки валидации:
{validation_errors_pretty_printed}

Исправь ответ. Возвращай ТОЛЬКО валидный JSON по исходной схеме, без объяснений и
preamble. Не добавляй markdown. Не цитируй ошибки в ответе.
```

User message предыдущего вызова — сохраняется. Добавляется assistant message с raw response, затем user message с текстом repair-prompt.

### 5.3 Параметры repair-вызова

- Тот же провайдер (не fallback на этом этапе).
- Та же модель.
- Temperature: 0.0 (детерминизм).
- Max tokens: те же.
- Timeout: тот же.

### 5.4 Лимит итераций

Жёсткий лимит — 1 repair. Дальнейшие итерации вероятностно не помогают (модель «застревает» в неверном паттерне) и удлиняют latency.

### 5.5 Метрики

- `lic_agent_repair_attempts_total{agent}` counter.
- `lic_agent_repair_outcome_total{agent, outcome}` (`outcome` ∈ `repaired_ok | repair_failed`).

---

## 6. Provider fallback (детальная логика)

См. `llm-provider-abstraction.md` §2. Краткое резюме:

```
Order: agent.PrimaryProvider → LIC_PROVIDER_FALLBACK_ORDER (без дублей)
For each provider in chain:
  if !healthy(provider): skip + metric
  if rateLimit(provider): skip + metric
  resp, err := provider.Complete(req)
  if err == nil: return resp [if i>0: metric fallback_total]
  if !isRetryable(err): return err  // fatal — не fallback
  log + continue
return ErrLLMAllProvidersFailed
```

### 6.1 Когда fallback не выполняется

- При `ErrLLMInvalidAPIKey`, `ErrLLMQuotaExceeded`, `ErrLLMContentPolicyViolation`, `ErrLLMContextTooLong`, `ErrLLMMalformedRequest` — fatal, не fallback.

### 6.2 Алёрты

```
ALERT LICAllProvidersFailing
  IF rate(lic_llm_calls_total{outcome="fail"}[5m]) by (provider) ==
     rate(lic_llm_calls_total[5m]) by (provider)   // 100% failure
  FOR 2m
  SEVERITY critical
  MESSAGE "Provider {{.provider}}: 100% failure rate за 5 мин"
```

---

## 7. Timeout policy

### 7.1 Таблица таймаутов

| Точка | Env | Default |
|-------|-----|---------|
| LLM HTTP request (single attempt) | `LIC_LLM_REQUEST_TIMEOUT` | 60s |
| Per-agent timeout | См. ниже | 5–12s (в зависимости от агента) |
| DM artifact request response | `LIC_DM_REQUEST_TIMEOUT` | 30s |
| DM persist confirmation | `LIC_DM_PERSIST_CONFIRM_TIMEOUT` | 30s |
| Pipeline overall (job-level) | `LIC_JOB_TIMEOUT` | 90s |
| Consumer message TTL | (queue-level x-message-ttl) | 86400000ms (24h) |
| Pending type confirmation | `LIC_PENDING_CONFIRMATION_TTL` | 25h (Redis TTL) |

### 7.2 Per-agent timeouts

| Агент | Timeout |
|-------|---------|
| 1. Type Classifier | 5s |
| 2. Key Parameters Extractor | 8s |
| 3. Party Consistency | 6s |
| 4. Mandatory Conditions | 8s |
| 5. Risk Detection | 12s |
| 6. Recommendation | 10s |
| 7. Business Summary | 6s |
| 8. Detailed Report | 12s |
| 9. Risk Delta | 8s |

### 7.3 Иерархия таймаутов

`context.WithTimeout` иерархичен:
- Job-level context: 90s.
- Stage context: derived (наследует deadline от parent + per-stage budget).
- Agent context: derived (наследует от stage + agent-specific timeout).
- LLM HTTP request context: derived (наследует от agent — то есть LLM не превысит agent timeout).

При истечении job-level deadline — все nested context'ы cancel'ятся, in-flight LLM-вызовы прерываются.

---

## 8. Graceful degradation

### 8.1 Сценарии деградации

| Сценарий | Поведение | Не-fatal? |
|----------|-----------|----------|
| Один LLM-провайдер недоступен | Fallback на другой | да |
| Все LLM-провайдеры недоступны | FAILED `LLM_ALL_PROVIDERS_FAILED` | нет |
| Redis недоступен (idempotency) | Fallback: обработка без idempotency check (риск дубликата); метрика `lic_idempotency_fallback_total` + alert | да (degraded) |
| Redis недоступен (pending state) | Pending state не сохраняется → при confidence < threshold возвращаем FAILED `INTERNAL_ERROR` (нельзя реализовать pause) | нет |
| RabbitMQ недоступен | Consumer cycles reconnect; publish буферизуется в памяти до `LIC_PUBLISH_BUFFER_SIZE` | (degraded) |
| DP processing_warnings отсутствуют | Pipeline продолжается с предположением «всё ОК» | да |
| Parent RISK_ANALYSIS отсутствует (RE_CHECK) | Skip Stage 6 + warning в DETAILED_REPORT | да |
| Один из non-critical агентов (3, 9) timed out | Skip + warning | да |
| Один из tier-2 агентов (2, 4, 6, 7) failed | FAILED pipeline `is_retryable=true` | нет |
| Один из critical агентов (1, 5, 8) failed | FAILED pipeline `is_retryable=true` | нет |

### 8.2 Принципы degradation

1. **Прозрачность для пользователя.** Любая degradation отмечается warning в `DETAILED_REPORT.warnings`. Пользователь видит, что результат неполный.
2. **Безопасность.** Деградация **не** заполняет «фейковыми» данными отсутствующие артефакты.
3. **Метрики.** Каждый degradation-сценарий имеет свою метрику для observability.

---

## 9. DLQ-стратегия

### 9.1 Топики

См. `event-catalog.md` §3.2:
- `lic.dlq.invalid-message` — невалидная схема входящего сообщения.
- `lic.dlq.consumer-failed` — исчерпан retry.
- `lic.dlq.publish-failed` — не удалось опубликовать исходящее.
- `lic.dlq.agent-output-invalid` — LLM вернул невалидный JSON после repair.

### 9.2 Структура DLQ-сообщения

См. `event-catalog.md` §3.1.

### 9.3 Обработка DLQ

DLQ-сообщения **не обрабатываются автоматически**. Они архивируются для пост-мортем анализа:
- Эксплуатация / SRE мониторят DLQ-метрики (`rabbitmq_queue_messages{queue=~"lic.dlq.*"}`).
- При накоплении DLQ-сообщений выше threshold — alert.
- Манус для post-mortem: SRE инспектирует DLQ через RabbitMQ Management UI / CLI; решение по сообщению (re-enqueue / архивация / drop).

### 9.4 Retention DLQ

`x-message-ttl` для DLQ queues — `7 дней` (`LIC_DLQ_TTL=604800000`). После истечения — auto-drop.

---

## 10. Health checks

### 10.1 `/healthz` (liveness)

- Проверяет, что процесс жив и event loop отвечает.
- Простой 200 OK без проверок зависимостей (live ≠ ready).
- Используется kube-probe для restart на crash/hang.

### 10.2 `/readyz` (readiness)

- Проверяет:
  - RabbitMQ connection: `Connection.IsClosed() == false`.
  - Redis: `PING` за 100ms.
  - Хотя бы один LLM-провайдер healthy (light healthcheck per provider, см. `llm-provider-abstraction.md` §2.3).
- Возвращает 503, если хоть одна критическая зависимость недоступна.
- Kube-probe убирает pod из service endpoints при 503.

### 10.3 Поведение при зависимостях

| Зависимость | Влияние на /readyz |
|-------------|---------------------|
| RabbitMQ down | 503 (cannot consume / publish) |
| Redis down | 503 (cannot guard idempotency / pause state) |
| All LLM providers down | 503 (cannot do any work) |
| One LLM provider down (others up) | 200 (fallback работает) |

---

## 11. Алёрты

| Alert | Условие | Severity |
|-------|---------|----------|
| `LICPipelineFailureRate` | > 10% jobs failed за 5 мин | warning |
| `LICAllProvidersFailing` | 100% failure rate per provider за 2 мин | critical |
| `LICCostSpike` | > $100/час | warning |
| `LICDLQGrowth` | > 100 messages в DLQ за 1 час | warning |
| `LICAgentOutputInvalidRate` | > 5% repair_failed за 30 мин | warning |
| `LICUserConfirmationExpiredRate` | > 50% expirations | info |
| `LICRedisDown` | Redis ping fails for 1 min | critical |
| `LICRabbitMQDown` | RabbitMQ disconnect for 1 min | critical |
| `LICPipelineDuration` | p95 > 60 sec | warning |
| `LICStuckPendingState` | pending state TTL nearly expired (<2h) и нет user confirm | info |

См. также `observability.md`.

---

## 12. Restart и graceful shutdown

### 12.1 Сигналы

LIC реагирует на:
- `SIGTERM` / `SIGINT` — graceful shutdown.
- `SIGHUP` — re-read config (env), reload secrets, без рестарта (см. `llm-provider-abstraction.md` §6.3).

### 12.2 Graceful shutdown sequence

1. `/readyz` начинает возвращать 503 (kube перестаёт направлять трафик).
2. RabbitMQ consumer останавливает приём новых сообщений (`channel.Cancel`).
3. Ждать завершения in-flight pipelines (с deadline `LIC_SHUTDOWN_TIMEOUT=120s`).
4. Если deadline истёк — оставшиеся сообщения NACK с requeue (попадают другому инстансу).
5. Закрыть RabbitMQ connection.
6. Закрыть Redis connection.
7. Flush OpenTelemetry traces.
8. Exit 0.

### 12.3 Crash recovery

- Stateless архитектура: при crash инстанса pending state — в Redis, идемпотентность — в Redis. Новый инстанс подхватывает работу без потерь.
- In-flight pipeline: при crash во время LLM-вызова — сообщение возвращается в очередь (NACK без ack) → новый инстанс начнёт пайплайн заново. Idempotency Guard видит `lic-trigger:{version_id}` со статусом `PROCESSING` (с TTL 90s); при повторе после TTL — обработка идёт по новой.

---

## 13. Self-check

- [x] Внешние статусы (3) и внутренние стадии (`STAGE_*`) разделены.
- [x] DomainError-формат с UserMessage (RU), DevMessage (EN), Retryable, Stage, Cause.
- [x] Каталог error_code покрывает все классы ошибок.
- [x] Retry policy на 5 уровней с описанием budget.
- [x] Repair loop × 1 для невалидного JSON.
- [x] Provider fallback с healthcheck + circuit breaker.
- [x] Timeout policy иерархична (job → stage → agent → LLM HTTP).
- [x] Graceful degradation: critical / tier-2 / non-critical агенты.
- [x] DLQ-стратегия и retention.
- [x] Health checks разделены на liveness / readiness.
- [x] Алёрты покрывают cost, failure rate, DLQ growth, dependencies.
- [x] Graceful shutdown и crash recovery описаны.
