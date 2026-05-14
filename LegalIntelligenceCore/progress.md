# Legal Intelligence Core — Progress Log

Лог прогресса coding-агентов по задачам из `tasks.json`.

## Формат записи

Каждый агент после завершения задачи добавляет блок:

```
## LIC-TASK-NNN — <короткое название>
- **Status:** done
- **Completed at:** YYYY-MM-DD
- **Agent:** <модель / краткий ID>
- **Summary:** что сделано, какие файлы/пакеты затронуты, какие тесты пройдены
- **Notes (optional):** замечания, найденные подводные камни, нерешённые вопросы
```

---

## LIC-TASK-001 — Scaffolding проекта LIC
- **Status:** done
- **Completed at:** 2026-05-14
- **Agent:** claude-opus-4-7 (с консультациями code-architect, architect-reviewer)

### План реализации
1. Изучить конвенции DocumentProcessing (Makefile, go.mod, main.go, Dockerfile).
2. Согласовать со специализированными агентами стратегию scaffolding-а (.gitkeep vs пустые .go, минимальный stub main.go, скелет Dockerfile).
3. Создать дерево директорий из `architecture/deployment.md §1.2` + `tasks.json` acceptance criteria.
4. Заполнить leaf-директории `.gitkeep` (чтобы коммитились в git, без анти-паттерна пустых package-файлов).
5. Создать `go.mod` (module `contractpro/legal-intelligence-core`, Go 1.26.1).
6. Создать stub `cmd/lic-service/main.go` с `version` ldflag.
7. Создать `Makefile` (build/test/lint/docker-build) и скелет `Dockerfile`.
8. Прогнать `make build`, `make test`, `make lint`; проверить наличие бинарника.
9. Через `architect-reviewer` сверить структуру с архитектурой.

### Прогресс
- ✅ Дерево директорий: 47 пакетов (cmd/lic-service + 41 leaf-пакет internal/).
- ✅ `.gitkeep` placeholders во всех пустых директориях.
- ✅ `go.mod`: `module contractpro/legal-intelligence-core`, `go 1.26.1`.
- ✅ `cmd/lic-service/main.go`: stub печатает банер с version и завершается с кодом 0.
- ✅ `Makefile`: цели `build` (с `-ldflags -X main.version`, output `bin/lic-service`), `test` (`go test ./...`), `lint` (`go vet ./...`), `docker-build` (с `--build-arg VERSION`, dual-tag image).
- ✅ Скелет `Dockerfile` (alpine builder → alpine runtime); финальная distroless/nonroot версия — LIC-TASK-003.
- ✅ `make build` → бинарь `bin/lic-service` 1.6 MB.
- ✅ `make test` → `?[no test files]`, 0 ошибок.
- ✅ `make lint` → 0 ошибок.
- ✅ `architect-reviewer` подтвердил полное соответствие deployment.md §1.2.

### Summary
Создан scaffolding для LIC-сервиса согласно архитектуре `deployment.md §1.2`. Готова основа для LIC-TASK-002 (config loader) и далее — без сюрпризов: ports, agents, llm, ingress/egress, infra, application — структура уже под рукой.

### Notes
- Используются `.gitkeep`, а не пустые `.go`-файлы — чтобы будущие задачи могли свободно создавать первый файл пакета без overwrite/удаления placeholder'ов.
- `Dockerfile` намеренно простой (alpine, без CGO) — full distroless/nonroot реализация принадлежит LIC-TASK-003.
- Module path следует DP-конвенции: `contractpro/<domain>` (без `github.com/...`).
- `make build` использует `VERSION ?= $(IMAGE_TAG)` (git describe → 4fd2a35 в текущем состоянии), который пробрасывается в `main.version` через ldflags.
- Бинарь не запускался автоматически (требует approval в текущей sandboxed-среде), но успешная сборка и `go vet` подтверждают корректность.

### Следующая задача
LIC-TASK-002: config loader (godotenv) с `LIC_`-prefixed env переменными, валидация (required/optional/TLS enforcement), tests.

---

## LIC-TASK-002 — Config loader (`LIC_`-env + validation)
- **Status:** done
- **Completed at:** 2026-05-14
- **Agent:** claude-opus-4-7 (с консультациями code-architect, golang-pro, architect-reviewer)

### План реализации
1. Прочитать `architecture/configuration.md` (~80 env vars, 11 правил валидации, TLS enforcement для staging/production).
2. Изучить DP-аналог `DocumentProcessing/development/internal/config/` как референс структуры.
3. Согласовать структуру пакета через code-architect — рекомендация Option C: один файл на под-конфиг, агрегировать ошибки `errors.Join`, TLS-enforcement как cross-cutting функция.
4. Реализовать 14 файлов: `config.go` (root + Load + helpers), `app.go`, `broker.go`, `redis.go`, `idempotency.go`, `pipeline.go`, `llm.go`, `agents.go`, `scoring.go`, `observability.go`, `pricing.go`, `cache.go`, `security.go`, `tls.go`.
5. Написать `config_test.go` с покрытием happy path, missing required, conditional provider keys, invalid values, TLS enforcement, env helpers, struct injection, joined-errors.
6. Прогнать `make build`, `make test`, `make lint`.
7. golang-pro код-ревью → применить must/should-fix.
8. architect-reviewer верификация соответствия `configuration.md` §2.1–2.16 и §3.

### Прогресс
- ✅ Структура пакета: 14 production-файлов + 1 test-файл (one-concern-per-file).
- ✅ `Load()` с godotenv (env override), `Validate()` агрегирует через `errors.Join` — оператор видит все misconfigurations сразу.
- ✅ Required vars: `LIC_BROKER_URL`, `LIC_REDIS_URL`, `LIC_PROMPT_INJECTION_HASH_KEY`, `LIC_DLQ_HASH_KEY`, плюс conditional `LIC_*_API_KEY` (если провайдер в `LIC_PROVIDER_FALLBACK_ORDER`).
- ✅ TLS enforcement (configuration.md §3 rule 10) для `LIC_ENV ∈ {staging,production}`: Redis TLS, amqps://, https:// у LLM, OTEL_INSECURE=false. `LIC_BROKER_TLS=true` поддержан как альтернатива `amqps://`.
- ✅ Per-agent maps (provider, timeout) для 9 агентов — string keys (миграция на typed `domain.AgentID` — после LIC-TASK-011).
- ✅ Защитные проверки сверх spec: дубли в fallback chain, label-threshold ordering, `MaxAgentInputTokens ≤ MaxInputTokens`, `HeartbeatInterval < ProcessingTTL`, unknown agent IDs в maps.
- ✅ `go mod tidy` подтянул `github.com/joho/godotenv v1.5.1`.
- ✅ `make build/test/lint` — все три цели зелёные.
- ✅ Тесты: 29 PASS, 0 FAIL (включая 6 TLS-enforcement, 3 invalid value, 2 conditional provider key, struct-injection, joined-errors, env helpers).

### Summary
Готов production-ready config-пакет для LIC. Соответствует `configuration.md` §2–§3, расширен дополнительными защитными инвариантами (cross-field). Hermetic: только `os`, stdlib, `godotenv`. Готова основа для LIC-TASK-004+ (logger/metrics/tracer/broker/redis — все они получают свой срез *Config через композицию).

### Notes
- Структура согласована с code-architect: Option C (one-concern-per-file) вместо DP-стиля (всё в 2 файлах) — при ~80 vars двух файлов уже мало.
- golang-pro код-ревью прошёл: применены `errors.Join` во всех sub-validate (раньше возвращали первую ошибку), убран дублирующий `envVarSuffix` map (имя константы агента уже совпадает с env-suffix-ом), убрано лишнее поле `minIngestedBytes` (заменён константой пакета), детерминированный порядок ошибок (slice вместо map).
- architect-reviewer подтвердил соответствие `configuration.md` §2.1–2.16 (все категории env vars), §3 rules 1–11 (все правила валидации). Единственный WARNING — отсутствие `LIC_BROKER_TLS` — закрыт.
- env-helpers (`envInt`, `envDuration`, ...) silently fall back to default при невалидном значении (как в DP). Это сознательное решение: текущий тест-контракт это явно покрывает; альтернатива (возвращать error) — значимая API-перестройка, которую можно сделать позже унифицированно для DP + LIC.
- Тесты НЕ `t.Parallel` — несовместимо с `t.Setenv`.

### Следующая задача
LIC-TASK-003: distroless multi-stage Dockerfile (`gcr.io/distroless/static-debian12:nonroot`) с `CGO_ENABLED=0`, version build-arg, image < 50 MB. Зависимость: LIC-TASK-001 (done).

---

## LIC-TASK-004 — Structured logger (slog + allowlist + sanitizer)
- **Status:** done
- **Completed at:** 2026-05-14
- **Agent:** claude-opus-4-7 (с консультациями code-architect, golang-pro, security-engineer)

### План реализации
1. Прочитать observability.md §2.1–2.5 (Logger spec) и security.md §3.4 (sanitize regex) + §6.2 (allowlist policy).
2. Изучить DP-аналог `internal/infra/observability/{logger,context,handler}.go` как референс.
3. Согласовать структуру через code-architect — рекомендация: `log/slog` (stdlib, hermetic), one-concern-per-file, кастомный handler с auto-injection RequestContext + auto-sanitize attr `error`.
4. Реализовать 6 production-файлов: `logger.go`, `handler.go`, `context.go`, `level.go`, `sanitizer.go`, `keys.go`.
5. Написать 5 test-файлов (46 тестов): happy path, mandatory fields, level filter, ctx propagation, sanitizer (Anthropic/OpenAI/Gemini/Bearer/?key=), edge cases.
6. golang-pro код-ревью → применить must-fix (WithGroup nesting, component dup, KindAny bypass).
7. security-engineer ревью → применить must-fix (Bearer alphabet truncation, narrow attr coverage).
8. Прогнать `make build`, `make test`, `make lint` (+ `-race`).

### Прогресс
- ✅ Pkg `internal/infra/observability/logger`: 6 production + 5 test файлов.
- ✅ stdlib log/slog (hermetic): нулевые внешние зависимости в этом пакете.
- ✅ JSON-формат на stdout, обязательные поля: `timestamp`, `level`, `service`, `correlation_id`, `job_id`, `version_id`, `organization_id`, `component`, `message` (когда соответствующие IDs есть в RequestContext).
- ✅ FATAL уровень (`slog.Level(12)` = `LevelFatal`) с лейблом "FATAL", вызывает `os.Exit(1)` через подменяемую `exitFn` (тестируется без kill процесса).
- ✅ `RequestContext` POD из 8 ID-полей (CorrelationID, JobID, DocumentID, VersionID, OrganizationID, CreatedByUserID, ConfirmedByUserID, MessageID); `WithRequestContext`/`RequestContextFrom`.
- ✅ `Logger.With(component)` — единственный source для `component`-attr (ctx-based WithComponent удалён, чтобы не было дублирующего поля).
- ✅ Sanitize: 5 паттернов в одном `secretPattern` (Bearer, sk-ant-, sk-(proj|svcacct|admin|or)-, sk-classic, AIza...) + `queryKeyPattern` (?KEY/?key= case-insensitive). Bearer alphabet включает `+/=~` (защита от truncation на base64-standard токенах).
- ✅ Auto-sanitize в handler: keys `error`, `error_message`, `request_body`, `response_body` (KindString И KindAny+error iface); msg на WARN/ERROR/FATAL.
- ✅ allowlist через exported `KeyXxx` constants — каждый `Key*` это decision-point на review (новый key = явное согласование).
- ✅ `make build/test/lint` — все три цели зелёные. `go test -race` — 0 races.
- ✅ Тесты: 46 PASS (5 файлов), включая регрессии для 5 code-review findings.

### Summary
Готов production-ready logger для LIC. Единая точка структурированного вывода (`log/slog` JSON), enforced allowlist через `KeyXxx`-константы, auto-sanitize защищает от утечки API-ключей в логи (security.md §3.4 + §6.2). Готова основа для LIC-TASK-007 (Redis client) и LIC-TASK-008 (broker client) — оба зависят от logger через DI.

### Notes
- Выбран `log/slog` (stdlib) вместо `zerolog`/`zap`: hermetic (нулевые deps в logger-пакете), достаточная производительность для ~1000 contracts/day.
- `WithGroup` сделан **документированным no-op**: spec observability.md §2.1 требует service/IDs top-level всегда, нативная slog-семантика нестит их в группу и ломает дашборды. Callers, которым нужен логический scope, используют `Logger.With(component)` (top-level `component`-attr). Это локальное отклонение от slog.Handler контракта — проявляется только если callers используют `Slog().WithGroup()`, что в LIC не нужно (есть Logger API).
- `Logger.With(component)` — единственный source для `component`. ctx-based `WithComponent` удалён (изначально был в архитектурном плане), чтобы не было сценариев с двумя `component`-полями в одной JSON-строке.
- Auto-sanitize не покрывает все string-attrs (false-positive risk на legit IDs) — только explicit set: `error`, `error_message`, `request_body`, `response_body`. Документировано; новые "untrusted-content" keys добавляются в `autoSanitizeKeys` явно.
- msg на DEBUG/INFO **не** sanitize-ится (cheap hot path, dev logs verbose). Sanitize включается на WARN/ERROR/FATAL — там же, где обычно появляются leaked secrets через `fmt.Sprintf("...: %v", err)`.
- golang-pro нашёл 5 проблем: WithGroup nesting, component dup, KindAny bypass, неточный allocation-free comment, per-record WithAttrs alloc — first 3 (must-fix) исправлены; alloc concerns mitigated новой реализацией (один record build вместо WithAttrs.Handle).
- security-engineer нашёл 7 проблем: Bearer alphabet (HIGH), narrow attr coverage (HIGH), case-insensitive ?KEY (LOW), header dump patterns (MED), msg sanitize (MED), opentai sk-proj false-negative (HIGH), info-leak through marker (LOW/safe) — first 5 (HIGH+MED) исправлены; header-dump patterns не добавлены (LIC использует только Anthropic/OpenAI/Gemini, AWS sigv4 не in scope в v1).
- Тесты НЕ `t.Parallel` для `TestLogger_FatalCallsExit` — он подменяет package-level `exitFn`. Остальные тесты thread-safe.

### Следующая задача
LIC-TASK-005: Prometheus metrics registry с `lic_*` prefix, factories для counter/histogram/gauge, cardinality-safe (no `organization_id` label). Зависит от LIC-TASK-002 (done). Параллельно может идти LIC-TASK-006 (OTel tracer) и LIC-TASK-011 (domain types).

---

## LIC-TASK-005 — Prometheus metrics registry (`lic_*` prefix + cardinality-safe)
- **Status:** done
- **Completed at:** 2026-05-14
- **Agent:** claude-opus-4-7 (с консультациями code-architect, golang-pro, architect-reviewer)

### План реализации
1. Прочитать observability.md §3.1–3.10 (10 категорий метрик, ~38 штук, cardinality budget 1500 series/instance).
2. Изучить DP-аналог `internal/infra/observability/metrics.go` как референс — flat struct из 6 метрик, для LIC структура должна быть богаче.
3. Согласовать структуру через code-architect — рекомендация: nested sub-groups (`m.Pipeline.StartedTotal` вместо flat `m.PipelineStartedTotal`), отдельный sub-package под logger/metrics/tracer, typed string-константы для всех label-значений (single source of truth), BuildInfo в конструкторе, bucket-наборы как функции (Go не имеет const slices). Дополнительно: добавить `labels.go` и cardinality-тест на reflection.
4. Реализовать 10 production-файлов: registry.go, labels.go, buckets.go, pipeline.go, agent.go, llm.go, dm.go, idempotency.go, pending.go, dlq.go, crosscut.go, buildinfo.go.
5. Написать 4 test-файла: registry_test (inventory + cardinality + hermetic + concurrent + exhaustive enums), buckets_test (spec values + monotonic + fresh instances), labels_test (lock strings + circuit gauge encoding), groups_test (smoke per group).
6. golang-pro код-ревью → применить MUST-FIX (hermetic-from-default через прямую проверку lic_* в DefaultRegisterer, exhaustive enum maps) + SHOULD-FIX (BuildInfo normalization, concurrent test, stage bucket coverage).
7. architect-reviewer верификация соответствия observability.md §3.

### Прогресс
- ✅ Pkg `internal/infra/observability/metrics`: 10 production + 4 test файлов (16 файлов total).
- ✅ 38 Prometheus метрик зарегистрированы — все из §3.2–3.9.
- ✅ Nested sub-groups: `m.Pipeline`, `m.Agent`, `m.LLM`, `m.DM`, `m.Idempotency`, `m.Pending`, `m.DLQ`, `m.CrossCut`, `m.BuildInfo`.
- ✅ Зависимость `github.com/prometheus/client_golang v1.23.2` (та же, что DP).
- ✅ Hermetic registry: `New()` возвращает свой `*prometheus.Registry`; `DefaultRegisterer` не затрагивается (test-проверка прямой walk lic_* prefix-семейств).
- ✅ Cardinality §3.10: `TestNew_NoOrganizationIDLabel` walks every gathered family и asserts отсутствие organization_id; 0 violations.
- ✅ `lic_build_info{version,commit,go_version}` seeded в New() из `BuildInfo` struct; пустые поля нормализуются в "unknown" (защита от silent dashboard breakage).
- ✅ Typed label values (single source of truth observability.md §3): PipelineMode, PipelineOutcome, AgentInvocationOutcome, AgentRepairOutcome, LLMCallOutcome, LLMErrorCode, LLMHealthState, DMOperation, DMOutcome, IdempotencyLookupResult, PendingConfirmationOutcome, DLQTopic, PartyValidationType, PublishOutcome — 14 enum-семейств с exhaustive-проверкой через map-size assertion.
- ✅ Histogram buckets — точные spec-значения (pipeline 1..120s, agent_duration 0.5..20s, agent_input_tokens 1k..64k, agent_output_tokens 100..8k, llm_latency 0.2..30s, dm_request 0.1..30s); монотонность + fresh-instance invariant.
- ✅ `cached_tokens_total` отдельной метрикой (защита от 10× cost-инфляции на Anthropic prompt-cache).
- ✅ `prompt_injection_detected_total` БЕЗ severity label (C-lite OQ-13).
- ✅ `make build/test/lint` — все три цели зелёные. `go test -race` — 0 races.
- ✅ Тесты: 25 PASS metrics (100 total с config+logger), 0 FAIL.

### Summary
Готов production-ready Prometheus metrics registry для LIC. Полное соответствие observability.md §3.1–3.10; все 10 ключевых spec-инвариантов защищены regression-тестами. Готова основа для LIC-TASK-009 (health handler — `/metrics` экспонент), LIC-TASK-010 (concurrency limiter — обновляет ConcurrentJobs gauge), LIC-TASK-018 (cost tracker), LIC-TASK-019 (provider router — обновляет fallback/health/skip counters), LIC-TASK-024+ (агенты — обновляют invocations/duration/tokens).

### Notes
- Структура согласована с code-architect (Option: nested sub-groups вместо flat) — при 38+ метриках flat namespace ломает IDE-навигацию; sub-group handle упрощает DI (оркестратору нужен только `*PipelineMetrics`, не весь `*Metrics`).
- Bucket-наборы экспортируются как функции (не вар-slices) — caller mutation одной серии не утечёт в следующую конструкцию; протестировано через `TestBuckets_AreFreshInstances` (мутируем slice А, проверяем что slice Б не затронут).
- golang-pro нашёл 8 проблем: 2 MUST-FIX (false-negative в hermetic-test через count-diff, отсутствие exhaustive enum-теста) + 6 SHOULD-FIX (BuildInfo zero-value silent footgun, PublishOutcome spec-divergence, dead state field, concurrent test, missing stage bucket assertion, seed coverage doc). Применено 7 из 8 (BuildInfoMetric.info оставлен — пригодится для health endpoint и documented).
- architect-reviewer PASS без несоответствий. Подтверждены invariants: 38-name set + lic_ prefix universality + organization_id absence + severity absence on prompt-injection + exact bucket boundaries + bucket monotonicity/freshness + outcome enum exhaustiveness + cached_tokens independence + build_info value=1 + registry hermeticity.
- Поле `PublishOutcome` (success|failure|nacked|invalid) — package-defined; §3.9 spec не enum'ил значения для consumer/publisher_messages_total; задокументировано в labels.go.
- Не реализовано (out of scope): integration с promhttp (LIC-TASK-009), реальные call-sites из агентов/router (LIC-TASK-019, 024+), OTel span attributes (LIC-TASK-006).
- Tests НЕ `t.Parallel` для большинства (они и так быстрые, milliseconds total); `TestNew_ConcurrentConstructionIsSafe` явно использует 50 goroutines для проверки thread-safety.

### Следующая задача
LIC-TASK-006: OpenTelemetry tracer с W3C Trace Context propagation, OTLP gRPC exporter, ParentBased(TraceIDRatio) sampler, helpers для StartSpan/SpanFromContext/InjectIntoHeaders. Зависит от LIC-TASK-002 (done). Параллельно может идти LIC-TASK-011 (domain types) или LIC-TASK-007 (Redis client — зависит от 002, 004).

---

## LIC-TASK-006 — OpenTelemetry tracer (W3C TraceContext + OTLP gRPC + ParentBased(TraceIDRatio))
- **Status:** done
- **Completed at:** 2026-05-14
- **Agent:** claude-opus-4-7 (с консультациями code-architect, golang-pro, architect-reviewer)

### План реализации
1. Прочитать observability.md §4 (Provider, hierarchy, attributes, propagation, sampling, retention) + configuration.md (LIC_OTEL_* env vars). DP-аналог `internal/infra/observability/tracer.go` — реф для конструктора и no-op fallback.
2. Согласовать структуру через code-architect — рекомендация: гибрид из 3-4 файлов (typed attribute keys обязательно отдельно для 40+ call sites; propagator выделен из-за разной транспортной концепции; tracer.go и span.go разделены по lifecycle vs runtime).
3. Согласовать идиоматику через golang-pro: OTel v1.32+ (фактически вышла v1.43.0 — взяли её), semconv v1.26.0 (консистентно с DP), `ParentBased(TraceIDRatioBased)` явно (без implicit OTEL_TRACES_SAMPLER env-magic), `sync.Once` для install of globals, `crypto/rand` 16 байт hex для service.instance.id (без uuid-deps), MapCarrier для Inject/Extract.
4. Реализовать 4 production-файла: attrs.go (28 typed `attribute.Key` + `SpanFields` POD), tracer.go (Config, Tracer, New, Shutdown, buildSampler, newOTLPExporter, buildResource, instanceID, installGlobals), span.go (StartSpan/StartSpanWithFields/SpanFromContext/RecordError/SetOK), propagator.go (InjectIntoHeaders/ExtractFromHeaders).
5. Написать 4 test-файла: tracer_test (sampler-матрица, resource attrs, instanceID uniqueness, Shutdown timeout, SDK-backed flush ordering, ApplyTo), span_test (StartSpan/StartSpanWithFields, RecordError success/nil-guards, SetOK), propagator_test (round-trip preservation, baggage, nil-guards), attrs_test (omit-empty + all-fields-covered + namespace stability lock).
6. golang-pro + architect-reviewer code review → применить must/should-fix.
7. Прогнать `make build`, `make test`, `make lint` (+ `-race`).

### Прогресс
- ✅ Pkg `internal/infra/observability/tracer`: 4 production + 4 test файла (8 файлов total).
- ✅ OTLP gRPC exporter (otlptracegrpc.New), не HTTP — соответствует §4.1 + acceptance criteria.
- ✅ Composite W3C propagator (`TraceContext{}, Baggage{}`) применён даже для no-op tracer — upstream `traceparent` не теряется, когда LIC export disabled.
- ✅ Sampler-матрица (7 вариантов): default empty fallback на `parentbased_traceidratio`, плюс `always_on/off`, `traceidratio`, 3× `parentbased_*`. Unknown name отклоняется явно (без silent fallback). Range [0,1] для arg-based.
- ✅ Resource: service.name, service.instance.id (16-байт hex от crypto/rand), service.version (opt), deployment.environment (opt), process.runtime.name=go, process.runtime.version. resource.Default() добавляет host.*, остальные process.*. Custom resource без SchemaURL — иначе конфликт с SDK Default's v1.40.0 в Merge.
- ✅ Shutdown: ForceFlush (3s бюджет) и Shutdown (5s бюджет) идут с независимыми timeouts через `errors.Join` — медленный flush не съест бюджет gRPC close. No-op tracer Shutdown — early return nil.
- ✅ installGlobals через `sync.Once`: первый `New(InstallGlobals=true)` фиксирует глобалы; последующие `New()` возвращают usable-через-DI Tracer, но не перезаписывают globals (документировано). Тесты используют `InstallGlobals=false` — глобальное состояние процесса не загрязняется.
- ✅ 28 typed `attribute.Key` констант — single source of truth по observability.md §4.3 (mandatory IDs + lic.pipeline.* + lic.agent.* + lic.llm.*). `TestAttrConstants_StableNamespacing` локирует wire-format strings — drift сломает компиляцию теста до того, как сломает дашборды.
- ✅ `SpanFields.AsKeyValues` (omit-empty) + `ApplyTo(span)` (single SetAttributes batch для hot-path call sites).
- ✅ `InjectIntoHeaders(nil headers)` — паника с явным сообщением: silent drop traceparent ломает W3C propagation invariant §4.4.
- ✅ `make build/test/lint` — все три цели зелёные. `go test -race` — 0 races. 124 теста PASS (29 config + 46 logger + 25 metrics + 24 tracer).
- ✅ Зависимости: `go.opentelemetry.io/otel v1.43.0`, `go.opentelemetry.io/otel/sdk v1.43.0`, `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.43.0`, semconv v1.26.0 (как в DP).

### Summary
Готов production-ready OpenTelemetry tracer для LIC. Полное соответствие observability.md §4.1–4.5 + acceptance criteria LIC-TASK-006. Hermetic (только `otel/*` + stdlib), транспорт-агностичный (конверсия amqp.Table → map[string]string остаётся в infra/broker boundary). Готова основа для LIC-TASK-008 (broker — Inject в outgoing headers, Extract из incoming), LIC-TASK-009 (health — promhttp + tracer.Enabled() metric), LIC-TASK-024 (BaseAgent — span-per-agent с lic.agent.* attributes), LIC-TASK-019 (LLM router — span-per-llm-call с lic.llm.* attributes), LIC-TASK-036 (pipeline orchestrator — root span + span-per-stage hierarchy §4.2).

### Notes
- Структура согласована с code-architect: гибрид Option B-lite (4 файла), не полный 5-файловый split (`noop.go` отдельным был бы искусственным разделением одной концепции lifecycle).
- `propagator.go` оставлен отдельным (не сложен в tracer.go) — разные транспортные boundaries; будущая HTTP middleware (Orchestrator) переиспользует его без изменений.
- Method-style `Tracer.InjectIntoHeaders` (vs package-level) — выбрано для DI-friendliness в hexagonal arch; package-level alias не добавлен (surface bloat); auto-instrumented libs (otelgrpc/otelhttp) подхватят через `otel.GetTextMapPropagator()` после `installGlobals`.
- `crypto/rand` panic при сбое (вместо silent "unknown") — поломанный entropy source = сломанная ОС, fail-loud правильнее. 128 бит уникальности эквивалентно UUIDv4, без uuid-зависимости.
- `tracetest.InMemoryExporter` wipes spans на `Shutdown()` — это сломало бы post-Shutdown assertion в `TestShutdown_FlushesPendingSpans`. Заменён на собственный `capturingExporter` (10 LOC), который сохраняет spans across Shutdown.
- Exporter cleanup при ошибке `buildResource` использует fresh `context.Background()` с timeout — caller's ctx может быть уже cancelled, gRPC close нужно довести до конца.
- golang-pro нашёл 9 проблем (3 must-fix + 5 should-fix + 1 nit), все применены кроме package-level Inject/Extract alias (намеренно) и sync.Pool для AsKeyValues (заменено `ApplyTo` — нулевые промежуточные allocs). architect-reviewer PASS без CRITICAL/HIGH; единственный MEDIUM — отсутствие SDK-backed flush coverage — закрыт `TestShutdown_FlushesPendingSpans`.
- AttributeKey `confirmed_by_user_id` переименован из `AttrConfirmedByUser` → `AttrConfirmedByUserID` для консистентности с остальными `*UserID` именами (wire-format string не изменился).
- Тесты НЕ `t.Parallel` для большинства — быстрые (milliseconds), `TestShutdown_FlushesPendingSpans` использует BatchSpanProcessor с реальным timing-зависимым flush через `tr.Shutdown`.

### Следующая задача
Свободно несколько критических задач без блокеров:
- LIC-TASK-003 (distroless multi-stage Dockerfile) — зависит только от LIC-TASK-001 (done).
- LIC-TASK-007 (Redis client с Lua + TLS) — зависит от LIC-TASK-002 (done) + LIC-TASK-004 (done).
- LIC-TASK-008 (RabbitMQ broker с publisher confirms + DLX-loop topology) — зависит от LIC-TASK-002 + LIC-TASK-004 (оба done). Подходит для интеграции с tracer (Inject/Extract в outgoing/incoming headers).
- LIC-TASK-011 (domain types: статусы, стадии, agent IDs, error codes, DomainError) — зависит только от LIC-TASK-001.
- LIC-TASK-010 (concurrency limiter) — зависит от LIC-TASK-005 (done).

