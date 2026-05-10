# Абстракция LLM-провайдера в LIC

Документ описывает абстракцию LLM-провайдера (`LLMProviderPort`), реализации для Claude / OpenAI / Gemini, Provider Router (стратегия выбора + fallback), rate limiting, cost & usage tracking, кэширование и управление секретами.

Принципы:
- Замена провайдера = добавление нового адаптера, **без изменений в коде агентов**.
- Все провайдерские особенности (формат сообщений, системного промпта, tool use, response parsing) скрыты за единым контрактом.
- Тенант-isolated: каждый вызов несёт `organization_id` в OTel attributes (но не в самих сообщениях для LLM).

---

## 1. Интерфейс `LLMProviderPort`

### 1.1 Контракт (Go)

```go
package port

type LLMProviderID string

const (
    ProviderClaude LLMProviderID = "claude"
    ProviderOpenAI LLMProviderID = "openai"
    ProviderGemini LLMProviderID = "gemini"
)

type Role string

const (
    RoleSystem    Role = "system"
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
)

type Message struct {
    Role    Role
    Content string  // текст; multimodal не поддерживается в v1
}

type CompletionRequest struct {
    AgentID     AgentID         // metadata (для метрик и логов; не передаётся в провайдер)
    Model       string          // конкретная модель: claude-sonnet-4-6, gpt-4.1-mini, gemini-2.5-pro, ...
    Messages    []Message       // первая обязана быть Role=System
    MaxTokens   int             // upper bound на output
    Temperature float64         // 0..1
    JSONMode    bool            // принудительный JSON-режим (если поддерживается)
    StopSequences []string      // optional
    Metadata    Metadata        // OTel attributes: correlation_id, job_id, version_id, organization_id, agent_id
}

type Metadata struct {
    CorrelationID  string
    JobID          string
    VersionID      string
    OrganizationID string
    AgentID        AgentID
}

type CompletionResponse struct {
    Content      string         // raw content (до schema validation)
    InputTokens  int
    OutputTokens int
    StopReason   string         // "end_turn", "max_tokens", "stop_sequence"
    LatencyMs    int64
    ProviderID   LLMProviderID
    Model        string
}

type LLMProviderPort interface {
    ID() LLMProviderID
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
    HealthCheck(ctx context.Context) error  // light-weight check для /readyz
}
```

### 1.2 Семантика ошибок

`Complete` возвращает либо успех, либо ошибку из набора:

| Ошибка | Retryable | Описание |
|--------|-----------|----------|
| `ErrLLMTimeout` | yes | HTTP timeout (>60s) или context cancelled |
| `ErrLLMRateLimit` | yes | 429 от провайдера (с `Retry-After`) |
| `ErrLLMServerError` | yes | 5xx |
| `ErrLLMNetwork` | yes | TCP reset, DNS error |
| `ErrLLMOverloaded` | yes | Anthropic-specific 529 |
| `ErrLLMInvalidAPIKey` | no (fatal) | 401 — escalate, ALERT |
| `ErrLLMQuotaExceeded` | no (fatal) | quota exhausted — escalate, ALERT |
| `ErrLLMContentPolicy` | no | 400 content_policy_violation — fail agent (`is_retryable=false`) |
| `ErrLLMContextTooLong` | no | input exceeds context window — fail agent (`is_retryable=false`); LIC должен был усечь до вызова |
| `ErrLLMMalformedRequest` | no | 400 — баг в коде LIC, escalate в логи + DLQ |

Реализации провайдеров маппят native errors на эти типизированные.

### 1.3 Реализации (контурно)

| Адаптер | Endpoint | Default model | SDK |
|---------|----------|---------------|-----|
| `claudeProvider` | `https://api.anthropic.com/v1/messages` | `claude-sonnet-4-6` | официальный `anthropic-go` SDK или ручной HTTP |
| `openaiProvider` | `https://api.openai.com/v1/responses` | `gpt-4.1` | официальный `openai-go` SDK |
| `geminiProvider` | `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent` | `gemini-2.5-pro` | ручной HTTP (Google Go SDK для GenAI на стадии стабилизации; в v1 — REST) |

> Версии моделей фиксируются в env (`LIC_CLAUDE_MODEL`, `LIC_OPENAI_MODEL`, `LIC_GEMINI_MODEL`). Смена версии — через перезапуск сервиса (rolling). Смена major модели — отдельная задача с регресс-тестированием на эталонных договорах.

### 1.4 Размещение системного промпта

| Провайдер | Где живёт системный промпт |
|-----------|----------------------------|
| Claude | Поле `system` в Messages API |
| OpenAI | Первое сообщение `role: developer` (Responses API) или `role: system` (Chat Completions, deprecated) |
| Gemini | Поле `systemInstruction` в `generateContent` |

Адаптер сам трансформирует первый Message с `Role=System` → нативный формат.

### 1.5 JSON-режим

| Провайдер | Реализация JSON Mode |
|-----------|----------------------|
| Claude | Strict JSON schema через `tool_use` (специальный «virtual tool» с input_schema = JSON Schema агента) |
| OpenAI | `response_format: {type: "json_schema", strict: true, schema: ...}` |
| Gemini | `responseSchema` в `generationConfig` |

Когда JSON Mode не поддерживается провайдером (legacy models) — `JSONMode=true` игнорируется адаптером, валидация остаётся на стороне Schema Validator + Repair Loop.

### 1.6 Multimodal в v1

Не поддерживается. `Content` — только text. Если в будущем потребуется анализ сканов изображений — добавим `Parts []ContentPart` без breaking change (через optional поле в `Message`).

---

## 2. Provider Router

### 2.1 Стратегия выбора (per-agent + global fallback)

```
                  +-------------------+
                  |   Agent Run()     |
                  +---------+---------+
                            |
                            v
                  +-------------------+
                  |  Provider Router   |
                  +---------+---------+
                            |
        ┌───────────────────┼───────────────────┐
        |                   |                    |
        v                   v                    v
  agent.PrimaryProvider   FALLBACK_ORDER[1]   FALLBACK_ORDER[2]
   (e.g. claude)            (e.g. openai)       (e.g. gemini)
```

**Логика выбора (псевдокод):**

```go
func (r *Router) Complete(ctx, req) (Response, error) {
    primary := r.config.AgentPrimary[req.AgentID]
    chain := []LLMProviderID{primary}
    for _, p := range r.config.GlobalFallbackOrder {
        if p != primary {
            chain = append(chain, p)
        }
    }

    var lastErr error
    for i, providerID := range chain {
        provider := r.providers[providerID]
        if !r.healthy(providerID) {
            metric.Inc("lic_llm_provider_skipped_unhealthy_total{provider=" + string(providerID) + "}")
            continue
        }
        if err := r.rateLimit(ctx, providerID); err != nil {
            lastErr = err
            continue
        }
        resp, err := provider.Complete(ctx, req)
        if err == nil {
            if i > 0 {
                metric.Inc("lic_llm_provider_fallback_total{from=" + string(primary) + ",to=" + string(providerID) + "}")
            }
            return resp, nil
        }
        if !isRetryable(err) {
            return Response{}, err  // fatal — не пытаемся fallback
        }
        lastErr = err
        metric.Inc("lic_llm_provider_failed_total{provider=" + string(providerID) + ",reason=" + classify(err) + "}")
    }
    return Response{}, fmt.Errorf("all providers exhausted: %w", lastErr)
}
```

### 2.2 Конфигурация per-agent

```env
LIC_AGENT_TYPE_CLASSIFIER_PROVIDER=claude
LIC_AGENT_KEY_PARAMS_PROVIDER=claude
LIC_AGENT_PARTY_CONSISTENCY_PROVIDER=claude
LIC_AGENT_MANDATORY_CONDITIONS_PROVIDER=claude
LIC_AGENT_RISK_DETECTION_PROVIDER=claude
LIC_AGENT_RECOMMENDATION_PROVIDER=claude
LIC_AGENT_SUMMARY_PROVIDER=claude
LIC_AGENT_DETAILED_REPORT_PROVIDER=claude
LIC_AGENT_RISK_DELTA_PROVIDER=claude
LIC_PROVIDER_FALLBACK_ORDER=claude,openai,gemini
```

В v1 — все default на claude. Изменение — через env, без ребилда.

### 2.3 Health check

Каждый провайдер выставляет `HealthCheck(ctx)`:
- Claude: light request `POST /v1/messages` с минимальным prompt (`max_tokens=10`, sample message). На staging — раз в 60s, на проде — раз в 30s.
- OpenAI: то же на `/v1/responses`.
- Gemini: то же на `:generateContent`.

Provider Router держит in-memory state: `provider → {healthy: bool, last_check_at, consecutive_failures}`. При `consecutive_failures >= 3` → mark unhealthy на 60s. Авто-recovery после успешного healthcheck.

`/readyz` — OK, если хотя бы один провайдер healthy. Если все unhealthy — `/readyz` 503, kube-probe останавливает консьюмер.

---

## 3. Rate Limiting (per-provider)

### 3.1 Token bucket в Redis

Алгоритм: leaky bucket / token bucket с атомарным Lua-скриптом.

```
Key: lic:rate:{provider}:{shard}
Value: { tokens, last_refill }
TTL: max(window, 60s)

Lua:
  -- atomically get current tokens, refill based on elapsed, decrement, set
  -- return: 1 = allowed, 0 = denied (with retry-after)
```

`shard` — `org_id_hash%4` для чуть большего параллелизма без существенного нарушения суммарной квоты (опц.).

### 3.2 Параметры

```env
LIC_LLM_RPS_CLAUDE=10
LIC_LLM_RPS_OPENAI=20
LIC_LLM_RPS_GEMINI=20
LIC_LLM_BURST_CLAUDE=20
LIC_LLM_BURST_OPENAI=40
LIC_LLM_BURST_GEMINI=40
LIC_LLM_CONCURRENCY_PER_PROVIDER=10
```

При превышении bucket → `ErrLLMRateLimit` (retryable). Provider Router ждёт `Retry-After` (если получен от провайдера) или применяет backoff `200ms * 2^attempt + jitter`.

### 3.3 Защита от cascade

В дополнение к token bucket — circuit breaker (gobreaker) для каждого провайдера:
- Half-open после 30 секунд open state.
- 50% failure rate в окне 60 секунд → open.

При open state — provider помечается unhealthy, fallback идёт на следующий.

---

## 4. Cost & Usage Tracking

### 4.1 Метрики

После каждого успешного `Complete`:

```
lic_llm_input_tokens_total{provider, agent}     counter
lic_llm_output_tokens_total{provider, agent}    counter
lic_llm_calls_total{provider, agent, outcome}   counter  (outcome ∈ success|repair|fail|fallback)
lic_llm_latency_seconds{provider, agent}        histogram
lic_llm_cost_usd_total{provider, agent}         counter
```

Cost рассчитывается из встроенной таблицы цен (per-provider, per-model, input/output):

```go
type ModelPricing struct {
    InputPerMTokenUSD  float64
    OutputPerMTokenUSD float64
}

var pricingTable = map[string]ModelPricing{
    "claude-sonnet-4-6": {InputPerMTokenUSD: 3.00, OutputPerMTokenUSD: 15.00},
    "gpt-4.1":           {InputPerMTokenUSD: 2.50, OutputPerMTokenUSD: 10.00},
    "gemini-2.5-pro":    {InputPerMTokenUSD: 1.25, OutputPerMTokenUSD: 5.00},
    // ... обновляется через config map
}
```

> Цены в таблице — иллюстративные (на момент проектирования). Поддержка через `LIC_PRICING_TABLE_PATH=/etc/lic/pricing.yaml` (горячая перезагрузка не требуется в v1; при изменении — restart).

### 4.2 Per-organization агрегация

Для биллинга / аналитики — labels включают `organization_id`. Это создаёт высокую кардинальность; mitigation:
- В Prometheus — выставить retention 30 дней для метрик с `org_id`.
- Альтернатива: пишем в OTel-events, агрегация в Tempo / Jaeger / специальную трубу для биллинга. В v1 — ограничиваемся Prometheus (~10 К организаций × 9 агентов × 3 провайдера = 270К серий — приемлемо).

> Отдельной биллинговой трубы в LIC v1 не требуется (нет Payment Processing-интеграции). Метрики используются для контроля стоимости со стороны эксплуатации.

### 4.3 Алёрты по стоимости

```
ALERT LICCostSpike
  IF rate(lic_llm_cost_usd_total[1h]) > 100
  FOR 30m
  SEVERITY warning
  MESSAGE "LIC LLM cost rate exceeded $100/hour for 30 minutes; investigate"
```

---

## 5. Кэширование

### 5.1 Системный промпт через провайдерскую prompt-cache

| Провайдер | Поддержка |
|-----------|-----------|
| Claude | Anthropic Prompt Caching API: ставим `cache_control: {type:"ephemeral"}` на system message |
| OpenAI | Implicit prompt caching на префиксы; контролируется автоматически |
| Gemini | `cachedContent` (отдельный API endpoint для длительного кэша) — в v1 не используется |

Системные промпты агентов длинные (5–10 К токенов). Кэширование сокращает input cost на 90% при cache hit (по бенчмаркам Anthropic). Для high-throughput LIC это значимая экономия.

Реализация в `claudeProvider`: при формировании запроса — добавляем `cache_control` маркер на system block. TTL — 5 минут (Anthropic default ephemeral cache). При хорошей нагрузке (>1 запрос за 5 минут) cache hits.

### 5.2 Кэширование результатов LIC по контракту — отключено

Кэш «контракт → классификация» **не используется** (см. ASSUMPTION-LIC-15):
- Договоры уникальны (даже одна и та же организация редко загружает идентичные документы дважды).
- Hash-on-content не работает (минимальные правки → другой hash).
- Cross-tenant утечки результатов недопустимы (сегрегация tenant).

**Исключение:** при `origin_type=RE_CHECK` — никакого кэширования; полная переоценка (по требованию пользователя).

### 5.3 Кэш `origin_type` для RE_CHECK

См. high-architecture §6.10 — Redis ключ `lic-version-meta:{version_id}` с TTL 24h. Это **не кэш LLM-результатов**, а кэш metadata из `dm.events.version-created` для определения режима.

---

## 6. Управление секретами

### 6.1 API-ключи

```env
LIC_CLAUDE_API_KEY=...
LIC_OPENAI_API_KEY=...
LIC_GEMINI_API_KEY=...
```

**Никогда** в логах, конфиг-дампах, error-сообщениях. Логирование через структурированный logger с redaction-фильтром (см. `security.md`).

### 6.2 Источник секретов

| Среда | Источник |
|-------|----------|
| Local dev | `.env` файл (gitignore) |
| Staging | Yandex Lockbox (Secrets Manager) с инжекцией через CSI driver |
| Production | Yandex Lockbox + KMS-encrypted volume на podе |

В коде — через `os.Getenv()` или специальный SecretsProvider с поддержкой реролл по сигналу (HUP).

### 6.3 Ротация

- Текущая стратегия: ручная ротация каждые 90 дней (NIST recommendation).
- Хот-реролл: SIGHUP → перечитать env → пересоздать HTTP-клиенты с новым Authorization header. Не теряет in-flight запросы (graceful).

### 6.4 Аудит

Каждый успешный/неуспешный LLM-вызов логируется (без тела — см. `security.md` §6 redaction):
```
{
  "ts":"2026-05-06T12:00:01Z","level":"info","logger":"llm.claude",
  "correlation_id":"...","job_id":"...","organization_id":"...",
  "agent_id":"AGENT_RISK_DETECTION","model":"claude-sonnet-4-6",
  "input_tokens":15234,"output_tokens":2891,"latency_ms":4321,
  "outcome":"success","provider":"claude"
}
```

---

## 7. Data residency и compliance

### 7.1 Юрисдикции LLM-провайдеров

| Провайдер | Регионы (relevant) |
|-----------|--------------------|
| Anthropic | US (default), AWS Bedrock в EU/Asia |
| OpenAI | US (default), Azure в EU |
| Gemini | Multi-region (US, EU, Asia) |

### 7.2 Соответствие 152-ФЗ (ПДн)

ContractPro — RU-юрисдикция. Договоры могут содержать ПДн (ФИО, паспортные данные, адреса). При отправке в зарубежные LLM — формально это трансграничная передача ПДн.

**Решение для v1 (ASSUMPTION-LIC):** В системе ContractPro (на уровне регистрации клиента) собирается **явное согласие пользователя** на использование AI-анализа с трансграничной передачей данных в LLM-провайдеров. Это происходит **вне LIC** — в Orchestrator/UOM. LIC получает корректные данные и их анализирует.

LIC **дополнительно**:
- Применяет PII redaction в **логах** (не в LLM-вызовах) — см. `security.md` §6. Для анализа сама ПДн нужна (договоры без ИНН/ФИО — невозможно проанализировать).
- Поддерживает per-tenant override провайдера (`LIC_AGENT_*_PROVIDER` через config) — если в будущем какому-то tenant'у регулятор запретит US-LLM, можно переключить на on-premise модель **за тот же** `LLMProviderPort` без изменений в коде.

> Для v2 предусмотрен on-premise провайдер (например, на базе self-hosted Llama / GigaChat). В v1 — не реализуется (YAGNI).

### 7.3 Anthropic Privacy

Anthropic API не использует входные данные для тренировки моделей (по политике Commercial Terms). Тоже у OpenAI (если флаг `data: don't train` — default для API).

---

## 8. Тестирование `LLMProviderPort`

### 8.1 Mock-провайдер

Для интеграционных тестов LIC используется `mockProvider`:
- Возвращает заранее заданные ответы по `(AgentID, Model)` keys.
- Поддерживает inject errors для тестирования retry / fallback.
- Учёт invocations для assertion.

### 8.2 Контрактные тесты

Для каждого реального провайдера — отдельный test suite (запускается nightly с реальными API keys):
- `TestProviderHappyPath`: классический запрос с валидным JSON-выходом.
- `TestProviderJSONSchemaCompliance`: проверка, что provider возвращает валидный JSON по нашей схеме.
- `TestProviderTimeout`: запрос с очень коротким `ctx` deadline.
- `TestProviderRateLimitHandling`: 100 запросов в секунду — проверка `ErrLLMRateLimit`.
- `TestProviderHealthCheck`: ping-style.

> Реальные API-ключи в CI — через encrypted secrets. Бюджет nightly tests — ограничен (не больше $5/ночь).

---

## 9. Future considerations

В v1 архитектура достаточна. При появлении новых требований:
- **Streaming**: интерфейс может быть расширен `CompleteStream(ctx, req) <-chan ContentPart`. В v1 streaming не нужен (пайплайн серверный, нет UI ожидания tokens).
- **Tool use / function calling**: для extra-validation шагов (например, агент вызывает «calculator» tool для проверки сумм). Не требуется в v1.
- **Embeddings**: для семантического сравнения версий или поиска похожих договоров. Не требуется в v1 — Risk Delta делает структурное сравнение.

> Согласно YAGNI-принципу LIC v1 — эти расширения **не добавляются** до появления реальной потребности.

---

## 10. Self-check

- [x] `LLMProviderPort` — единый контракт, реализуемый Claude / OpenAI / Gemini без изменений в коде агентов.
- [x] Provider Router — per-agent default + global fallback list (ADR-LIC-03).
- [x] Rate limiting — token bucket per provider в Redis + circuit breaker.
- [x] Cost & usage tracking — Prometheus metrics с per-agent / per-provider labels + alert на cost spike.
- [x] Кэширование — только prompt-caching для system messages; НЕ result caching.
- [x] Секреты — env + Lockbox + redaction.
- [x] Data residency — через явное согласие на трансграничную передачу + готовность переключиться на on-premise через адаптер.
- [x] Health checks — для `/readyz`.
- [x] Тестируемость — mockProvider + nightly contract tests.
