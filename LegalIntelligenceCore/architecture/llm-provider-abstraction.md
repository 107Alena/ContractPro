# Абстракция LLM-провайдера в LIC

Документ описывает абстракцию LLM-провайдера (`LLMProviderPort`), реализации для Claude / OpenAI / Gemini, Provider Router (стратегия выбора + fallback), rate limiting, cost & usage tracking, кэширование и управление секретами.

Принципы:
- Замена провайдера = добавление нового адаптера, **без изменений в коде агентов**.
- Все провайдерские особенности (формат сообщений, системного промпта, tool use, response parsing) скрыты за единым контрактом.
- Тенант-isolated: каждый вызов несёт `organization_id` в OTel attributes (но не в самих сообщениях для LLM).

---

## 1. Интерфейс `LLMProviderPort`

### 1.1 Контракт (Go)

Контракт спроектирован так, чтобы изолировать различия chat-форматов трёх провайдеров (Anthropic Messages API, OpenAI Responses API, Gemini `generateContent` с `systemInstruction` отдельно от `contents`). Вместо передачи готового массива сообщений (что течёт спецификой Anthropic/OpenAI и плохо ложится на Gemini), используется явное разделение `System` / `User` / `PriorTurns`. Адаптер каждого провайдера сам собирает свой нативный формат.

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
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
)

// Turn — один обмен в multi-turn-контексте (для repair-loop).
// Role=System в Turn НЕ допускается — system-prompt передаётся отдельным полем System.
type Turn struct {
    Role    Role
    Content string  // текст; multimodal не поддерживается в v1
}

// CompletionRequest — структурированный запрос к LLM-провайдеру.
// System / User / PriorTurns — отдельные поля; адаптер провайдера
// сам собирает нативный формат:
//   Claude: { system: System, messages: [...PriorTurns, {role:"user", content:User}] }
//   OpenAI: { input: [{role:"developer", content:System}, ...PriorTurns,
//                     {role:"user", content:User}] }
//   Gemini: { systemInstruction: {parts:[{text:System}]},
//             contents: [...PriorTurns, {role:"user", parts:[{text:User}]}] }
type CompletionRequest struct {
    AgentID       AgentID         // для метрик и логов; не передаётся в провайдер
    Model         string          // конкретная модель: claude-sonnet-4-6, gpt-4.1, gemini-2.5-pro, ...

    System        string          // системный промпт (зашитый промпт агента)
    User          string          // user-сообщение текущего turn'а (XML-обёрнутые данные)
    PriorTurns    []Turn          // optional; история turn'ов (для repair-loop); пустой при первом вызове

    MaxTokens     int             // upper bound на output
    Temperature   float64         // 0..1
    StopSequences []string        // optional

    JSONMode      bool            // флаг «вернуть JSON»; если JSONSchema указана, JSONMode подразумевается true
    JSONSchema    json.RawMessage // optional; strict structured outputs (JSON Schema draft-07).
                                  // Адаптеры с поддержкой передают в нативный API (Claude tool_use,
                                  // OpenAI response_format json_schema, Gemini responseSchema).
                                  // Адаптер без поддержки — игнорирует поле; валидация остаётся на
                                  // стороне Schema Validator + Repair Loop.
}

type CompletionResponse struct {
    Content           string         // raw content (до schema validation)
    InputTokens       int            // billable uncached input tokens
    CachedInputTokens int            // tokens served from provider prompt-cache (Anthropic);
                                     // 0 для OpenAI/Gemini в v1.
                                     // Cost & Usage Tracker учитывает отдельно (cache hit ≈ 10% от обычной цены).
    OutputTokens      int
    StopReason        StopReason     // typed enum
    LatencyMs         int64
    ProviderID        LLMProviderID
    Model             string
}

type StopReason string

const (
    StopReasonEndTurn      StopReason = "end_turn"
    StopReasonMaxTokens    StopReason = "max_tokens"
    StopReasonStopSequence StopReason = "stop_sequence"
    StopReasonContentFilter StopReason = "content_filter"  // OpenAI/Gemini-specific; рассматривается как ErrLLMContentPolicy
)

type LLMProviderPort interface {
    ID() LLMProviderID
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
    HealthCheck(ctx context.Context) (*LLMProviderError, error)  // см. §1.2; возвращает typed err для permanent-unhealthy решений
}
```

**Compile-time interface checks (по стилю DP):**
```go
var _ port.LLMProviderPort = (*claudeProvider)(nil)
var _ port.LLMProviderPort = (*openaiProvider)(nil)
var _ port.LLMProviderPort = (*geminiProvider)(nil)
```

**Конструкторы (по DP-конвенции — `NewTypeName`, не `New`):**
```go
func NewClaudeProvider(cfg ClaudeConfig) (*claudeProvider, error)
func NewOpenAIProvider(cfg OpenAIConfig) (*openaiProvider, error)
func NewGeminiProvider(cfg GeminiConfig) (*geminiProvider, error)
func NewProviderRouter(providers map[LLMProviderID]LLMProviderPort, cfg RouterConfig) *ProviderRouter
```

**Корреляционные ID (`correlation_id`, `job_id`, `version_id`, `organization_id`, `created_by_user_id`)** — пробрасываются через `context.Context` (стандартная Go-конвенция; OTel также извлекает их через `trace.SpanContextFromContext`). Адаптеры читают значения из ctx через ключи `internal/observability/contextkeys.go` (см. high-architecture §6.6). Поле `Metadata` из старой версии контракта удалено — `AgentID` остаётся как первоклассное поле `CompletionRequest`, остальное в ctx.

**Использование `PriorTurns` в repair-loop:**

При первом вызове агента `PriorTurns` пустой; передаются только `System` + `User`. При repair (см. §6.8 в high-architecture) — Router добавляет 2 турна: `[{Role:Assistant, Content: invalid_response}, {Role:User, Content: repair_prompt}]`. Адаптер каждого провайдера сериализует это в свой нативный формат:
- Claude: `messages: [{role:"user", content:User}, {role:"assistant", content:invalid_response}, {role:"user", content:repair_prompt}]`
- OpenAI: `input: [{role:"developer", content:System}, {role:"user", content:User}, {role:"assistant", content:invalid_response}, {role:"user", content:repair_prompt}]`
- Gemini: `systemInstruction: {...System}`, `contents: [{role:"user", parts:[User]}, {role:"model", parts:[invalid_response]}, {role:"user", parts:[repair_prompt]}]` (заметьте: Gemini использует `"model"` вместо `"assistant"` — это маппинг адаптера, не утечка в порт).

> Замечание: при repair-loop провайдер **тот же**, что обслуживал исходный вызов (см. OQ-10 / §2.1 «used_provider sticky for repair»). PriorTurns содержат ответ именно этого провайдера, что сохраняет conversation continuity.

### 1.2 Семантика ошибок

`Complete` возвращает либо успех, либо `*LLMProviderError` с двумя ортогональными признаками:

- **`Retryable`** — следует ли retry **на этом же** провайдере (с backoff).
- **`FallbackEligible`** — следует ли пробовать **другого** провайдера из fallback-chain.

Разделение признаков критично: например, истёкший API-ключ Claude (401) — это `Retryable=false` (тот же провайдер не примет следующий запрос), но `FallbackEligible=true` (у OpenAI ключ может быть валиден). Это закрывает аварийный сценарий «истёк ключ primary в проде», который при объединённом признаке привёл бы к mass-fail.

```go
type LLMProviderError struct {
    Code             ErrorCode
    Retryable        bool   // retry на этом же провайдере
    FallbackEligible bool   // try другого провайдера в fallback-chain
    RetryAfter       *time.Duration  // optional, для 429 с Retry-After header
    Wrapped          error
}

type ErrorCode string

const (
    ErrCodeTimeout            ErrorCode = "TIMEOUT"
    ErrCodeRateLimit          ErrorCode = "RATE_LIMIT"
    ErrCodeServerError        ErrorCode = "SERVER_ERROR"
    ErrCodeNetwork            ErrorCode = "NETWORK"
    ErrCodeOverloaded         ErrorCode = "OVERLOADED"          // Anthropic 529
    ErrCodeInvalidAPIKey      ErrorCode = "INVALID_API_KEY"      // 401
    ErrCodeQuotaExceeded      ErrorCode = "QUOTA_EXCEEDED"
    ErrCodeContentPolicy      ErrorCode = "CONTENT_POLICY"
    ErrCodeContextTooLong     ErrorCode = "CONTEXT_TOO_LONG"
    ErrCodeMalformedRequest   ErrorCode = "MALFORMED_REQUEST"
)

// Helpers (для Router-логики):
func (e *LLMProviderError) IsAuthError() bool  // true для ErrCodeInvalidAPIKey
func (e *LLMProviderError) IsAuthError() bool  // используется HealthCheck-обработкой
```

| Code | Retryable | FallbackEligible | Описание / поведение Router'а |
|------|:---------:|:----------------:|--------------------------------|
| `TIMEOUT` | yes | yes | HTTP timeout (> per-request limit) или context cancelled |
| `RATE_LIMIT` | yes | yes | 429 от провайдера; backoff = `RetryAfter` или экспоненциальный |
| `SERVER_ERROR` | yes | yes | 5xx |
| `NETWORK` | yes | yes | TCP reset, DNS error |
| `OVERLOADED` | yes | yes | Anthropic-specific 529 |
| `INVALID_API_KEY` | **no** | **yes** | 401 — primary не примет следующий запрос (ключ невалиден), но fallback-провайдер может справиться. Router помечает primary **permanently unhealthy** до SIGHUP/restart (см. §1.3 HealthCheck) + ALERT |
| `QUOTA_EXCEEDED` | **no** | **yes** | Quota исчерпана; ALERT, primary помечается unhealthy на длительное время; fallback допустим |
| `CONTENT_POLICY` | **no** | **yes** | 400 content_policy_violation — у каждого провайдера своя политика; fallback **может** справиться (другие правила). Если все провайдеры отказали → fail agent с `is_retryable=false` |
| `CONTEXT_TOO_LONG` | **no** | **no** | Input превышает context window. У всех провайдеров аналогичные лимиты; fallback не поможет. Fail agent с `is_retryable=false`; LIC должен был усечь input до вызова (см. ASSUMPTION-LIC-12) |
| `MALFORMED_REQUEST` | **no** | **no** | 400 — баг в коде LIC (неправильно собранный payload). Fallback не поможет. Escalate в логи + DLQ |

**Семантика для Router (см. §2.1):**
- `Retryable=true` → один retry на том же провайдере с backoff (внутри адаптера, перед возвратом err).
- `FallbackEligible=true` → если retry'и исчерпаны, Router переходит к следующему провайдеру.
- `Retryable=false, FallbackEligible=true` → немедленный переход к fallback без retry.
- `Retryable=false, FallbackEligible=false` → немедленный fail.

Реализации провайдеров маппят native errors на `*LLMProviderError` с правильными признаками.

### 1.3 Реализации (контурно)

| Адаптер | Endpoint | Default model | SDK |
|---------|----------|---------------|-----|
| `claudeProvider` | `https://api.anthropic.com/v1/messages` | `claude-sonnet-4-6` | официальный `anthropic-go` SDK или ручной HTTP |
| `openaiProvider` | `https://api.openai.com/v1/responses` | `gpt-4.1` | официальный `openai-go` SDK |
| `geminiProvider` | `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent` | `gemini-2.5-pro` | ручной HTTP (Google Go SDK для GenAI на стадии стабилизации; в v1 — REST) |

> Версии моделей фиксируются в env (`LIC_CLAUDE_MODEL`, `LIC_OPENAI_MODEL`, `LIC_GEMINI_MODEL`). Смена версии — через перезапуск сервиса (rolling). Смена major модели — отдельная задача с регресс-тестированием на эталонных договорах.

### 1.4 Размещение системного промпта

Адаптер каждого провайдера сам трансформирует поле `CompletionRequest.System` в нативный формат:

| Провайдер | Куда адаптер помещает `System` |
|-----------|--------------------------------|
| Claude | Поле `system` в Messages API |
| OpenAI | Первое сообщение `role: developer` (Responses API) |
| Gemini | Поле `systemInstruction` в `generateContent` |

Аналогично, `User` помещается в последнее `role:"user"` сообщение, а `PriorTurns` — в предшествующие turns (с маппингом `Role=Assistant` → `"model"` для Gemini).

### 1.5 JSON-режим и structured outputs

Поле `CompletionRequest.JSONSchema` (`json.RawMessage`) — optional JSON Schema draft-07. Когда задан, адаптер использует **strict structured outputs** провайдера; вероятность invalid JSON output снижается до ~0%.

| Провайдер | Реализация structured outputs |
|-----------|-------------------------------|
| Claude | `tool_use` с virtual tool, у которого `input_schema = JSONSchema`; вынуждает модель вернуть JSON, соответствующий схеме |
| OpenAI | `response_format: {type: "json_schema", strict: true, schema: <JSONSchema>}` |
| Gemini | `responseSchema: <JSONSchema>` в `generationConfig` |

**Поведение по комбинации полей:**

| `JSONSchema` | `JSONMode` | Эффект |
|:------------:|:----------:|--------|
| `nil` | `false` | Свободный ответ (markdown, plain text) — для агентов, не требующих JSON (в LIC v1 не применяется — все агенты возвращают JSON) |
| `nil` | `true` | JSON-only ответ без schema; адаптер использует «json mode» (Claude — без tool_use, OpenAI — `response_format: {type: "json_object"}`, Gemini — `responseMimeType: "application/json"`). Валидация — на стороне LIC Schema Validator + Repair Loop |
| `<schema>` | (ignored) | Strict structured outputs. Если адаптер не поддерживает (legacy model) — он fallback'ает на JSON-only режим, валидация остаётся на стороне LIC |

В v1 все 9 агентов передают `JSONSchema` (схема для конкретного агента, embed-ed в `agents/schemas/*.json` — см. `ai-agents-pipeline.md` §0.2). Это снижает вероятность срабатывания Repair Loop до edge-case, что соответствует ADR-LIC-04 (repair limit N=1).

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

Router имеет два режима:

- **Primary call** (`Complete`) — собирает chain из primary-провайдера агента + остальных из fallback-order, проходит по ней с учётом `Retryable` / `FallbackEligible`.
- **Repair call** (`CompleteRepair`) — sticky: использует **тот же** `used_provider`, который обслужил исходный успешный response (см. OQ-10 / §6.8 в high-architecture). Fallback на другого провайдера в repair **запрещён** — нарушит conversation continuity.

```go
// PrimaryCallResult выдаёт также used_provider — для последующего repair-call.
type PrimaryCallResult struct {
    Response     CompletionResponse
    UsedProvider LLMProviderID
}

func (r *Router) Complete(ctx context.Context, req CompletionRequest) (PrimaryCallResult, error) {
    primary := r.config.AgentPrimary[req.AgentID]
    chain := []LLMProviderID{primary}
    for _, p := range r.config.GlobalFallbackOrder {
        if p != primary {
            chain = append(chain, p)
        }
    }

    var lastErr *LLMProviderError
    for i, providerID := range chain {
        provider := r.providers[providerID]
        if !r.healthy(providerID) {
            metric.Inc("lic_llm_provider_skipped_unhealthy_total{provider=" + string(providerID) + "}")
            continue
        }
        // Rate-limit: блокирующее ожидание токена в пределах ctx deadline.
        // НЕ переход к fallback при denied — это нормальный backpressure.
        if err := r.rateLimit(ctx, providerID); err != nil {
            // ErrLLMRateLimit возвращается только при истечении ctx.
            if errors.Is(err, context.DeadlineExceeded) {
                return PrimaryCallResult{}, &LLMProviderError{
                    Code: ErrCodeRateLimit, Retryable: true, FallbackEligible: true,
                }
            }
            lastErr = err.(*LLMProviderError)
            continue
        }

        resp, err := provider.Complete(ctx, req)
        if err == nil {
            if i > 0 {
                metric.Inc("lic_llm_provider_fallback_total{from=" + string(primary) + ",to=" + string(providerID) + "}")
            }
            return PrimaryCallResult{Response: resp, UsedProvider: providerID}, nil
        }

        pe := err.(*LLMProviderError)
        lastErr = pe
        metric.Inc("lic_llm_provider_failed_total{provider=" + string(providerID) + ",code=" + string(pe.Code) + "}")

        // INVALID_API_KEY — primary помечается permanently unhealthy (см. §1.3 HealthCheck).
        if pe.IsAuthError() {
            r.markPermanentlyUnhealthy(providerID)
            alert.Send("LIC_PROVIDER_AUTH_FAILED", providerID)
        }

        if !pe.FallbackEligible {
            return PrimaryCallResult{}, pe  // например, CONTEXT_TOO_LONG / MALFORMED_REQUEST
        }
        // FallbackEligible=true (включая Retryable=false + FallbackEligible=true:
        // INVALID_API_KEY, QUOTA_EXCEEDED, CONTENT_POLICY) — переход к следующему.
    }
    return PrimaryCallResult{}, &LLMProviderError{
        Code: "ALL_PROVIDERS_FAILED", Retryable: true, FallbackEligible: false, Wrapped: lastErr,
    }
}

// CompleteRepair — same-provider sticky (см. OQ-10).
// usedProvider передаётся вызывающим (Agent.Run() запоминает его после Complete).
// При provider error в repair — НЕ переходим к fallback (нарушение conversation continuity);
// эскалируем AGENT_OUTPUT_INVALID немедленно.
func (r *Router) CompleteRepair(ctx context.Context, req CompletionRequest, usedProvider LLMProviderID) (CompletionResponse, error) {
    provider := r.providers[usedProvider]
    if !r.healthy(usedProvider) {
        // Тот провайдер, что дал успех 5 секунд назад, внезапно unhealthy.
        // Эскалируем без перехода к fallback.
        return CompletionResponse{}, &LLMProviderError{
            Code: ErrCodeServerError, Retryable: false, FallbackEligible: false,
        }
    }
    if err := r.rateLimit(ctx, usedProvider); err != nil {
        return CompletionResponse{}, err
    }
    resp, err := provider.Complete(ctx, req)  // req.PriorTurns содержит invalid response
    if err != nil {
        return CompletionResponse{}, err
    }
    return resp, nil
}
```

**Семантика `rateLimit(ctx, providerID)`** (см. также §3.2): блокирующее ожидание токена в `golang.org/x/time/rate.Limiter.Wait(ctx)` стиле. Возвращает `nil` сразу при доступности токена; при denied — спит до next refill или истечения ctx. `ErrLLMRateLimit` возвращается **только** если ctx истёк до доступности токена. Это означает: при норме backpressure (Claude перегружен пиком) caller дождётся токена в пределах своего deadline, а не сразу прыгнет на fallback (что усилило бы cost на OpenAI).

**Sticky `used_provider` инвариант (по OQ-10):**
- Agent.Run() запоминает `UsedProvider` из `PrimaryCallResult` после успешного `Complete`.
- При schema validation failure → Agent.Run() передаёт его в `CompleteRepair`.
- Repair всегда на том же провайдере — это сохраняет conversation continuity (PriorTurns содержат assistant-ответ именно этого провайдера).

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

Каждый провайдер выставляет `HealthCheck(ctx) (*LLMProviderError, error)`:
- Claude: light request `POST /v1/messages` с минимальным prompt (`max_tokens=10`, sample message). На staging — раз в 60s, на проде — раз в 30s.
- OpenAI: то же на `/v1/responses`.
- Gemini: то же на `:generateContent`.

`HealthCheck` возвращает типизированный `*LLMProviderError` (как `Complete`), что позволяет Router'у различать transient unhealthy и permanent-unhealthy:

| Результат HealthCheck | Поведение Router'а |
|----------------------|---------------------|
| `nil` (success) | Reset `consecutive_failures`; provider healthy |
| `*LLMProviderError{IsAuthError()==true}` (401) | **Permanent unhealthy** — больше не ping'аем до SIGHUP/restart. Это закрывает кейс «истёк ключ»: pinger не тратит quota впустую и не подбрасывает unhealthy/healthy при каждом ping'е |
| `*LLMProviderError{Code: ErrCodeQuotaExceeded}` | **Permanent unhealthy** на 24h (квота обычно обновляется суточно); auto-recovery попытка через 24h |
| `*LLMProviderError{Retryable: true}` (5xx, network, timeout) | Inc `consecutive_failures`; mark transient unhealthy на 60s при `>= 3`. Auto-recovery после успешного healthcheck |
| Иной error | Inc `consecutive_failures`; transient |

Provider Router держит in-memory state: `provider → {healthy: bool, permanent: bool, last_check_at, consecutive_failures}`. `permanent=true` означает, что без оператора восстановления не будет (требуется SIGHUP с обновлёнными секретами или restart). Метрики `lic_llm_provider_health_status{provider, state}` (state ∈ `healthy | unhealthy | permanent`) — для алертов.

`/readyz` — OK, если хотя бы один провайдер healthy. Если все unhealthy (включая permanent) — `/readyz` 503, kube-probe останавливает консьюмер.

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
lic_llm_input_tokens_total{provider, agent}             counter  (billable uncached tokens)
lic_llm_cached_tokens_total{provider, agent}            counter  (tokens served from prompt-cache; 0 для OpenAI/Gemini в v1)
lic_llm_output_tokens_total{provider, agent}            counter
lic_llm_calls_total{provider, agent, outcome}           counter  (outcome ∈ success|repair|fail|fallback)
lic_llm_latency_seconds{provider, agent}                histogram
lic_llm_cost_usd_total{provider, agent}                 counter
```

Cost рассчитывается из встроенной таблицы цен (per-provider, per-model, input/output/cached):

```go
type ModelPricing struct {
    InputPerMTokenUSD       float64  // обычные input tokens
    CachedInputPerMTokenUSD float64  // tokens из prompt-cache (Anthropic: ~10% от обычной цены)
    OutputPerMTokenUSD      float64
}

var pricingTable = map[string]ModelPricing{
    "claude-sonnet-4-6": {InputPerMTokenUSD: 3.00, CachedInputPerMTokenUSD: 0.30, OutputPerMTokenUSD: 15.00},
    "gpt-4.1":           {InputPerMTokenUSD: 2.50, CachedInputPerMTokenUSD: 1.25, OutputPerMTokenUSD: 10.00},
    "gemini-2.5-pro":    {InputPerMTokenUSD: 1.25, CachedInputPerMTokenUSD: 0.0,  OutputPerMTokenUSD: 5.00},
    // ... обновляется через config map
}

// Formula:
//   cost_usd = (resp.InputTokens * Input + resp.CachedInputTokens * CachedInput + resp.OutputTokens * Output) / 1_000_000
```

Без учёта `CachedInputTokens` cost-метрика была бы завышена до **10× на cache-hit запросах** (Anthropic prompt caching у нас включён — см. §5.1). Точное измерение критично для алёрта `LICCostSpike` (см. §4.3).

> Цены в таблице — иллюстративные (на момент проектирования). Поддержка через `LIC_PRICING_TABLE_PATH=/etc/lic/pricing.yaml` (горячая перезагрузка не требуется в v1; при изменении — restart).

### 4.2 Per-organization агрегация

**`organization_id` НЕ публикуется в Prometheus labels** (cardinality blowup при 10К tenants × текущие labels = 15M серий — недопустимо; см. `observability.md` §3.10). Per-tenant cost agregation выполняется через **OpenTelemetry events / span attributes** с экспортом в OTel-совместимое хранилище (Jaeger / Tempo / специальная биллинговая труба).

В LIC v1 separate биллинговая труба не требуется (нет Payment Processing-интеграции). Per-tenant cost-аналитика — на основе OTel span attribute `lic.pipeline.organization_id` + per-span `lic.llm.cost_usd`. Агрегация в downstream-системе (Tempo SQL-запрос или Jaeger API).

Prometheus метрики `lic_llm_cost_usd_total{provider, model, agent}` остаются **без `organization_id`** — для global cost-monitoring и алертов (см. §4.3).

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
- `LLMProviderPort` спроектирован как точка расширения: новый адаптер добавляется без изменений в коде агентов (см. §1.1). Это даёт гибкость при изменении регуляторных требований.

### 7.3 Политики приватности LLM-провайдеров

Каждый провайдер имеет собственную политику обработки и retention передаваемых данных. ContractPro обязан информировать пользователей обо всём согласно 152-ФЗ ст. 9 (см. `security.md` §7.4).

#### Anthropic (primary в v1)

| Аспект | Положение |
|--------|-----------|
| Training | Anthropic **не использует** API-данные для обучения моделей (по Commercial Terms на момент написания: 2026-Q1) |
| Abuse-detection retention | **До 30 календарных дней** Anthropic хранит prompts/responses для безопасности и обнаружения нарушений acceptable-use-policy |
| Юрисдикция хранения | США (data centers Anthropic) |
| Zero-retention option | Доступно через отдельный Enterprise agreement (требует подписания DPA) — **не используется** в v1, рассматривается для premium-tier клиентов в v2 |
| Sub-processors | AWS (US-инфраструктура), используется как cloud provider Anthropic |
| Reference | Anthropic Commercial Terms + Privacy Policy (актуальная версия на сайте anthropic.com) |

#### OpenAI (fallback)

| Аспект | Положение |
|--------|-----------|
| Training | API-данные **по умолчанию не используются** для обучения (`data: don't train` is default для API endpoints) |
| Abuse-detection retention | **До 30 календарных дней** для compliance/safety review |
| Юрисдикция хранения | США |
| Zero-retention option | Через Enterprise / Zero Data Retention agreement |
| Reference | OpenAI API Data Usage Policy + Privacy Policy |

#### Google Gemini (fallback)

| Аспект | Положение |
|--------|-----------|
| Training | Vertex AI / Gemini API endpoints — данные **не используются** для тренировки общих моделей (per Google Cloud terms) |
| Abuse-detection retention | До 24 часов default; до 30 дней при возникновении abuse-flag |
| Юрисдикция хранения | Multi-region (Google Cloud zones) |
| Reference | Google Cloud Terms of Service + Vertex AI Privacy |

> **Operational invariant:** при изменении политики любого провайдера (Anthropic/OpenAI/Gemini может объявить, например, training-by-default) — Legal team обязан обновить PrivacyPolicy.ru ContractPro **до** деплоя такого изменения в production. Это требует мониторинга версии Commercial Terms провайдеров (security-team responsibility).

> **Note для security audit:** retention 30 дней — **не нарушает** заявление «не используется для тренировки». Abuse-detection — отдельная цель обработки, легитимная по 152-ФЗ при наличии информированного согласия пользователя (см. §7.4 в `security.md`).

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

## 9. Self-check

- [x] `LLMProviderPort` — единый контракт, реализуемый Claude / OpenAI / Gemini без изменений в коде агентов.
- [x] Provider Router — per-agent default + global fallback list (ADR-LIC-03).
- [x] Rate limiting — token bucket per provider в Redis + circuit breaker.
- [x] Cost & usage tracking — Prometheus metrics с per-agent / per-provider labels + alert на cost spike.
- [x] Кэширование — только prompt-caching для system messages; НЕ result caching.
- [x] Секреты — env + Lockbox + redaction.
- [x] Data residency — через явное согласие на трансграничную передачу; `LLMProviderPort` оставляет возможность смены провайдера без изменений в коде агентов.
- [x] Health checks — для `/readyz`.
- [x] Тестируемость — mockProvider + nightly contract tests.
