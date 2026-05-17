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

---

## LIC-TASK-011 — Domain types (статусы, стадии, agent IDs, error codes, DomainError)
- **Status:** done
- **Completed at:** 2026-05-14
- **Agent:** claude-opus-4-7 (с консультациями code-architect, golang-pro, architect-reviewer)

### План реализации
1. Прочитать high-architecture.md §2.1.3 (3 external status + 18 STAGE_*), error-handling.md §2.1 + §3 (DomainError + каталог 20 кодов RU/EN), ai-agents-pipeline.md §1 (12-value contract-type whitelist + 9 agents).
2. Согласовать структуру через code-architect — рекомендации: типизированные `type X string` для всех enum (ErrorCode/Stage/AgentID/ContractType), статический errorCatalog вместо 20 per-code конструкторов, init() panic-on-mismatch как single-source-of-truth invariant, AllX() helpers возвращают свежий slice, экспортированный ErrorSpec.
3. Реализовать 5 production-файлов: status.go (ExternalStatus + Stage), agent.go (AgentID), contract_type.go (ContractType + regex), errors.go (DomainError + fluent builders), error_codes.go (20-кодовый каталог + IsPublishableToOrchestrator + LookupErrorSpec).
4. Написать 5 test-файлов: status_test, agent_test, contract_type_test, errors_test, error_codes_test — 53 теста, включая wire-format locks, retryable flag locks vs error-handling.md §3, exhaustive catalog completeness via init().
5. Прогнать `make build`, `make test`, `make lint` (+ `-race`).
6. golang-pro + architect-reviewer code review → применить must/should-fix.

### Прогресс
- ✅ Pkg `internal/domain/model`: 5 production + 5 test файлов (10 файлов total).
- ✅ Hermetic: только stdlib (errors, fmt, regexp, unicode) — никаких infra/agents/llm импортов, чистый inner ring hexagonal arch.
- ✅ status.go: ExternalStatus (3 значения: IN_PROGRESS/COMPLETED/FAILED — без QUEUED/COMPLETED_WITH_WARNINGS/TIMED_OUT/REJECTED) + Stage (18 STAGE_*) + AllStages/AllExternalStatuses (fresh slices) + IsValid (init-backed lookup).
- ✅ agent.go: AgentID (typed string) с 9 константами в pipeline-order + AllAgentIDs + IsValid.
- ✅ contract_type.go: ContractType с 12 whitelist константами + `contractTypeFormatRE = regexp.MustCompile("^[A-Z_]{1,32}$")` + ValidateContractTypeFormat + IsValidContractType (комбинированный gate).
- ✅ errors.go: DomainError{Code, UserMessage, DevMessage, Retryable, Stage, Cause, Attributes}; Error()/Unwrap()/errors.As support; fluent builder API NewDomainError(code, stage) → WithCause → WithDevMessage → WithUserMessage → WithRetryable → WithAttributes → WithAttribute; все builders mutate-in-place + nil-receiver safe + chainable; helpers IsDomainError/IsRetryable/GetErrorCode/AsDomainError.
- ✅ error_codes.go: ErrorCode (typed string) с 20 константами в 7 секциях; статический errorCatalog{retryable, userMessage RU, devMessage EN}; init() panics на любой mismatch между AllErrorCodes() и errorCatalog (single-source-of-truth invariant); экспортированный ErrorSpec + LookupErrorSpec возвращает (ErrorSpec, bool); ErrorCode.IsPublishableToOrchestrator() — структурный guard от leak пустого UserMessage в status-changed (DLQ-only коды защищены).
- ✅ `make build/test/lint` — все три цели зелёные. `go test -race` — 0 races.
- ✅ Тесты: 188 всего PASS по модулю (29 config + 53 domain/model + 46 logger + 25 metrics + 24 tracer + 11 пакетных smoke), 0 FAIL.

### Summary
Готов production-ready domain types-пакет для LIC. Полное соответствие error-handling.md §3 + high-architecture.md §2.1.3 + ai-agents-pipeline.md §1. Hermetic, zero внешних deps. Готова основа для LIC-TASK-012 (PipelineState, AgentInput, артефакты), LIC-TASK-013 (ports — все hexagonal interfaces), LIC-TASK-014–016 (LLM provider adapters), LIC-TASK-020–033 (агенты), LIC-TASK-036/037 (orchestrator + pause-resume), LIC-TASK-038–046 (ingress/egress).

### Notes
- Структура согласована с code-architect: typed string aliases для всех 4 enum (ErrorCode/Stage/AgentID/ContractType) — compile-time safety на switch/map keys; статический catalog вместо per-code constructors (20 кодов × 5 строк = 100 LOC дубликата vs 60-строчный map); init() panic — startup-time guarantee, не runtime surprise.
- golang-pro code review нашёл 2 HIGH + 4 MEDIUM + 5 LOW; применены все HIGH/MEDIUM:
  - (H1) WithAttribute контракт уточнён: все builders мутируют receiver (а не возвращают копию); документировано что *DomainError owned-by-single-goroutine; "safe across goroutines" из исходного docstring удалён.
  - (H2) добавлен ErrorCode.IsPublishableToOrchestrator() — структурный guard против leak пустого UserMessage в lic.events.status-changed (DLQ-only коды — INVALID_MESSAGE_SCHEMA / INVALID_ORG_ID_MISMATCH / IDEMPOTENCY_STORE_UNAVAILABLE — возвращают false; неизвестные коды тоже false — safe default).
  - (M1+M2) переход на fluent builder API: NewDomainError(code, stage) + chained WithCause/WithDevMessage/WithUserMessage/WithRetryable/WithAttributes/WithAttribute — заменил неудобный 5-арг конструктор с тремя default-value позиционными аргами; одновременно решён MEDIUM от architect-reviewer (DM_PERSIST_FAILED non-retryable RU variant override через WithRetryable(false) + WithUserMessage("Документ был удалён или недоступен.")).
  - (M3) Cyrillic regex replaced на unicode.Is(unicode.Cyrillic, r) — корректно покрывает Cyrillic Supplement (U+0480..U+04FF) и Extended-A блоки.
  - (M4) ErrorSpec экспортирован, LookupErrorSpec(ErrorCode) (ErrorSpec, bool) — struct return вместо 4-tuple, исключает positional-argument bug.
- architect-reviewer PASS (no HIGH violations): подтвердил соответствие spec для всех 18 STAGE_* (точные wire-format strings), 3 ExternalStatus (только IN_PROGRESS/COMPLETED/FAILED), 9 AgentID, 12 ContractType (точно по ai-agents-pipeline.md §1), 20 ErrorCode (все 7 секций §3.1–§3.7 покрыты), Retryable flags соответствуют spec table; regex ^[A-Z_]{1,32}$ соответствует wire-format constraint из integration-contracts/event-catalog.
- LOW issues задокументированы, не применены: (L1) nil-receiver Error() возвращает "<nil>" — намеренно defensive (callers могут строить error в deferred path); (L2) init() lookup-table pattern концурентно-безопасен по Go spec — нет блокировки нужно; (L3) панику init() тестировать через build constraint не стали — слишком инфраструктурно; добавлен TestErrorCatalog_WireStringsAreUnique как regression для типов tippa.
- Спорная точка spec'а: error-handling.md §3 использует строку "STAGE_RECEIVE" для inbound errors, но high-architecture.md §2.1.3 (авторитет) использует "STAGE_RECEIVED". Реализация следует high-arch — конкретный комментарий не добавлен (это spec-internal inconsistency не код-level issue).
- Tests НЕ `t.Parallel` — все быстрые (milliseconds total), reflect-based catalog validation и regex-compiled MustCompile вычислены однажды на init.

### Следующая задача
Разблокированы все следующие critical-задачи pipeline:
- LIC-TASK-012 (PipelineState + AgentInput + типизированные artifacts + PendingTypeConfirmation) — зависит от 011 (done). Logical next: данные, которые движутся через pipeline.
- LIC-TASK-013 (Hexagonal ports — все internal interfaces) — зависит от 012. Сразу после 012.
- LIC-TASK-020 (embed prompts/schemas через embed.FS) — зависит от 011 (done). Параллельно с 012.
- LIC-TASK-021 (Token Estimator) — зависит от 011 (done).

Также по-прежнему свободны без новых блокеров: LIC-TASK-003 (Dockerfile distroless), LIC-TASK-007 (Redis), LIC-TASK-008 (RabbitMQ), LIC-TASK-010 (concurrency limiter).

---

## LIC-TASK-012 — Domain models (PipelineState, AgentInput, типизированные artifacts, PendingTypeConfirmation)
- **Status:** done
- **Completed at:** 2026-05-14
- **Agent:** claude-opus-4-7 (с консультациями code-architect, golang-pro, architect-reviewer)

### План реализации
1. Прочитать high-arch §2.1 (entities), §6.10 (Pending state — gzip+base64, 25h TTL, поля), §6.11 (RiskProfile/AggregateScore + 22-value расширенный RiskCategory + R-NNN/R-PNNN/R-MNNN namespaces), ai-agents-pipeline.md §1-9 (9 JSON schemas артефактов) + §8 (warnings object-map), integration-contracts.md §4 (envelope: correlation_id + job_id + document_id + version_id + organization_id + created_by_user_id + opaque origin_type), event-catalog.md §2.1 (LIC-side ужесточения regex).
2. Согласовать структуру через code-architect — рекомендации: (Q1) разбить artifacts.go (~600 LOC) на 8 концептуальных файлов под one-concern-per-file; (Q2) DM-outbound counterpart types отложить до LIC-TASK-035 (scope разделения); (Q3) prompt_injection_detected — простой bool на каждом артефакте; (Q4) TraceContext — именованной struct {TraceParent, TraceState} с json lowercase per W3C; (Q5) InputArtifactsCompact — typed map[ArtifactType]json.RawMessage с deferred decoding (byte-faithful copy DM payload); (Q6) Warnings — named-field struct с 5 *Warning указателями + omitempty (named fields IDE-friendly, JSON shape соответствует spec); (Q7) PipelineState — плоская struct (13 указателей agent outputs).
3. Реализовать 14 production-файлов: trace.go, derived.go, classification.go, key_parameters.go, party_consistency.go, mandatory_conditions.go, risk_analysis.go, recommendations.go, report.go, warnings.go, delta.go, agent_input.go, pipeline_state.go, pending.go.
4. Реализовать 11 test-файлов с golden round-trip JSON + edge-cases (nil receiver, invalid base64/gzip/json для pending, nullable vs omittable wire-семантика).
5. Прогнать `make build/test/lint -race`.
6. Code review через golang-pro + architect-reviewer (параллельно) — применить MUST/SHOULD-FIX.

### Прогресс
- ✅ Pkg `internal/domain/model`: 14 production-файлов + 11 test-файлов (расширил LIC-TASK-011's 5+5).
- ✅ Hermetic: только stdlib (bytes, compress/gzip, encoding/base64, encoding/json, fmt, io, regexp, time).
- ✅ **PipelineState** — плоская struct с 13 указателями agent-результатов, идентифицирующими полями (correlation_id/job_id/document_id/version_id/organization_id/created_by_user_id), Mode (PipelineMode INITIAL/RE_CHECK), OriginType (opaque DM-enum), ParentVersionID (*string), CurrentStage (Stage), StartedAt (time.Time UTC), TraceContext (W3C), InputArtifacts (deferred-decoded), RiskProfile + AggregateScore. Helper `NewPipelineState(correlationID, jobID, documentID, versionID, organizationID)` со Stage=STAGE_RECEIVED, Mode=INITIAL, StartedAt=now UTC.
- ✅ **AgentInput** — POD-контейнер: correlation IDs + Artifacts (InputArtifactsCompact) + 8 agent-result указателей + Recommendations slice + ParentRiskAnalysis (для Agent 9 в RE_CHECK).
- ✅ **TraceContext** — W3C 2-field struct (TraceParent + TraceState с omitempty), `IsZero()` helper.
- ✅ **InputArtifactsCompact** = `map[ArtifactType]json.RawMessage` + Has(t) helper; ArtifactType enum с 5 константами (SEMANTIC_TREE/EXTRACTED_TEXT/DOCUMENT_STRUCTURE/PROCESSING_WARNINGS/RISK_ANALYSIS).
- ✅ **RiskLevel** (high/medium/low) + **RiskProfile** (OverallLevel + 3 counts) + **AggregateScore** (Score + Label) + **AggregateScoreLabel** (low/medium/high).
- ✅ **9 артефакт-типов** с FROZEN-DM-conformant JSON tags по ai-agents-pipeline.md §1-9: ClassificationResult, KeyParameters (+InternalExtras +PartyRole +KeyDate), PartyConsistencyFindings (7 PARTY_* findings), MandatoryConditionsReport (^MC_[A-Z0-9_]+$ regex), RiskAnalysis (22-value RiskCategory + ^R-(P|M)?[0-9]{3,}$ regex), Recommendations, Summary, DetailedReport (7 секций), RiskDelta.
- ✅ **RiskCategory 22-value enum** (13 от агента 5 + 7 PARTY_* + 2 MANDATORY_*) с exhaustive IsValid через init-built map + AllRiskCategories() возвращает fresh slice.
- ✅ **Warnings wrapper** с 5 типизированными *Warning указателями (PROMPT_INJECTION_DETECTED/RE_CHECK_PARENT_ANALYSIS_MISSING/INPUT_TRUNCATED/CLASSIFICATION_PARAMS_MISMATCH/RECOMMENDATION_ORPHAN_REF), omitempty → object-map wire shape, IsEmpty() helper.
- ✅ **PendingTypeConfirmation** — 12 полей (10 spec из §6.10 + 2 LIC-internal OriginType/ParentVersionID для resume-completeness); Encode() (JSON → gzip → base64) + DecodePendingTypeConfirmation() с гарантированным byte-for-byte round-trip; edge cases (nil receiver, invalid base64/gzip/json) покрыты.
- ✅ **Nullable wire-fields** — корректно сериализуются как `null` (не omit) для всех `type:[T,null]` schema полей: PartyRole INN/OGRN/Address/Signatory/SignatoryAuthority/ClauseRef, PartyFinding PartyName, MandatoryCondition FoundIn/IssueDescription, ReportItem Severity/ClauseRef/LegalBasis/LinkedRiskID/LinkedRecommendation, RiskChange OldClauseRef/NewClauseRef.
- ✅ `make build/test/lint` — все три цели зелёные. `go test -race` — 0 races.
- ✅ Тесты: 100 PASS в domain/model (53 от LIC-TASK-011 + 45 new), 0 FAIL.
- ✅ Общий пакет LIC: 5 packages PASS, ~230 тестов total.

### Summary
Готов production-ready domain pipeline-models пакет для LIC. Полное соответствие ai-agents-pipeline.md §1-9 + high-architecture.md §2.1/§6.10/§6.11 + integration-contracts.md §4. Hermetic, zero внешних deps. Готова основа для LIC-TASK-013 (Hexagonal ports — все ports принимают/возвращают эти типы), LIC-TASK-020 (embed prompts/schemas — схемы валидируют LLM outputs против этих типов), LIC-TASK-024 (BaseAgent runner — Run() возвращает AgentResult), LIC-TASK-035 (Result Aggregator — единственная точка stripping internal fields + расчёта RiskProfile/AggregateScore + сборки 22-value risks[]), LIC-TASK-037 (Pause-and-Resume — Encode/Decode PendingTypeConfirmation в/из Redis).

### Notes
- Структура согласована с code-architect: разбит artifacts.go (~600 LOC) на 8 концептуальных файлов под one-concern-per-file (продолжает paradigm LIC-TASK-011); DM-outbound counterpart types сознательно отложены до LIC-TASK-035 (scope LIC-TASK-035 = единственная точка stripping rationale/internal_extras/prompt_injection_detected перед публикацией в DM); InputArtifactsCompact как map[ArtifactType]json.RawMessage сохраняет byte-faithful копию payload от DM (никаких re-encode на pause/resume); Warnings как named-field struct (IDE autocomplete в Aggregator, no `any`/type-assertion, golden-test diffs читаемы).
- golang-pro code review: 1 MUST-FIX/верификация + 6 SHOULD-FIX/NIT. Применено:
  - (MUST-1) Nullable wire-fields per ai-agents-pipeline.md §2/§3/§4/§8/§9 — убран ошибочный `omitempty` с PartyRole INN/OGRN/Address/Signatory/SignatoryAuthority/ClauseRef, PartyFinding PartyName, MandatoryCondition FoundIn/IssueDescription, ReportItem Severity/ClauseRef/LegalBasis/LinkedRiskID/LinkedRecommendation, RiskChange OldClauseRef/NewClauseRef — все эти поля schema объявляет `type:[T,null]` (nullable), значит должны serialize as null when unset, не быть omitted. Добавлены явные регрессионные тесты `TestPartyRole_NullableFieldsSerialiseAsNull` и `TestMandatoryCondition_NullableFieldsSerialiseAsNull`.
  - (S-3) Risk.Rationale comment уточнён с ссылкой на LIC-TASK-035 как single stripping site.
  - (S-5) Pending.go: добавлен комментарий про base64.StdEncoding.EncodedLen exactness (защищает от well-meaning future "fix" на slicing).
  - (M-2) Pending.go: документирована TraceContext value-vs-pointer семантика (TraceContext.IsZero() — канонический "no trace" тест).
  - LOW-findings (init() ordering, JSON tags на PipelineState dead annotations, AgentInput-vs-PipelineState near-duplicate) — оставлены как acceptable для v1; перепроектирование AgentInput через projection-метод от PipelineState — задача LIC-TASK-013+.
- architect-reviewer PASS APPROVED без HIGH/MEDIUM: подтверждены все 11 верификационных пунктов — 18 STAGE_* (из LIC-TASK-011), 3 ExternalStatus (из 011), PipelineMode INITIAL/RE_CHECK с default INITIAL, 22 RiskCategory values (13+7+2 names матчат §6.11.2 verbatim), ^R-(P|M)?[0-9]{3,}$ regex в IsValidRiskID, ^MC_[A-Z0-9_]+$ в IsValidMandatoryConditionCode, PendingTypeConfirmation с 10 spec-полями + 2 LIC-internal для resume-completeness, 9 agent артефактов с FROZEN-DM-conformant tags, KeyParameters Price/Duration/Penalties/Jurisdiction *string без omitempty (serialize null), 7 PARTY_* finding types, 7 ReportSection codes в фиксированном порядке, Warnings object-map с точными field names per spec (detected/detected_by_agents/detection_count/user_message; truncated_bytes/total_bytes; orphan_risk_ids), TraceContext W3C 2-field lowercase, InputArtifactsCompact с 5 ArtifactType ключами. LOW findings задокументированы.
- Тесты НЕ `t.Parallel` — все быстрые (milliseconds total), reflect.DeepEqual + regex MustCompile вычислены однажды на init.
- Зависимостей в go.mod не добавлено — пакет полностью hermetic.

### Следующая задача
Разблокированы все critical-задачи следующего слоя (ports + Aggregator + agents):
- LIC-TASK-013 (Hexagonal ports — все internal interfaces, использующие model.* типы) — зависит от 012 (done). Logical next.
- LIC-TASK-020 (embed prompts/schemas) — зависит только от 011 (done). Параллельно.
- LIC-TASK-021 (Token Estimator) — зависит только от 011 (done). Параллельно.

Также по-прежнему свободны без новых блокеров: LIC-TASK-003 (Dockerfile distroless), LIC-TASK-007 (Redis), LIC-TASK-008 (RabbitMQ), LIC-TASK-010 (concurrency limiter).

---

## LIC-TASK-013 — Hexagonal ports (все internal interfaces)
- **Status:** done
- **Completed at:** 2026-05-15
- **Agent:** claude-opus-4-7 (с консультациями code-architect, golang-pro, architect-reviewer)

### План реализации
1. Прочитать llm-provider-abstraction.md §1-2 (LLMProviderPort + Router + LLMProviderError taxonomy), high-architecture.md §6 (компоненты), event-catalog.md §1-3 (LIC events + DLQ), integration-contracts.md §1-2 (subscriptions/publications), DM event-catalog.md §1.4-1.5, §2.1-2.2 (FROZEN inbound/outbound контракты), error-handling.md §3 (error codes).
2. Согласовать структуру через code-architect — рекомендации Q1-Q8 одобрены: (Q1) wire-DTO в port-пакете (а не infra/wire или model — они часть контракта portов); (Q2) симметрично для outbound; (Q3) LLM-types разнесены на llm.go + llm_errors.go (concept boundary); (Q4) ErrorCode→LLMErrorCode (исключение name conflict с model.ErrorCode); (Q5) DMResponseAwaiter → 2 порта per ISP (ArtifactsAwaiter + PersistConfirmationAwaiter — разные lookup keys, TTLs, failure modes); (Q6) per-file compile-time tests `var _ Port = (*fake)(nil)`; (Q7) AgentResult = any (без marker interface, чтобы избежать touch к 9 типам в model/; stage executor диспатчит по AgentID); (Q8) переименовать ArtifactPersistencePort → AnalysisArtifactsPublisherPort (семантика publish, не persist).
3. Реализовать 10 production-файлов: events.go, inbound.go, llm.go, llm_errors.go, agents.go, idempotency.go, pending.go, dm.go, publisher.go, router.go.
4. Написать 10 test-файлов: per-file compile-time stub-checks + smoke tests + JSON round-trip + omitempty contract + nil-safety + catalog exhaustiveness + IsAuthError + errors.As unwrap + XOR validity для PersistConfirmation.
5. Прогнать `make build/test/lint -race`.
6. Code review через golang-pro + architect-reviewer (параллельно) — применить MUST/SHOULD/LOW-FIX.

### Прогресс
- ✅ Pkg `internal/domain/port`: 10 production-файлов + 10 test-файлов.
- ✅ Hermetic: только stdlib (context, errors, fmt, encoding/json, strings, time) + `contractpro/legal-intelligence-core/internal/domain/model`. Никаких infra/agents/llm импортов — чистый inner ring hexagonal arch.
- ✅ **events.go** — wire DTO 6 inbound (VersionProcessingArtifactsReady с ParentVersionID/CreatedByUserID, VersionCreated с version_number int + JobID omitempty, ArtifactsProvided с map[ArtifactType]json.RawMessage + MissingTypes, LegalAnalysisArtifactsPersisted compact, LegalAnalysisArtifactsPersistFailed с IsRetryable, UserConfirmedType с ContractType string) + 4 outbound (GetArtifactsRequest, LegalAnalysisArtifactsReady с optional RiskDelta v1.1, LICStatusChangedEvent с Stage/ErrorCode/ErrorMessage/IsRetryable omitempty, ClassificationUncertain) + LICDLQEnvelope (PII-safe: HMAC original_message_hash + size + payload_storage_key для publish-failed) + DLQTopic enum 4 значения.
- ✅ **llm.go** — LLMProviderPort (ID/Complete/HealthCheck) + Turn (Role user/assistant) + CompletionRequest (10 полей: AgentID, Model, System, User, PriorTurns, MaxTokens, Temperature, StopSequences, JSONMode, JSONSchema) + CompletionResponse (8 полей с CachedInputTokens отдельно) + StopReason (end_turn/max_tokens/stop_sequence/content_filter) + Role.IsValid/StopReason.IsValid/LLMProviderID.IsKnown helpers.
- ✅ **llm_errors.go** — LLMProviderError (Code + Retryable + FallbackEligible + RetryAfter + Wrapped) с errors.Is/As (Unwrap()) + LLMErrorCode 11 значений (TIMEOUT/RATE_LIMIT/SERVER_ERROR/NETWORK/OVERLOADED/INVALID_API_KEY/QUOTA_EXCEEDED/CONTENT_POLICY/CONTEXT_TOO_LONG/MALFORMED_REQUEST/ALL_PROVIDERS_FAILED) + llmCodeCatalog с canonical (Retryable, FallbackEligible) пары + NewLLMProviderError + AsLLMProviderError + IsAuthError/IsRetryable/IsFallbackEligible nil-safe.
- ✅ **inbound.go** — 5 handler interfaces (VersionArtifactsReadyHandler, VersionCreatedHandler, ArtifactsProvidedHandler, PersistConfirmationHandler — объединяет HandlePersisted + HandlePersistFailed, UserConfirmedTypeHandler).
- ✅ **agents.go** — Agent interface (ID + Run) + AgentResult = any (тип-erased return для heterogeneous registry; stage executor dispatches type-assertion по AgentID).
- ✅ **idempotency.go** — IdempotencyStorePort (SetNX + Get + ExtendTTL + SetCompleted + SetPaused) + IdempotencyStatus enum 4 значения (absent ""/PROCESSING/PAUSED/COMPLETED) с IsTerminal helper + ErrIdempotencyKeyExists sentinel.
- ✅ **pending.go** — PendingStatePort (Save + Load + Delete; Save принимает *model.PendingTypeConfirmation + TTL) + ErrPendingStateNotFound sentinel (errors.Is matchable).
- ✅ **dm.go** — 4 порта: ArtifactRequesterPort (RequestArtifacts с correlation_id + 5 IDs + []model.ArtifactType), AnalysisArtifactsPublisherPort (Publish с LegalAnalysisArtifactsReady), ArtifactsAwaiterPort (Register/Await/Cancel + ErrAwaitTimeout + ErrDuplicateRegistration), PersistConfirmationAwaiterPort (зеркальный API на job_id). PersistConfirmation discriminated-union с NewPersistConfirmationSuccess/Failure constructors (panic-on-nil) + IsSuccess/IsFailure/IsValid XOR helpers.
- ✅ **publisher.go** — 3 publishers: StatusPublisherPort (PublishStatus), UncertaintyPublisherPort (PublishClassificationUncertain), DLQPublisherPort (PublishDLQ с topic + envelope).
- ✅ **router.go** — ProviderRouterPort (Complete + CompleteRepair sticky) + PrimaryCallResult (Response + UsedProvider — OQ-10 invariant).
- ✅ Compile-time checks `var _ Port = (*fake)(nil)` для всех 21 интерфейсов (5 handlers + LLMProviderPort + Agent + IdempotencyStorePort + PendingStatePort + 4 DM ports + 3 publishers + ProviderRouterPort).
- ✅ `make build/test/lint` — все три цели зелёные. `go test -race` — 0 races.
- ✅ Тесты: 248 PASS по всему модулю (29 config + 100 model + 20 port + 46 logger + 25 metrics + 24 tracer + smoke), 0 FAIL.

### Summary
Готов production-ready port-пакет для LIC. Полное соответствие llm-provider-abstraction.md §1-2 + event-catalog.md §1-3 + integration-contracts.md §1-2 + FROZEN DM event-catalog §1.4-1.5/§2.1-2.2. Hermetic, zero внешних deps. Готова основа для всех инфраструктурных и функциональных задач следующего слоя: LIC-TASK-014/015/016 (Claude/OpenAI/Gemini adapters — реализуют LLMProviderPort), LIC-TASK-017 (rate limiter), LIC-TASK-018 (cost tracker), LIC-TASK-019 (Provider Router — реализует ProviderRouterPort), LIC-TASK-024 (BaseAgent — использует ProviderRouterPort), LIC-TASK-025-033 (9 агентов — реализуют Agent), LIC-TASK-036 (Pipeline Orchestrator — coordinator всех портов), LIC-TASK-037 (PendingTypeConfirmationManager — использует PendingStatePort), LIC-TASK-038 (Idempotency Guard — реализует IdempotencyStorePort), LIC-TASK-039 (Consumer — десериализует в wire-DTO + вызывает handlers), LIC-TASK-040 (Event Router — диспатчит между handlers), LIC-TASK-041 (DM awaiters — реализуют ArtifactsAwaiterPort + PersistConfirmationAwaiterPort), LIC-TASK-042-046 (publishers).

### Notes
- **Naming deviations from acceptance criteria документированы:**
  - `ArtifactPersistencePort` (tasks.json) → `AnalysisArtifactsPublisherPort` (код). Семантика: метод publish-event (fire-and-forget); persistence — DM-side side effect, подтверждается через `PersistConfirmationAwaiterPort`. Согласовано с code-architect; вариант с сохранением имени из task'а отклонён как менее точный.
  - `DMResponseAwaiterPort` (tasks.json, единственное число) → `ArtifactsAwaiterPort` + `PersistConfirmationAwaiterPort` (2 порта). Разные lookup keys (correlation_id vs job_id), разные TTLs (~5s vs ~30s), разные failure modes (timeout vs explicit PersistFailed). ISP-compliant. Архитектура LIC-TASK-041 явно их разделяет.
- **`PersistConfirmation` discriminated-union:** в первой итерации был простой struct с двумя `*X` указателями; после golang-pro review добавлены `NewPersistConfirmationSuccess`/`NewPersistConfirmationFailure` constructors с panic-on-nil (защита от half-construction) + `IsValid()` XOR helper (защита от both-set ambiguity при literal-construction). IsSuccess/IsFailure возвращают false на ambiguous/empty состояние — caller должен проверить IsValid сначала или использовать constructors.
- **LLMProviderPort adapter-invariant:** signature остался `(CompletionResponse, error)` (не `*LLMProviderError`) — позволяет адаптерам возвращать typed-nil без gotchas; godoc усилен явным требованием "errors.As на *LLMProviderError должно срабатывать на любой не-nil error" + ссылка на `AsLLMProviderError` helper. Router-side тест enforce'ит этот контракт (LIC-TASK-019).
- **AgentResult = any** — обоснование в godoc agents.go: heterogeneous registry для stage executor требует erasure; marker interface добавил бы 9 single-line методов в model/ (touch outside LIC-TASK-013 scope); type-safe assertion в executor — per-AgentID dispatch table (LIC-TASK-034). При желании marker interface можно ввести в LIC-TASK-035 без breaking change.
- **`LLMErrorCode` rename** оправдан: `model.ErrorCode` — pipeline-state taxonomy (20 кодов: DM_PERSIST_FAILED, AGENT_TIMEOUT, ANALYSIS_TIMEOUT...); `port.LLMErrorCode` — wire-level taxonomy (11 кодов: TIMEOUT, RATE_LIMIT, INVALID_API_KEY...). Router-слой выполняет mapping; qualified imports (port.* vs model.*) делают translation самодокументируемой.
- **PrimaryCallResult.UsedProvider** — sticky-provider invariant OQ-10 enforced на type-level: CompleteRepair требует usedProvider parameter, нельзя её "забыть" передать.
- golang-pro code review: 0 MUST-FIX + 4 SHOULD-FIX (все применены: gofmt alignment в CompletionResponse, NewPersistConfirmation constructors + IsValid XOR, усиление godoc LLMProviderPort с adapter-invariant, `strings.Contains` вместо самописного `contains`) + 8 NIT (selectively applied). architect-reviewer PASS без HIGH-severity: 10/10 верификационных пунктов confirmed (LLMProviderPort §1.1 поля, LLMErrorCode §1.2 матрица, ExternalStatus 3 значения, Inbound DTO fidelity с FROZEN DM/Orch, DLQ envelope PII-safe, 4 DLQ топика, Idempotency 4 статуса, Hexagonal hygiene, 6+4 events покрыты, Sticky-provider invariant); MEDIUM PersistConfirmation closed via IsValid XOR; LOW ErrDuplicateRegistration sentinel закрыт.
- Тесты НЕ `t.Parallel` — большинство тестов hermetic и быстрые (microseconds); явный `t.Parallel()` на 4 helpers тестах не имеет смысла оптимизировать.
- Зависимостей в go.mod не добавлено — пакет полностью hermetic.

### Следующая задача
Разблокированы все следующие critical-задачи pipeline:
- LIC-TASK-014 (Claude provider adapter — реализует LLMProviderPort) — зависит от 013 (done).
- LIC-TASK-015 (OpenAI provider adapter) — зависит от 013 (done). Параллельно с 014.
- LIC-TASK-016 (Gemini provider adapter) — зависит от 013 (done). Параллельно с 014/015.
- LIC-TASK-017 (Rate limiter — token bucket в Redis) — зависит от 007 (pending Redis).
- LIC-TASK-018 (Cost & Usage Tracker) — зависит от 005 (done) + 013 (done).

Также по-прежнему свободны: LIC-TASK-003 (Dockerfile distroless), LIC-TASK-007 (Redis), LIC-TASK-008 (RabbitMQ), LIC-TASK-010 (concurrency limiter), LIC-TASK-020 (embed prompts/schemas), LIC-TASK-021 (Token Estimator).

---

## LIC-TASK-014 — Claude provider adapter (Anthropic Messages API + tool_use)
- **Status:** done
- **Completed at:** 2026-05-15
- **Agent:** claude-opus-4-7 (с консультациями code-architect, golang-pro, security-engineer, architect-reviewer)

### План реализации
1. Прочитать llm-provider-abstraction.md §1.1–1.6 (LLMProviderPort контракт + Anthropic tool_use для structured outputs + prompt cache), §1.2 error matrix (10 кодов), error-handling.md §3.4 + §7.3, security.md §3/§6/§8 (API key, allowlist, TLS), configuration.md §2.7 (LIC_CLAUDE_*).
2. Согласовать структуру через code-architect (Q1–Q10): 6-файловый split (provider.go + config.go + payload.go + response.go + errors.go + CLAUDE.md — последний skipped per LIC convention из 011-013); HTTPClient interface для test injection; per-request Model override; fixed virtualToolName="return_analysis_result"; всегда-array system block; Retry-After integer+HTTP-date parser; ctx.Cancellation+DeadlineExceeded → LLMErrorTimeout (deviation from DP OCR pass-through pattern, documented).
3. Реализовать 5 production-файлов с hermetic deps (stdlib + internal/domain/{model,port} only): provider.go (Provider struct + NewClaudeProvider + Complete + HealthCheck + ID + do helper + HTTPClient interface + constants), config.go (ClaudeConfig + Validate + defaultHTTPClient TLS 1.2), payload.go (anthropicRequest DTO + buildRequestPayload + buildSystemBlocks), response.go (anthropicResponse + parseResponse + extractContent + mapStopReason), errors.go (mapTransportError + mapHTTPError + classify400 + parseRetryAfter).
4. Реализовать 5 test-файлов с httptest fakes и race-safe assertions: 55 тестов initial → 61 после code review (+6 от feedback).
5. Прогнать `make build/test/lint -race`.
6. Параллельный code review: golang-pro + security-engineer.
7. architect-reviewer финальный аудит соответствия спеке.

### Прогресс
- ✅ Pkg `internal/llm/claude`: 5 production-файлов + 5 test-файлов.
- ✅ Hermetic: только stdlib (bytes, context, crypto/tls, encoding/json, errors, fmt, io, net, net/http, net/url, strconv, strings, time, unicode/utf8) + `contractpro/legal-intelligence-core/internal/domain/port`. Никаких Anthropic SDK, никаких observability/config импортов — adapter полностью изолированный.
- ✅ **provider.go**: exported тип `Provider` (после golang-pro M1: было `*claudeProvider` — unexported тип из exported конструктора блокировал бы Phase-4 wiring и go vet); HTTPClient interface co-located с типом; constants anthropicVersionHeader=2023-06-01 + messagesEndpointPath=/v1/messages + healthCheckUserContent=ping + maxResponseBytes=8MiB + maxDrainBytes=64KiB; compile-time check `var _ port.LLMProviderPort = (*Provider)(nil)`.
- ✅ **config.go**: ClaudeConfig (APIKey + BaseURL + Model + PromptCacheEnabled + optional HTTPClient — RPS/Burst НЕ в адаптере, те для LIC-TASK-017 rate limiter); Validate с aggregated errors.Join и проверками: пустые поля, валидность URL, scheme∈{http,https}, **userinfo=nil** (security-engineer S1 — defense in depth против url.Error echoes credentials); defaultHTTPClient enforces tls.VersionTLS12 без InsecureSkipVerify, БЕЗ Client.Timeout (caller's ctx owns budget per error-handling.md §7.3), Transport tuned по DP convention (MaxIdleConns=10, MaxIdleConnsPerHost=5, IdleConnTimeout=90s, TLSHandshakeTimeout=10s).
- ✅ **payload.go**: anthropicRequest с полями Model/System/Messages/MaxTokens/Temperature/StopSequences/Tools/ToolChoice (omitempty где нужно); systemBlock + cacheControlBlock + anthropicMessage + anthropicTool + anthropicToolChoice; buildRequestPayload валидирует PriorTurns.Role (invalid → LLMErrorMalformedRequest defensive), составляет messages из PriorTurns+финальный user, добавляет system блок ВСЕГДА-array form, добавляет Tools+ToolChoice при JSONSchema≠nil; buildSystemBlocks с conditional cache_control:ephemeral. virtualToolName="return_analysis_result" + virtualToolDescription — agent identity exposed только через OTel attributes, не на wire.
- ✅ **response.go**: anthropicResponse + anthropicContent + anthropicUsage (с CacheReadInputTokens отдельным полем); parseResponse сначала пытается json.Unmarshal (failure → LLMErrorServerError, не MALFORMED — golang-pro M4: provider sent corrupt JSON, не наш bug); extractContent в expectToolUse режиме демандует tool_use с virtualToolName + non-empty Input (mismatch → LLMErrorServerError), иначе strings.Builder.WriteString для всех text-блоков; mapStopReason: "end_turn"+"tool_use"→EndTurn, "max_tokens"→MaxTokens, "stop_sequence"→StopSequence, **"refusal"→StopReasonContentFilter** (golang-pro S6: Anthropic emit "refusal" при content-policy denial, Router map в LLMErrorContentPolicy через port spec §1.1), default→EndTurn.
- ✅ **errors.go**: mapTransportError маппит context.DeadlineExceeded+context.Canceled+net.Error.Timeout()→LLMErrorTimeout, прочие net errors→LLMErrorNetwork (документированное отклонение от DP OCR pass-through pattern — adapter invariant llm-provider-abstraction.md §1.2 требует ВСЕ ошибки typed); mapHTTPError маппит 401/403→InvalidAPIKey, 429→RateLimit с RetryAfter из заголовка, 408→Timeout, 529→Overloaded, 5xx→ServerError, 400→classify400, default→ServerError (golang-pro S9: преж. MALFORMED не давал Router fallback на 451/421/etc.); classify400 порядок: isContextLength→ContextTooLong, isQuotaExceeded→QuotaExceeded (security-engineer N4 — billing/quota/credit balance markers), isContentPolicy→ContentPolicy, fallback→MalformedRequest; httpStatusError truncate body @512 bytes с UTF-8 rune-boundary safety (security-engineer N1); parseRetryAfter поддерживает integer seconds + HTTP-date + maxRetryAfter=1h cap (security-engineer N2 — защита от Retry-After: 9999999).
- ✅ Adapter invariant: КАЖДАЯ non-nil ошибка из Complete/HealthCheck unwraps к `*port.LLMProviderError` через errors.As. Enforced на 5 error-construction sites + 2 defensive wraps в HealthCheck fall-through пути; покрыт тестом `TestComplete_AdapterInvariant_AllErrorsAreLLMProviderError` (6 случаев).
- ✅ Тесты 61 PASS включая: happy text-only Complete + JSONSchema/tool_use + prompt cache marker (с CachedInputTokens из usage.cache_read_input_tokens); 401/403→InvalidAPIKey с IsAuthError()=true; 429 с Retry-After integer/HTTP-date; 529→Overloaded; 5xx→ServerError; 400 context-too-long + quota_exceeded (type + message variants) + content-policy + unparsable body + other-invalid; ctx-cancellation/deadline→Timeout; network hijack-close→Network; AdapterInvariant 6 случаев; **TestComplete_ErrorsDoNotLeakAPIKey** (security canary с unwrap-chain walk через 5 error path); **TestComplete_Concurrent_NoRace** 32 параллельных вызова; HealthCheck happy + 401 typed + **503 typed** + network transport; BaseURL trailing-slash trim + **userinfo rejection**; per-request Model override; mapStopReason все 6 значений; parseRetryAfter cap (adversarial 9999999 + HTTP-date 48h-future).
- ✅ `make build/test/lint -race` — все цели зелёные.
- ✅ Тесты: 303 PASS по модулю (29 config + 100 model + 20 port + 46 logger + 25 metrics + 24 tracer + 59... финально 61 в claude после code review = 305+), 0 FAIL.

### Summary
Production-ready Claude provider adapter для LIC. Полное соответствие llm-provider-abstraction.md §1.1–§1.6 (LLMProviderPort контракт + Anthropic native chat format + tool_use structured outputs + Prompt Caching). Hermetic, zero внешних deps (нет SDK, ручной HTTP). Adapter invariant enforced: every error wraps to *LLMProviderError. Готова основа для LIC-TASK-019 (Provider Router — использует Complete + CompleteRepair sticky), LIC-TASK-024 (BaseAgent — единственный caller адаптера через Router), LIC-TASK-015/016 (OpenAI + Gemini adapters — следуют тому же 5-файл pattern).

### Notes
- **Naming deviations:** acceptance-criteria указывает `*claudeProvider` (unexported), но golang-pro M1 правильно flagged что unexported тип из exported конструктора блокирует Phase-4 wiring (cannot type return in main.go) — переименован в `*Provider` (exported, idiomatic Go). Compile-time check pattern `var _ port.LLMProviderPort = (*Provider)(nil)` сохранён. Acceptance-criteria также упоминают RPS/Burst в ClaudeConfig — те concerns rate-limiter (LIC-TASK-017), сознательно НЕ в адаптере.
- **Adapter invariant** — критичный контракт port-уровня: КАЖДАЯ non-nil ошибка из Complete/HealthCheck unwraps к `*port.LLMProviderError`. Соблюдён на 5 error-construction sites + 2 defensive wraps в HealthCheck (после golang-pro M2/M3). Без этого Router-side typed-switch decisions становятся неполными.
- **Error code semantics** — серьёзная семантическая дискуссия с golang-pro M4: первая итерация маппила json.Unmarshal failure + missing tool_use на LLMErrorMalformedRequest (Retryable=false, FallbackEligible=false). Но MALFORMED зарезервирован для "**наш**-bug" payload-ошибок (re-send не поможет). Provider sent corrupt JSON / violated tool_choice contract — это **provider misbehaviour**, должно быть LLMErrorServerError (Retryable=true, FallbackEligible=true) чтобы Router мог retry на том же провайдере или fallback к другому. Применено: json.Unmarshal failure → SERVER_ERROR; missing tool_use → SERVER_ERROR; empty tool input → SERVER_ERROR; no text blocks → SERVER_ERROR. MALFORMED только для **наших** payload-bugs (json.Marshal failure на наш struct, invalid PriorTurns Role, unparsable 400 body — последнее conservative).
- **Always-array system block** — упрощает code path (один cache_control conditional toggle вместо scalar-vs-array shape switch). Wire-size одинаков; future-proof для multi-block systems. Применяется только при req.System ≠ "" — иначе json:"system,omitempty" дропает ключ.
- **Sticky-provider repair semantics** — adapter сам по себе stateless; не различает primary vs repair calls. PriorTurns serialise в messages[] последовательно; Router (LIC-TASK-019) хранит UsedProvider и passes его в CompleteRepair.
- **TLS 1.2 floor** на адаптерском уровне (config.go MinVersion=TLS 1.2). Production-TLS enforcement (no http:// in URL) — на config-layer (LIC_ENV-driven). Adapter сам по себе scheme-permissive для httptest. Дополнительный security guard: Validate() rejects BaseURL с userinfo (defense in depth против `*url.Error.Error()` echoing credentials в logged строку).
- **Caller's ctx owns timeout** — `*http.Client.Timeout = 0`. http.Transport имеет TLSHandshakeTimeout=10s + IdleConnTimeout=90s но это connection lifecycle, не per-request. Иначе double-budget race ломал бы error-handling.md §7.3 hierarchical timeout invariant.
- **Bounded body drain** — golang-pro M5 нашёл что deferred io.Copy(io.Discard, resp.Body) без cap позволил бы adversarial peer стримить 100 GiB после 4xx и блокировать goroutine. Applied `io.LimitReader(resp.Body, maxDrainBytes=64KiB)` — sacrifices keep-alive reuse на oversized peer, но bounds tail latency.
- **Secret hygiene canary** — TestComplete_ErrorsDoNotLeakAPIKey walks полный unwrap-chain через 5 error paths (401, 429, 500+body, corrupt-2xx, hijack-close), asserting API key никогда не появляется. Defense against future refactors которые могли бы wrap key в deeper layer.
- code reviews: **golang-pro** 5 MUST-FIX + 13 SHOULD-FIX + 12 NIT; **security-engineer** 0 MUST + 2 SHOULD + 5 NIT; **architect-reviewer** PASS-WITH-NITS без HIGH/MEDIUM, 12/12 верификационных пунктов confirmed.
- Зависимостей в go.mod НЕ добавлено — пакет hermetic (только stdlib + internal/domain/port).

### Следующая задача
Разблокированы:
- LIC-TASK-015 (OpenAI provider adapter — Responses API, response_format=json_schema strict) — зависит от 013 (done). Тот же 5-файл pattern.
- LIC-TASK-016 (Gemini provider adapter — generateContent с responseSchema, role mapping Assistant→"model") — зависит от 013 (done).
- LIC-TASK-018 (Cost & Usage Tracker — Prometheus метрики с CachedInputTokens отдельной строкой, pricing table loader) — зависит от 005 (done) + 013 (done).

Свободны без новых блокеров: LIC-TASK-003 (Dockerfile distroless), LIC-TASK-007 (Redis), LIC-TASK-008 (RabbitMQ), LIC-TASK-010 (concurrency limiter), LIC-TASK-020 (embed prompts/schemas), LIC-TASK-021 (Token Estimator).

## LIC-TASK-008 — RabbitMQ broker client (publisher confirms, manual ACK, auto-reconnect, DLX-loop topology)
- **Status:** done
- **Started at:** 2026-05-15
- **Completed at:** 2026-05-15
- **Agent:** claude-opus-4-7 (консультации: code-architect [дизайн], golang-pro + security-engineer + code-reviewer [review])

### Обоснование выбора
Из eligible-задач (deps done): 003,007,008,010,016,018,020,021,035,041,052 — все critical кроме 010. LIC-TASK-008
разблокирует **7 задач** (009,039,042,043,044,045,046) — максимум; брокер — backbone event-driven LIC,
на критическом пути ко всей integration-фазе (до LIC-TASK-047).

### План реализации (валидирован code-architect, Q1–Q8)
1. Добавить `github.com/rabbitmq/amqp091-go v1.10.0` (версия из DP sibling).
2. Пакет `internal/infra/broker`, файлы: client.go, topology.go, publish.go, subscribe.go, reconnect.go, errors.go + test-peers + CLAUDE.md.
3. **errors.go**: broker-локальный типизированный `BrokerError{Op,Retryable,Cause}` + sentinels (НЕ model.ErrorCode — сломал бы SSOT-инвариант errorCatalog.init() + infra-ошибки не публикуются в Orchestrator). context.Canceled/DeadlineExceeded — passthrough raw (конвенция кодбазы). AMQP reply codes 404/403/406 → non-retryable, прочее retryable (зеркало DP).
4. **client.go**: инъектируемые `AMQPAPI`/`AMQPChannelAPI` (mock-тестирование без брокера, паттерн DP), real-wrappers, NewClient(cfg)+newClientWithAMQP(test seam), Ping (IsClosed-guard → open+close transient channel), graceful idempotent Close с handler-ctx cancel.
5. **topology.go**: статическая data-driven таблица из BrokerConfig (§6.1 routing-key map + retry TTLs). `DeclareTopology(ctx)`: 4 topic exchange (events/responses/commands/dlx) + 6 main queues + 18 retry queues + bindings. Единый `amqp.Table`-builder (byte-identical args для идемпотентного re-declare на reconnect — иначе 406 PRECONDITION_FAILED).
   - Main queue `lic.q.X`: durable, x-message-ttl=86400000, x-max-length=100000, x-dead-letter-exchange=contractpro.dlx; **БЕЗ статического x-dead-letter-routing-key** (escalation retry.N — динамическое решение consumer'а 043 по x-death.count, code-architect Q3 correction; задокументированное отклонение от литерального диаграмм-аннотейта §6.4, интент loop сохранён).
   - Retry queue `lic.q.X.retry.N`: x-message-ttl=ttlN(2s/10s/60s), x-dead-letter-exchange=contractpro.dlx, x-dead-letter-routing-key=RK (возврат в main).
   - Bindings: main ← source-exchange(RK) И main ← contractpro.dlx(RK, return path); retry.N ← contractpro.dlx(lic.q.X.retry.N). DLX = topic (НЕ fanout — иначе storm).
6. **publish.go**: выделенный publish channel в Confirm mode; Publish(ctx,exchange,rk,payload) под full Lock (сериализация confirm-wait против reconnect-swap, TOCTOU); NotifyPublish(size=1); select confirm/timeout(PublisherConfirmTimeout=5s)/ctx; retry 3× exp backoff+jitter; mandatory=false.
7. **subscribe.go**: Option B — broker-local `Delivery` интерфейс (Body/Headers/Ack/Nack/Reject/XDeath() []XDeathEntry — amqp-free decoded для 043, без утечки amqp091 в ingress); QoS prefetch per-channel; consumeLoop с lifecycle (done/closed/reconnect).
8. **reconnect.go**: reconnectLoop (NotifyClose + exp backoff+jitter 25%, cap), порядок: dial → pub-channel+Confirm() → DeclareTopology → per-sub channel → Qos → Consume.
9. Тесты против in-memory fake: publish→confirm, reconnect-redeclare, retry-queue TTL assertion, XDeath decode, topology completeness, DLX=topic assertion.
10. make build/test/lint + go test -race; параллельный review (golang-pro + security-engineer + code-reviewer); architecture-compliance vs integration-contracts §5/§6.

### Прогресс
- ✅ go.mod/go.sum: +github.com/rabbitmq/amqp091-go v1.10.0 (версия как в DP sibling, из module cache).
- ✅ 6 production-файлов + 6 test-peers + CLAUDE.md + config/broker_test.go. `go build ./...` OK, `go vet ./...` чисто.
- ✅ **errors.go**: BrokerError{Op,Retryable,Cause}+Unwrap; sentinels ErrNotConnected/ErrPublishNack/ErrConfirmTimeout; mapError (AMQP 404/403/406→non-retryable, прочее retryable, context passthrough raw); redactURLCredentials (security MF-1 — пароль из dial-ошибки не утекает).
- ✅ **client.go**: инъектируемые AMQPAPI/AMQPChannelAPI + real-wrappers + compile-time assertions; NewClient (dial с redact + openPublishChannel + DeclareTopology fail-fast + reconnectLoop); newClientWithAMQP test-seam; openPublishChannelOn(conn) (Confirm, не store); Ping (IsClosed-guard + Channel() off-goroutine с ctx-deadline + Close-error surface); idempotent Close с handler-ctx cancel; pubMu отдельно от mu.
- ✅ **topology.go**: subscriptionSpecs (6 frozen queue→exchange→rk §6.1); mainQueueArgs (durable/ttl 86400000/max-length 100000/DLX, БЕЗ статического dlrk — Q3); retryQueueArgs (ttlMillisInt32 clamp + DLX + dlrk=original RK); declareTopologyOn(conn) — 4 topic exchange + 6 main + 18 retry + bindings (main←source RK, main←dlx RK return, retry←dlx retryKey); DeclareTopology обёртка.
- ✅ **publish.go**: Publish под pubMu (НЕ mu — decouple от lifecycle, не стопорит Ping/Close/reconnect); per-attempt snapshot pubCh под mu; deferred confirm; waitConfirm select Done/ctx/c.done/timeout; classify: ctx→raw terminal, ErrNotConnected→terminal, ErrConfirmTimeout→retry, nack→retry; 3 attempts с backoff.
- ✅ **subscribe.go**: Delivery (amqp091-free: Body/Header/Headers shallow-copy/XDeath/Ack/Nack/Reject); XDeath decode с cap (64/64) + toInt64 (int*/uint*/float64, unknown→1<<62 exhausted, nil→0); Subscribe (record sub перед startConsumer, rollback по id); startConsumer (Qos prefetch + Consume + wg.Add под done-guard — golang-pro M2); consumeLoop (handler владеет ack).
- ✅ **reconnect.go**: reconnectLoop (NotifyClose буфер-1 + IsClosed re-check); reconnectWithBackoff (immediate first dial; no-store-until-validated: pub-channel+topology на newConn ДО adopt; done-atomic swap под mu; non-retryable DeclareTopology→backoff max не tight-loop; newConn не лик); backoffDelay exp+25% jitter cap.
- ✅ config/broker.go: upper-bound валидация LIC_CONSUMER_RETRY_TTL_* (<=24h) и LIC_PUBLISHER_CONFIRM_TIMEOUT (<=5m) — security MF-2 (overflow→406→outage).
- ✅ Тесты: 106 RUN/PASS subtests в broker, 0 FAIL, race-clean; config_test+broker_test PASS; весь модуль PASS; go vet чисто; make lint/build/test зелёные (docker-build — scope LIC-TASK-003).

### Summary
Production-ready RabbitMQ broker client для LIC — backbone event-driven архитектуры. Полное соответствие
integration-contracts §5/§6.1-§6.4 (topic exchange topology, 6 подписок, queue policies, DLX-loop retry queues
2s/10s/60s, publisher confirms, manual-ack prefetch). Hermetic кроме amqp091-go. 1 задокументированное
intent-сохраняющее отклонение (main-queue без статического x-dead-letter-routing-key — escalation = consumer
LIC-TASK-043). Разблокирует LIC-TASK-009,039,042,043,044,045,046.

### Notes
- **§6.4 deviation (code-architect Q3):** main-queue только x-dead-letter-exchange; динамический выбор retry.N
  по x-death[].count — зона consumer'а (043). Broker даёт примитив Delivery.XDeath() (amqp091-free, capped).
  Литеральная диаграмма §6.4 показывает dlrk=retry.1, но consumer-side текст §6.4 — динамику; интент сохранён.
- **Error-модель:** broker-локальный BrokerError, НЕ model.ErrorCode (errorCatalog.init() SSOT-panic; infra-ошибки
  не идут в Orchestrator). context-passthrough raw — конвенция кодбазы (как DP/LLM-адаптеры).
- **Publisher confirms via deferred confirmations** (не shared NotifyPublish-канал) — каждый publish владеет своим
  подтверждением, нет stale-confirmation correlation bug на timeout+retry. После review pubMu развязан с mu —
  медленный/мёртвый брокер НЕ стопорит Ping/Close/reconnect (инвариант swap-safety сохранён через per-attempt
  snapshot + deferred-confirm + retry, не lock-holding). Refinement code-architect Q4 после 3-reviewer pass.
- **Reconnect rework (golang-pro M2/M3):** no-store-until-validated + done-atomic swap устраняет wg.Add-after-Wait
  race и newConn-leak; immediate first dial убирает ~1s penalty (code-reviewer M2).
- **Security:** redactURLCredentials (пароль брокера не в логах — 152-ФЗ PII); config+adapter TTL upper-bound
  (overflow→outage); DLX=topic (не fanout); XDeath panic-safe + capped; defer-SF-3 (reconnect alert seam — поздние
  задачи, нет metrics-dep в infra by design, TODO в коде).
- amqp091-go v1.10.0 — единственная новая go.mod зависимость; версия совпадает с DP.

### Следующая задача
Разблокированы (deps теперь done): LIC-TASK-009 (deps 005,007,008 — нужен ещё 007 Redis), LIC-TASK-039
(deps 008,011,012 — wiring), LIC-TASK-042 (DLQ sender, dep 008), LIC-TASK-043/044/045 (consumers, dep 008),
LIC-TASK-046 (dep 008). Также свободны: LIC-TASK-003 (Dockerfile), LIC-TASK-007 (Redis), LIC-TASK-010
(concurrency), LIC-TASK-016 (Gemini), LIC-TASK-018 (Cost Tracker), LIC-TASK-020 (embed prompts), LIC-TASK-021,
LIC-TASK-035 (Aggregator), LIC-TASK-041 (DM awaiter), LIC-TASK-052.

## LIC-TASK-015 — OpenAI provider adapter (Responses API)
- **Status:** done
- **Completed at:** 2026-05-15
- **Agent:** claude-opus-4-7 (с консультациями code-architect, golang-pro, security-engineer)

### План реализации
1. Прочитать llm-provider-abstraction.md §1.1–1.6 (LLMProviderPort + structured outputs), port/llm.go + llm_errors.go (adapter invariant), config/llm.go (OpenAIProviderConfig), sibling internal/llm/claude (5+5 файлов — proven pattern), OpenAI Responses API (context7).
2. Согласовать дизайн через code-architect (Q1–Q10 + 8 нюансов sibling): зеркало claude-структуры; **ключевое решение Q2** — Responses API использует FLATTENED `text.format` (не Chat-Completions `response_format`); System→`input[0].role=developer` (не `instructions`); нет `stop` параметра; CachedInputTokens=0; HealthCheck floor 16.
3. Реализовать 5 production-файлов hermetic (stdlib + internal/domain/{model,port} only): config.go, provider.go, payload.go, response.go, errors.go.
4. Реализовать 5 test-файлов с httptest fakes: 71 → 75 subtests после review fixes.
5. Прогнать `make build/test/lint` + `go test -race`.
6. Параллельный code review: golang-pro + security-engineer.

### Прогресс
- ✅ Pkg `internal/llm/openai`: 5 production + 5 test файлов. Hermetic: zero новых go.mod deps (только stdlib + internal/domain/{model,port}). Никакого openai-go SDK — ручной HTTP.
- ✅ **provider.go**: exported `Provider` (конвенция sibling claude из LIC-TASK-014, не lowercase `openaiProvider` из иллюстративного текста арх); `var _ port.LLMProviderPort = (*Provider)(nil)`; NewOpenAIProvider + Complete + HealthCheck + ID + do + HTTPClient interface co-located; constants responsesEndpointPath=/v1/responses, healthCheckMaxTokens=16, maxResponseBytes=8MiB, maxDrainBytes=64KiB; bounded body drain через io.LimitReader; Authorization: Bearer header.
- ✅ **config.go**: OpenAIConfig (APIKey+BaseURL+Model+HTTPClient — БЕЗ PromptCacheEnabled: у OpenAI implicit cache; БЕЗ RPS/Burst — те для LIC-TASK-017); Validate с errors.Join + userinfo rejection + http/https-only; defaultHTTPClient TLS1.2 без client Timeout.
- ✅ **payload.go**: responsesRequest DTO; buildRequestPayload валидирует PriorTurns.Role, System→`input[0]{role:developer}` ТОЛЬКО при non-empty (HealthCheck без System), PriorTurns→user/assistant, JSONSchema→FLATTENED `text.format{type:json_schema,name:return_analysis_result,strict:true,schema}`, JSONMode→`text.format{type:json_object}`; **нет stop** (Responses API не поддерживает — задокументированное отклонение от sibling).
- ✅ **response.go**: parseResponse — json.Unmarshal failure на 2xx → LLMErrorServerError (provider misbehaviour, не MALFORMED, как claude M4); status=failed → SERVER_ERROR с bounded message; extractContent итерирует output[] message-items, output_text concat, reasoning items ignored; refusal или incomplete/content_filter → StopReasonContentFilter + **success** (Router маппит в LLMErrorContentPolicy через port godoc — зеркало claude refusal-пути); mixed output_text+refusal → refusal text wins (детерминизм); empty/whitespace (не на content-filter пути) → SERVER_ERROR; CachedInputTokens hardcoded 0; reportedModel fallback.
- ✅ **errors.go**: mapTransportError (ctx/net.Timeout→TIMEOUT, прочее→NETWORK); mapHTTPError 401||403→InvalidAPIKey, 429 insufficient_quota→QUOTA иначе RateLimit+RetryAfter, 5xx→ServerError, 400+422→classify4xx (context-length→ContextTooLong, quota→QuotaExceeded, content-policy→ContentPolicy, иначе MALFORMED), unknown→ServerError default (нет Anthropic-529 ветки); boundedDetail — единый UTF-8 rune-boundary 512B chokepoint для всех provider-controlled строк; parseRetryAfter RFC7231 + cap 1h + **reject signed delta-seconds** (сильнее sibling).
- ✅ Adapter invariant: каждая non-nil ошибка из Complete/HealthCheck unwraps к *port.LLMProviderError (включая json.Marshal нашего struct → MALFORMED, http.NewRequestWithContext failure → MALFORMED, invalid JSONSchema → MALFORMED). Покрыт TestComplete_AdapterInvariant (8 случаев) + canary.
- ✅ Тесты: 75 subtests PASS с -race в openai; весь модуль PASS; go vet чистый; make lint/build/test зелёные. Бинарь bin/lic-service gitignored — не в коммите.

### Summary
Production-ready OpenAI Responses API adapter. Соответствует llm-provider-abstraction.md §1.1–§1.6 и acceptance criteria LIC-TASK-015 (developer-role input, strict json_schema, error mapping, Bearer, HealthCheck, CachedInputTokens=0). Зеркало проверенного claude-паттерна. Разблокирует LIC-TASK-019 (Provider Router — Complete + CompleteRepair sticky).

### Notes
- **Ключевое архитектурное решение (code-architect Q2):** acceptance criteria и арх §1.5 буквально пишут `response_format:{type:json_schema,strict,schema}` — это формат **Chat Completions API**. Endpoint же — **Responses API** (`/v1/responses`, арх §1.3), который требует `text:{format:{type:json_schema,name,strict,schema}}` (json-schema поля FLATTENED внутри `format`, БЕЗ ключа `response_format`/`json_schema`). Реализован корректный Responses-формат — `response_format` против `/v1/responses` вернул бы 400. Архитектурный ИНТЕНТ (strict structured outputs) сохранён. **DOCS FOLLOW-UP:** арх §1.5 + acceptance criteria стоит переписать под Responses API.
- **HealthCheck max_output_tokens=16** — Responses API floor (отвергает <16 с 400). port/llm.go godoc и арх §2.3 говорят `max_tokens=10` (claude использует 10). Это сознательное provider-specific отклонение, громко задокументировано в коде (healthCheckMaxTokens const) — **DOCS FOLLOW-UP** для синхронизации спеки.
- **Refusal/content_filter → success:** 200 с refusal или incomplete/content_filter возвращает успешный CompletionResponse со StopReason=ContentFilter; Router маппит в LLMErrorContentPolicy (port godoc + арх §1.1). Это зеркалит claude mapStopReason("refusal") — оба адаптера ведут себя идентично для одного логического события.
- **StopSequences игнорируется:** Responses API не имеет `stop` параметра (в отличие от Chat Completions). port.CompletionRequest.StopSequences осознанно дропается; mapStopReason никогда не эмитит StopReasonStopSequence. LIC v1 агенты полагаются на strict JSON schema, не на stop-sequences (арх помечает поле "optional").
- **Review-фиксы:** golang-pro 0 MUST + применены S1 (документирован strict+omitempty invariant), S2 (mixed text+refusal детерминизм), S3.3 (тест invalid JSONSchema marshal-fail — reachable Router-driven path, untested и в sibling), S3.4 (empty error-object), N1 (drop unused Param), N3 (errors.New). security-engineer 0 MUST + применены S1 (status=failed message через boundedDetail/512B — закрыт unbounded PII/log-bleed по 152-ФЗ; реальный регресс vs sibling), S2 (canary расширен: status=failed inline + forced *url.Error). Threat-model: API key не утекает; SSRF adequate (operator-controlled endpoint); Retry-After hardening сильнее claude.
- Зависимостей в go.mod НЕ добавлено.

### Следующая задача
Разблокирована LIC-TASK-019 (Provider Router — per-agent primary + global fallback chain, sticky repair) при условии 014+015+016+017+018. Ещё открыты для следующей итерации (deps done): LIC-TASK-016 (Gemini adapter — тот же 5-файл pattern, responseSchema + role Assistant→"model"), LIC-TASK-018 (Cost & Usage Tracker), LIC-TASK-007 (Redis), LIC-TASK-008 (RabbitMQ), LIC-TASK-020 (embed prompts/schemas), LIC-TASK-021 (Token Estimator), LIC-TASK-003 (Dockerfile distroless), LIC-TASK-010 (concurrency limiter).

## LIC-TASK-007 — Redis-клиент (Lua scripts + TLS)
- **Status:** done
- **Completed at:** 2026-05-15
- **Agent:** claude-opus-4-7 (консультации: code-architect — design review; code-reviewer — финальное ревью)

### План реализации
1. Выбор задачи: среди eligible pending (deps done) LIC-TASK-007 — наивысший leverage critical: прямо разблокирует 009/017/037/038 + большую транзитивную часть pipeline. Deps 002/004 = done.
2. Проверка окружения: предыдущий session.log утверждал «все go-команды отклоняются» — ОПРОВЕРГНУТО: `go build/test/vet/mod tidy` + `make build/test/lint` уже в allowlist .claude/settings.json. `go build ./...` прошёл. Блокера нет.
3. Изучить config/redis.go+tls.go, DP-kvstore precedent, LIC broker precedent (client/errors/CLAUDE.md), port/idempotency+pending, arch §2.3 / §6.3 / §6.5 / §6.10 / §6.13, go-redis v9.18.0 API.
4. Design review code-architect: APPROVE Q1/Q2/Q5/Q6, CORRECT Q3 (per-source script cache), Q4 (экспорт config.UsesTLS вместо дублирования). 7 must-fix.
5. Реализовать 4 production + CLAUDE.md + 5 test файлов; экспорт config.UsesTLS; go-redis в go.mod через `go mod tidy` (офлайн из module-cache).
6. make build/test/lint + go test -race; финальное ревью code-reviewer.

### Прогресс
- ✅ Кэш проверен: go-redis/v9 v9.18.0 + ВСЕ транзитивные (dgryski/go-rendezvous, go.uber.org/atomic, zeebo/xxh3, klauspost/cpuid/v2, cespare/xxhash/v2) полностью офлайн в module-cache. `go mod tidy` отработал офлайн.
- ✅ **errors.go**: `ErrKeyNotFound` плоский sentinel (НЕ RedisError — downstream 037/038 делают errors.Is); `RedisError{Op,Retryable,Cause}`+Unwrap+IsRetryable; `mapError` (ctx raw → redis.Nil→ErrKeyNotFound → redis.ErrClosed→non-retryable → default retryable); `errClientClosed`; `redactURLCredentials` (152-ФЗ, сознательно дублировано из broker).
- ✅ **options.go**: `buildOptions`=ParseURL+overrides (DB всегда из cfg; password только при cfg.Password!=""; Read/WriteTimeout=DialTimeout — нет отдельных env §2.3); TLS harden MinVersion TLS1.2+ServerName, force-TLS поверх redis:// при cfg.UsesTLS(), без InsecureSkipVerify.
- ✅ **client.go**: `RedisAPI` seam (subset+redis.Scripter+io.Closer); `var _ RedisAPI=(*redis.Client)(nil)` БЕЗ wrapper-структур (в отличие от broker); `NewClient` fail-fast (ParseURL→Ping, redacted); test seam; `Ping` early ctx.Err() (без broker half-open workaround — go-redis честит ctx сам); idempotent concurrent-safe `Close`.
- ✅ **ops.go**: Get(redis.Nil→ErrKeyNotFound), Set(TTL), SetNX(TTL), Delete(variadic), Expire(false=key gone=heartbeat-stop), Eval (redis.Script.Run EVALSHA→EVAL + per-source sync.Map cache; Lua nil→(nil,nil)); `scriptFor`.
- ✅ **config**: `RedisConfig.usesTLS()`→экспорт `UsesTLS()` (SSOT TLS; tls.go обновлён). Чистый рефактор, без изменения поведения, config-тесты зелёные.
- ✅ Тесты: faithful fakeRedis + programmable mockRedis + redisErrStr (реальный NOSCRIPT-fallback). `go test -race` PASS; весь модуль PASS; go vet чистый; make build/test/lint зелёные; bin gitignored.
- ✅ code-reviewer: APPROVE, 0 BLOCKER/HIGH. M1 закрыт уточняющим комментарием в ops.go; L1/L2/N1/N2 приняты (broker-прецедент / test-only / doc-формулировка).

### Summary
Production-ready Redis-клиент. Соответствует configuration.md §2.3 (LIC_REDIS_* в точности) и high-architecture.md §6.3/§6.5/§6.10/§6.13 (SETNX/GET/EXPIRE-heartbeat/DEL/SET..EX/Eval token-bucket). Hexagonal: чистая инфра, без доменного порта (зеркалит broker). Разблокирует LIC-TASK-009/017/037/038.

### Notes
- **Прошлый session.log был ошибочен** про «go-команды отклоняются» — на деле они в allowlist; задача реализована полностью с прохождением тестов/линтера.
- **Отклонение miniredis:** недоступен офлайн, сеть недоступна. Замена — faithful in-memory fakeRedis + programmable mockRedis (intent-preserving, зеркалит broker no-live-broker). Lua-VM офлайн невозможен — Eval тесты проверяют РЕАЛЬНЫЙ `redis.Script.Run` EVALSHA→NOSCRIPT→EVAL dispatch-контракт (redisErrStr satisfying redis.Error) + per-source cache; поведение token bucket — в LIC-TASK-017. Документировано в kvstore/CLAUDE.md + helpers_test.go.
- **Экспорт config.UsesTLS (code-architect Q4):** усечён риск дрейфа TLS-решения между enforceTLS (§3 rule 10) и kvstore. SSOT.
- **make docker-build не запускался:** Docker daemon недоступен/не разрешён; вне test_steps (как и в предыдущих LIC-задачах). build/test/lint пройдены.
- **Сознательные отклонения от broker (в kvstore/CLAUDE.md):** нет wrapper-структур; нет half-open Ping workaround; NewClient vs DP NewKVStoreClient (stutter-free).

### Следующая задача
Разблокированы (deps done): LIC-TASK-009 (health/readyz — 005+007+008 ✓), LIC-TASK-017 (rate limiter Lua — 007 ✓), LIC-TASK-038 (idempotency guard — 007 ✓), LIC-TASK-037 (pause-resume — 036+007). Свободны также: LIC-TASK-016 (Gemini), LIC-TASK-018 (Cost Tracker), LIC-TASK-020 (embed prompts), LIC-TASK-021 (Token Estimator), LIC-TASK-035 (Aggregator), LIC-TASK-039 (Event Consumer), LIC-TASK-041..046, LIC-TASK-003, LIC-TASK-010, LIC-TASK-052. Рекомендация: LIC-TASK-038 (idempotency guard) или LIC-TASK-009 (health) — следующие critical на пути pipeline.

## LIC-TASK-016 — Gemini provider adapter (generateContent с responseSchema)
- **Status:** done
- **Completed at:** 2026-05-15
- **Agent:** claude-opus-4-7 (консультации: code-architect — design review; code-reviewer — финальное ревью)

### План реализации
1. Выбор: среди eligible pending (deps done) LIC-TASK-016 — наивысший приоритет/leverage critical. Завершает триаду LLM-адаптеров (claude 014 ✓ + openai 015 ✓ + gemini). dep=013 done. Разблокирует 019 (Provider Router).
2. Изучить llm-provider-abstraction.md §1.1–§1.6/§2.3, port/llm.go+llm_errors.go (adapter invariant), config/llm.go (GeminiProviderConfig), sibling openai (5+5, proven pattern) + claude. Подтвердить Gemini generateContent wire-формат через context7 (systemInstruction, contents role, generationConfig responseSchema/responseMimeType, finishReason, promptFeedback.blockReason, usageMetadata, error envelope status, x-goog-api-key).
3. Design review code-architect: APPROVE Q1–Q12 + 6 MUST-FIX.
4. Реализовать 6 production + 6 test файлов hermetic (stdlib + internal/domain/{model,port} only). Без новых go.mod deps.
5. make build/test/lint + go test -race; финальное ревью code-reviewer.

### Прогресс
- ✅ Pkg `internal/llm/gemini`: 6 production + 6 test файлов. Hermetic: zero новых go.mod deps. Ручной HTTP (нет Google GenAI SDK — арх §1.3 «в v1 REST»).
- ✅ **config.go**: GeminiConfig{APIKey,BaseURL,Model,HTTPClient}; Validate (userinfo reject + http/https-only + **isValidModelID path-safe charset** — MUST-FIX #2); defaultHTTPClient TLS1.2 без client Timeout. БЕЗ PromptCacheEnabled (Gemini cachedContent не в v1).
- ✅ **provider.go**: exported `Provider` (sibling-конвенция; отклонение от acceptance `(*geminiProvider)` задокументировано); per-call endpointFor (model в URL path — отличие от claude/openai); **auth x-goog-api-key header, НЕ ?key= query** (MUST-FIX #2/Q2, security 152-ФЗ); chooseModel с trim (L1); re-validate override model перед URL (MUST-FIX #2); do() с bounded read+drain; HealthCheck dual-return, без systemInstruction (MUST-FIX #5).
- ✅ **payload.go**: generateContentRequest DTO; buildRequestPayload — System->systemInstruction только non-empty, PriorTurns role Assistant->"model" (wireRole), JSONSchema->transformSchema+responseMimeType, JSONMode->responseMimeType only; stopSequences форвардится (Gemini поддерживает — Q5).
- ✅ **schema.go** (Gemini-специфичный, MUST-FIX #1): transformSchema draft-07 -> Gemini OpenAPI-3.0 Schema subset — UPPERCASE type, X|null->nullable, $ref inline из $defs/definitions + cycle/depth guard, const->enum + inferGeminiType (M1), oneOf->anyOf, single-allOf non-destructive inline (M2), strip $schema/$id/additionalProperties/etc.; un-transformable -> error -> MALFORMED до wire I/O.
- ✅ **response.go**: parseResponse — promptFeedback.blockReason precedence (MUST-FIX #3) -> ContentFilter SUCCESS; candidate finishReason safety-family -> ContentFilter SUCCESS; mapFinishReason детерминированный ordering (MUST-FIX #4), safetyRatings НЕ authoritative; thought-parts skip; decode-fail/empty -> SERVER_ERROR; CachedInputTokens=0; modelVersion fallback.
- ✅ **errors.go**: mapTransportError + mapHTTPError (401||403->InvalidAPIKey MUST-FIX #6 / 429+RetryAfter / quota->QUOTA / 408->TIMEOUT / 5xx->SERVER / 400-only classify400 / unknown->SERVER); parseRetryAfter RFC7231 cap 1h reject signed; boundedDetail UTF-8 512B chokepoint.
- ✅ Тесты: 102 RUN / 0 FAIL, race-clean; весь модуль PASS; go vet чистый; make build/test/lint зелёные; bin gitignored.
- ✅ code-reviewer: APPROVE, 0 BLOCKER/0 HIGH. Применены M1 (typeless const->enum инференс типа), M2 (single-allOf non-destructive merge), L1 (chooseModel trim) + pinning-тесты.

### Summary
Production-ready Gemini generateContent адаптер. Соответствует llm-provider-abstraction.md §1.1–§1.6/§2.3 и acceptance LIC-TASK-016 (systemInstruction, contents Assistant->model, responseSchema+responseMimeType, x-goog-api-key, error mapping, HealthCheck). Зеркало проверенного openai/claude паттерна + Gemini-специфика (model-in-path, schema-транслятор, blockReason). Разблокирует LIC-TASK-019 (Provider Router): 014+015+016 закрыты, остаются 017+018.

### Notes
- **Schema-транслятор (MUST-FIX #1):** Gemini-2.5 responseSchema = OpenAPI-3.0 subset, НЕ draft-07. Pass-through (как у claude/openai) сломал бы prod Gemini-fallback (400 на каждый structured-вызов). Реализован transformSchema; интент strict structured outputs сохранён. Gemini-3 series — новый responseFormat object (DOCS FOLLOW-UP; v1 default gemini-2.5-pro -> responseSchema как в acceptance).
- **content_filter:** blocked prompt / safety finishReason -> УСПЕШНЫЙ CompletionResponse{StopReason=ContentFilter}; Router маппит в LLMErrorContentPolicy. Паритет с claude refusal / openai refusal — все три адаптера идентичны для одного логического события.
- **Auth header (Q2):** x-goog-api-key, не ?key= query — ключ не утекает в URL/логи (152-ФЗ); закрывает регрессию из openai-canary.
- **Без package CLAUDE.md** — паритет с siblings claude/openai (нет CLAUDE.md); исчерпывающий package doc в provider.go.
- **make docker-build не запускался:** нет Docker daemon (вне test_steps, как во всех прежних LIC-задачах).

### Следующая задача
Разблокированы (deps done): LIC-TASK-017 (rate limiter token-bucket Redis Lua — 007 ✓), LIC-TASK-018 (Cost & Usage Tracker — 005 ✓) — оба нужны для LIC-TASK-019 (Provider Router; 014+015+016 ✓, осталось 017+018). Свободны также: LIC-TASK-009 (health), LIC-TASK-020 (embed prompts), LIC-TASK-035 (Aggregator), LIC-TASK-038 (idempotency guard), LIC-TASK-039 (Event Consumer), LIC-TASK-041..046, LIC-TASK-003, LIC-TASK-010, LIC-TASK-052. Рекомендация: LIC-TASK-017 или LIC-TASK-018 — последние два блокера Provider Router (центральный LLM-шлюз на критическом пути).

## LIC-TASK-017 — Rate limiter (token bucket в Redis, atomic Lua)
- **Status:** done
- **Completed at:** 2026-05-16
- **Agent:** claude-opus-4-7[1m] (консультации: code-architect — design review; code-reviewer + golang-pro — финальное ревью параллельно)

### План реализации
1. Выбор: среди eligible pending (deps done) LIC-TASK-017 — critical, наивысший leverage: один из двух последних блокеров LIC-TASK-019 (Provider Router — центральный LLM-шлюз на критич. пути ко всему пайплайну агентов). dep=007 (Redis) done. Прошлая сессия дизайн отревьюила, но код НЕ сохранён (ratelimit/ только .gitkeep, git clean) → перезапуск с опорой на зафиксированный в session.log дизайн + свежая верификация.
2. Изучены: kvstore (Eval/RedisAPI seam, (nil,nil) на Lua-nil, EVALSHA-кэш), port/llm.go+llm_errors.go (LLMProviderError/NewLLMProviderError/Unwrap, LLMProviderID.IsKnown), port/router.go, config/llm.go (per-provider RPS/Burst SSOT), metrics/llm.go (RateLimitedTotal), arch llm-provider-abstraction §2.1 (rateLimit callsite), §3.1-§3.3, configuration.md §2 SSOT.
3. Design review → code-architect: APPROVE-WITH-MUST-FIX (MF1 miniredis-офлайн-отклонение; MF2 виртуальные часы в fake; MF3 имя LuaEvaluator не ScriptRunner + assert в 047; MF4 redis.replicate_commands() первой строкой; MF5 maxSleep cap; OQ-A..F).
4. Реализация 3 prod + CLAUDE.md + 5 test-файлов hermetic (stdlib + internal/domain/port).
5. make build/test/lint + go test -race; финальное ревью code-reviewer + golang-pro (параллельно); применение фиксов.

### Прогресс
- ✅ **script.go**: `tokenBucketScript` (Lua) — redis.replicate_commands() первой строкой (нон-детерм. redis.TIME, min Redis 5.0), integer micro-tokens SCALE=1e6 (lossless HSET), cold key=full burst без refill от ts=0 (overflow guard), refill cap burst, persist HSET на allow И deny, EXPIRE max(window,60s), retry_after_ms≥1, return {allowed,retry_after_ms} never nil. `computeBucket` — Go SSOT-зеркало арифметики (math.Floor/Ceil после SF-4), используется fake → Lua/Go не дрейфят. `decodeResult` defensive (int64/int/float64-integral/string; nil/bad→errScriptAnomaly).
- ✅ **bucket.go**: `TokenBucket{provider,key=lic:rate:{provider},rps,burst,eval}` (без shard — acceptance + 152-ФЗ; §3.1 shard опц.). `outcome` таксономия (Allowed/Denied/InfraError/ScriptAnomaly). `allow` — один не-блокирующий Eval + классификация, никогда не возвращает Go-error.
- ✅ **ratelimit.go**: seams `LuaEvaluator` (Eval; *kvstore.Client сатисфит, assert в 047) + `Observer` (RateLimited denied-only / FailOpen / ScriptAnomaly) + noopObserver. `Config`/`ProviderLimit`, `NewLimiter` fail-fast (eval req, RPS≥minRPS=1e-6, Burst≥1). `Wait(ctx,providerID)` — token→nil; denied→RateLimited+computeSleep(retry-after+full-jitter, floor max(1ms,1/2rps), cap maxSleep=2s, clamp ctx, ранний выход <1ms)→reusable timer vs ctx.Done()→retry; ctx-expiry→NewLLMProviderError(RATE_LIMIT,ctx.Err()); unknown→MALFORMED; infra-fail→ctx.Err()-guard иначе fail-OPEN+FailOpen; anomaly→fail-OPEN+ScriptAnomaly.
- ✅ **CLAUDE.md**: зафиксированы все отклонения (no-shard, fail-open rationale, anomaly≠outage + transport-redis.Nil-неотличимость, ctx-expiry race, redis.TIME determinism/min-Redis, cold-key, doc-расхождение LIC_LLM_RPS_*, miniredis test-стратегия).
- ✅ Тесты: fake_test (fakeEvaluator на computeBucket + инъектируемые виртуальные часы + recordingObserver с firstDenied-каналом), script_test (computeBucket/decode/Lua-pin инварианты), bucket_test (outcome), ratelimit_test (validation/Wait/sleep/race/§2.1-совместимость), script_integration_test (//go:build redis_integration, сверка реал-Lua с computeBucket SSOT). 70 RUN/PASS -race clean; модуль 12 OK/0 FAIL; vet (+tag) чисто; build OK (gitignored).
- ✅ Найден+исправлен реальный баг при первом прогоне: computeSleep low-rps minSleep>maxSleep (точно MF-5).

### Финальное ревью (применённые фиксы)
- **code-reviewer:** H1 (ctx истекает между ctx.Err() и Eval → infra-путь fail-open маскировал таймаут вместо RATE_LIMIT) → ctx.Err()-guard на outcomeInfraError. M1 (transport redis.Nil ≈ script-nil) → доки CLAUDE.md. M2 (флака-тест) → канал firstDenied. M3 (integration-тест не сверял с computeBucket) → числовая сверка (0,maxRetry]. L1/L2/L3 → коммент/math.Floor/float64-case.
- **golang-pro:** MF-1 (tiny-RPS float→Duration overflow) → minRPS=1e-6 bound + base<0 guard + math.* . MF-2 (= M2). SF-1 (busy-spin в хвосте дедлайна) → ранний выход при clamp<1ms. SF-2 (комментарий timer.C/ctx.Done race) → уточнён. SF-3 (randF контракт) → godoc [0,1)+concurrent-safe. SF-4 (math vs ручной floor/ceil — math это stdlib, герметичность не нарушается) → math.Floor/Ceil. N-4/N-5 → godoc/тест.

### Summary
Production-ready per-provider token-bucket rate limiter. Соответствует llm-provider-abstraction.md §3.1-§3.2, §2.1 router-контракту и acceptance LIC-TASK-017. Hermetic, zero новых go.mod deps. Разблокирует LIC-TASK-019 (Provider Router): 014+015+016+017 закрыты, остаётся только LIC-TASK-018 (Cost & Usage Tracker).

### Notes / следующая задача
- **DOC FOLLOW-UP (LIC-TASK-047):** §3.2/acceptance пишут `LIC_LLM_RPS_<PROVIDER>`/`LIC_LLM_BURST_<PROVIDER>` — таких env НЕТ. Реальный SSOT (configuration.md §2 / config/llm.go) — `LIC_<PROVIDER>_RPS`/`LIC_<PROVIDER>_BURST`. LIC-TASK-047 ДОЛЖЕН мапить `Config.Providers[id]={cfg.LLM.<Provider>.RPS, cfg.LLM.<Provider>.Burst}`, НЕ §3.2-написание. Также в 047: `var _ ratelimit.LuaEvaluator=(*kvstore.Client)(nil)` + Prometheus/logger Observer-адаптер (RateLimitedTotal{provider} + сэмплированный WARN на FailOpen/ScriptAnomaly).
- **make docker-build не запускался:** нет Docker daemon (вне test_steps, как во всех прежних LIC-задачах).
- **Разблокирована LIC-TASK-018** (Cost & Usage Tracker — deps 005+013 done) — последний блокер LIC-TASK-019. Также открыты (deps done): LIC-TASK-009 (health), 020 (embed prompts), 021 (token estimator), 035 (aggregator), 038 (idempotency guard), 039 (event consumer), 041-046, 003, 010, 052. Рекомендация следующей итерации: **LIC-TASK-018** (закрывает Provider Router 019 — центральный LLM-шлюз).

---

## LIC-TASK-018 — Cost & Usage Tracker (Prometheus метрики + pricing table loader)

- **Status:** done
- **Completed at:** 2026-05-16
- **Agent:** claude-opus-4-7[1m] (консультации: code-architect — design review; code-reviewer + golang-pro — финальное ревью параллельно)

### План реализации
1. Выбор: среди eligible pending (deps done) LIC-TASK-018 — critical, наивысший leverage: ПОСЛЕДНИЙ блокер LIC-TASK-019 (Provider Router; 014/015/016/017 уже done). deps 005 (metrics) + 013 (ports) done. Прошлая сессия (017) явно отметила это в session.log.
2. Изучены: tasks.json scaffold (llm/{...,cost,pricing}/ — 2 отдельных пакета), arch llm-provider-abstraction §4.1 (формула + pricing table + LICCostSpike), observability §3.4/§3.10 (метрики lic_llm_*, cardinality budget, organization_id запрещён в labels), configuration §2.15 (pricing.yaml формат — БЕЗ cached-ключа в примере), metrics/llm.go (метрики УЖЕ объявлены централизованно — НЕ переопределять), domain/port/llm.go (CompletionResponse: InputTokens/CachedInputTokens/OutputTokens/LatencyMs), ratelimit (hermetic + Observer seam прецедент), tracer (SpanFields.AttrLLMCostUSD — Router/019). go.sum: go.yaml.in/yaml/v2 v2.4.2 уже запинён (indirect, transitive via prometheus/common).
3. Design review → code-architect: APPROVE-WITH-MUST-FIX (MF-1 batched Recorder seam; MF-2 ObserveSuccess эмитит И usage И calls{success}; MF-3 unknown-model distinct signal, model НЕ Prometheus-label; MF-4 per-term float64 promote; MF-5 cached absent→explicit 0.0+flag; OQ-1..7).
4. Реализация 2 герметичных пакетов + 2 CLAUDE.md + тесты.
5. make build/test/lint + go test -race; финальное ревью golang-pro + code-reviewer (параллельно); применение фиксов.

### Прогресс
- ✅ **internal/llm/pricing/pricing.go**: `ModelPricing{InputPerMTokenUSD, CachedInputPerMTokenUSD, OutputPerMTokenUSD, CachedRateDefaulted}`, `Table`, `Table.CostUSD` (SSOT-формула §4.1, per-term float64-promotion — no int overflow MF-4, clamp negative). `Load(path)` — os.ReadFile + `yaml.UnmarshalStrict` (reject unknown keys — typo в money-файле fail loud) + валидация (sorted-key детерминизм E1; missing input/output; reject `<0` И `NaN`/`±Inf` Y1; empty/no-models; pointer-decode `*float64` различает absent vs 0.0; cached absent→explicit 0.0+CachedRateDefaulted MF-5). `const tokensPerMillion` (F2). Typed path-wrapped errors, no panic, no fallback.
- ✅ **internal/llm/cost/cost.go**: `Outcome` (локальный mirror metrics.LLMCallOutcome — pin-тест против дрейфа SSOT). `Recorder`+`noopRecorder` seam — **batched** `RecordUsage`/`RecordCall`/`UnknownModel` (MF-1; success-atomicity внутри Tracker). `Usage` (БЕЗ organization_id — структурная гарантия §3.10/§4.2). `Tracker` immutable (table read-only + rec фиксирован → -race без локов). `NewTracker` fail-fast на пустую таблицу. `ObserveSuccess`→CostUSD + RecordUsage + RecordCall(success) MF-2 + UnknownModel при !known MF-3; возвращает USD (даже 0.0) для OTel-span Router'а (Tracker НЕ трогает span — OQ-2). `ObserveCall`→calls_total only (repair|fail|fallback). `nonNeg` clamp (Counter.Add(<0) panics).
- ✅ **2×CLAUDE.md**: все намеренные решения с атрибуцией ревьюера (hermetic boundary, yaml=go.yaml.in/yaml/v2 НЕ yaml.v3, strict decode, NaN/Inf, детерминизм, cached-default, nonNeg in-sync дублирование, Outcome mirror, immutable -race, latency-unit контракт) + forward-requirements для LIC-TASK-047.
- ✅ go.mod: go.yaml.in/yaml/v2 v2.4.2 indirect→direct (`go mod tidy` — ровно 1 строка перемещена, go.sum НЕ изменён, 0 новых модулей).
- ✅ Тесты: pricing_test (Load valid +/-cached, 11 error-кейсов вкл NaN/+Inf/-Inf/typo-strict, FileNotFound→os.ErrNotExist, formula step2=0.045, cached step3 0.30≠3.00, large-token MF-4 2e9, negative-clamp), cost_test (NewTracker validation, all-families+success-call, unknown-model, negative-clamp, call-only, Outcome-pin, concurrent 16×64). go test -race PASS clean; go test ./... = 14 пакетов ok/0 FAIL; vet (+tag redis_integration) чисто; make test/build/lint OK.

### Финальное ревью (применённые фиксы)
- **golang-pro:** no MUST-FIX. SHOULD: Y1 (Load принимал NaN/±Inf — `NaN<0`/`+Inf<0`==false → silently defeat LICCostSpike) → math.IsNaN/IsInf в валидации; F1 (док overstate accuracy) → precision-контракт rel-err ~1e-15; D1 (nonNeg дублирование) → cross-ref коммент о намеренном in-sync (hermeticity > DRY, прецедент ratelimit). NIT: F2 const tokensPerMillion; E1 sort.Strings (детерминизм multi-defect); N1 lockstep-коммент; E2 док 'descriptive не typed'; O1 usageCall gofmt-stable.
- **code-reviewer:** APPROVE. Все MF-1..MF-5 верифицированы реализованными; утечки organization_id в cost-метку нет (структурно). LOW: L-1 (arch §4.1 stale {provider,agent}+inline pricingTable vs §3.4 {provider,model,agent} — код на правильной стороне §3.4/§2.15; зафиксировано как doc-discrepancy для архитектора); L-2 (CachedRateDefaulted не потребляется Tracker — by-design, для 047); L-3 (нет large-token теста) → добавлен TestCostUSD_LargeTokenCounts_NoOverflow; L-4 (Outcome.IsValid не enforced) → cost/CLAUDE.md forward-req: 019/047 только exported консты.

### Соответствие архитектуре
- llm-provider-abstraction.md §4.1: формула, pricing-table через LIC_PRICING_TABLE_PATH, ModelPricing с раздельным CachedInput (anti 10×-over-bill) — ✅.
- observability.md §3.4: метрики lic_llm_{input,cached,output}_tokens_total/cost_usd_total{provider,model,agent}, latency-histogram, calls_total{+outcome∈success|repair|fail|fallback} — ✅ (Recorder зеркалит centrally-declared metrics/llm.go).
- observability.md §3.10/§4.2: organization_id НЕ в Prometheus labels (Usage без поля), per-tenant cost через OTel (Tracker возвращает cost, span владеет Router/019) — ✅.
- configuration.md §2.15: pricing.yaml формат, cached опционален — ✅.

### Summary
Production-ready Cost & Usage Tracker (2 пакета). Hermetic, zero новых go.mod-модулей. Соответствует §4.1/§3.4/§3.10/§4.2/§2.15 + acceptance LIC-TASK-018. **Разблокирует LIC-TASK-019 (Provider Router) — последний блокер закрыт (014/015/016/017/018 done).**

### Notes / следующая задача
- **DOC FOLLOW-UP (для архитектора):** llm-provider-abstraction.md §4.1 содержит УСТАРЕВШИЙ label-set `{provider, agent}` + inline hardcoded `var pricingTable` — противоречит observability.md §3.4 `{provider,model,agent}` + YAML-file-design (configuration.md §2.15 + §4.1's own note про LIC_PRICING_TABLE_PATH). Реализация следует авторитетному §3.4/§2.15 SSOT. Рекомендуется сверка §4.1.
- **FORWARD (LIC-TASK-047 app-wiring):** (1) реализовать Recorder-адаптер над *metrics.LLMMetrics: RecordUsage→Input/Cached/Output/Cost(.Add)+Latency(.Observe(.Seconds())); RecordCall→CallsTotal; UnknownModel→provider-labelled counter (e.g. lic_llm_pricing_unknown_model_total{provider}) + WARN-лог с model (НЕ label — cardinality MF-3). (2) `pricing.Load(cfg.Pricing.TablePath)` fail → FATAL startup (OQ-6). (3) `Usage.Latency=time.Duration(resp.LatencyMs)*time.Millisecond`; provider/model/agent straight-through. (4) 019/047 — только exported `cost.Outcome` консты (L-4).
- **make docker-build не запускался:** нет Docker daemon (вне test_steps, как во всех прежних LIC-задачах).
- **Разблокирована LIC-TASK-019** (Provider Router — все deps 014/015/016/017/018 done). Также открыты (deps done): 003, 009, 010, 020, 021, 035, 038, 039, 041-046, 052. Рекомендация следующей итерации: **LIC-TASK-019** (центральный LLM-шлюз, разблокирует каскад 023→024→025..034→036→037→047) либо параллельно-независимые 020/021/038/039.

---

## LIC-TASK-019 — Provider Router (per-agent default + global fallback chain, healthy registry, Complete + CompleteRepair sticky)

- **Status:** done
- **Completed at:** 2026-05-16
- **Agent:** claude-opus-4-7[1m] (консультации: code-architect — design review; golang-pro + code-reviewer — финальное ревью параллельно)

### План реализации
1. Выбор: среди eligible pending (deps done) LIC-TASK-019 — critical, узел критического пути. deps 014/015/016/017/018 все done (018 закрыта прошлой сессией как «последний блокер 019»). Альтернативы (003,009,020,021) дают меньший разблокирующий эффект — 019 открывает каскад 023→024→025..034→036.
2. Изучены: port/router.go (ProviderRouterPort + PrimaryCallResult), port/llm.go, port/llm_errors.go (llmCodeCatalog — SSOT Retryable/FallbackEligible), ratelimit.Limiter.Wait + cost.Tracker.ObserveSuccess/ObserveCall (seam-прецеденты + их CLAUDE.md), metrics/llm.go (4 router-метрики УЖЕ объявлены централизованно), config/{llm,agents}.go, model/agent.go, claude/provider.go (адаптер БЕЗ внутр. retry — retry/fallback = задача роутера), llm-provider-abstraction §2/§2.3, error-handling §4-§6.
3. Design review → code-architect: APPROVE-WITH-MUST-FIX (D1-D9). MF-1 errors.As всюду + любая ctx-ошибка после Wait abort всей chain; MF-2 side-effects не меняют control-flow (порядок metric→registry→решение по флагам); MF-3 единая state-transition fn (Complete-fail и healthcheck-fail) + skipped_unhealthy только top-of-iteration; MF-4 CompleteRepair unhealthy-escalation = ЛИТЕРАЛ &port.LLMProviderError{ServerError,false,false} (НЕ NewLLMProviderError — catalog-строка SERVER_ERROR даёт {true,true}); MF-5 UsageTracker seam = split ObserveSuccess/ObserveCall, success ОБЯЗАН вызвать tracker. Scope-cuts (circuit-breaker, cost→span) подтверждены OUT.
4. Реализация герметичного пакета (6 .go + CLAUDE.md) + тесты.
5. make build/test/lint + go test -race; финальное ревью golang-pro + code-reviewer (параллельно); применение фиксов.

### Прогресс
- ✅ **seams.go**: package-doc (hermeticity-инвариант). Три consumer-seam-а + noop defaults: `RateLimiter`(Wait), `UsageTracker`(ObserveSuccess/ObserveCall split — MF-5), `Metrics`(ProviderFallback/SkippedUnhealthy/Failed/HealthState — typed-параметры, bounded cardinality). `CallOutcome`/`HealthState` локальные mirror-типы (pin-тест против дрейфа SSOT). `Deps` (nil→noop/real). `sleepCtx` ctx-aware single-shot timer (Go 1.23+).
- ✅ **config.go**: `RouterConfig{AgentPrimary, FallbackOrder, HealthCheckInterval/Timeout}` + 30s/10s дефолты. Без env (герметично — env→RouterConfig маппинг = задача 047).
- ✅ **registry.go**: `healthRegistry` — ЕДИНАЯ (MF-3) `recordSuccess`/`recordFailure` по §2.3: auth→permanent-навсегда (quotaUntil zero), quota→permanent-24h-autorecover, retryable/transport→consecutive≥3 transient, non-retry+non-fallback (CONTEXT_TOO_LONG/MALFORMED)→health no-op (LOW-1). `isHealthy`/`shouldProbe`. health_status gauge только на смену состояния (emit вне lock — eventual-consistency window, self-heal 30s, N-2 документирован).
- ✅ **router.go**: `ProviderRouter` (immutable кроме mutex-registry → -race без локов на hot-path). `NewProviderRouter` fail-fast (пустые providers, fallback на незарегистрированного, дубли, AgentPrimary не покрывает 9 агентов / указывает на незарегистрированного). `Complete`: chain primary+fallback dedup; skip-unhealthy (skipped_unhealthy ТОЛЬКО здесь, lastErr НЕ затирается — MEDIUM-1); rate-limit Wait (ctx-ошибка→abort chain MF-1, non-ctx→skip+ProviderFailed+ObserveCall(fail) MEDIUM-2); `attempt` 1 same-provider retry по `backoffFor` §4.3 (200ms 5xx/net/timeout, RetryAfter|1s 429, 500ms 529), метрики/registry/cost 1×/итерацию; success→ObserveSuccess+fallback_total(i>0); !ok untyped→fail-no-fallback+UNKNOWN; flag-driven fallback/fatal; chain exhausted→ALL_PROVIDERS_FAILED(wrap lastErr).
- ✅ **repair.go**: `CompleteRepair` sticky single-shot (no retry/no fallback). ObserveCall(repair) ОДИН раз в начале (любой invoke, вкл. pre-wire escalations — metrics/llm.go SSOT, N-1). Unhealthy/unregistered → ЛИТЕРАЛ &port.LLMProviderError{...,false,false} (MF-4).
- ✅ **health.go**: `Start`/`Stop` race-safe через lifeMu+started/stopped (S-1: 2×sync.Once не дают happens-before), `Stop` блокирует на WaitGroup (no leak). `healthLoop` 30s ticker, `runHealthChecks` sequential + per-probe timeout, dual-return HealthCheck → единый registry-path.
- ✅ **CLAUDE.md**: все решения с атрибуцией ревьюера; doc-нестыковка §6.1↔catalog-SSOT; scope-cuts (circuit-breaker, cost→span); forward-reqs 047.
- ✅ Тесты: 39 (router/repair/registry/health/seams + fakes) — test_steps 1-5, MF-1..5, S-1 concurrent Start/Stop, MEDIUM-1 root-cause preserve, LOW-1 health no-op, concurrent Complete×64, wire-string pin. `go test -race ./internal/llm/router/` 39 PASS race-clean; `go test ./...` все ok/0 FAIL; vet чисто; make build/test/lint OK.

### Финальное ревью (применённые фиксы)
- **code-architect:** APPROVE-WITH-MUST-FIX — MF-1..5 (см. план §3), все реализованы.
- **golang-pro:** APPROVE (no MUST-FIX). gofmt подтверждён byte-level чистым (все 12 файлов, struct-выравнивание, const-блоки, import-группы — 0 дельт). SHOULD S-1 (cross-method Start/Stop data-race на stopFn/wg — 2×sync.Once без happens-before) → lifeMu+started/stopped. NIT N-1 (early-escalation repair не считал calls{repair}) → ObserveCall(repair) перенесён в начало метода (раз на любой invoke). NIT N-2 (gauge eventual-consistency при concurrent transitions) → коммент-уточнение.
- **code-reviewer:** APPROVE-WITH-FIX (no CRITICAL). Tenant-isolation/§3.10 подтверждены (нет organization_id; label из typed-источников; UNKNOWN bounded). MEDIUM-1 (skip затирал lastErr→synthetic SERVER_ERROR маскировал root-cause в ALL_PROVIDERS_FAILED.Wrapped) → lastErr только если nil. MEDIUM-2 (non-ctx Wait не звал ObserveCall(fail)→LICAllProvidersFailing alert не сработал бы на mis-wired primary) → добавлен ObserveCall(fail) (recordFailure намеренно НЕ зовётся — wiring-bug ≠ provider ill-health, документировано). LOW-1 (MALFORMED/CONTEXT_TOO_LONG копили consecutive→ложный transient при рекуррентном LIC-баге) → health no-op для non-retry+non-fallback. LOW-2 → +2 edge-теста (last-provider-skipped root-cause, non-fallback-fatal health no-op).

### Соответствие архитектуре
- llm-provider-abstraction.md §2.1: per-agent primary + global fallback dedup, Complete (skip-unhealthy/rate-limit/retry/fallback-по-флагу/fatal/ALL_PROVIDERS_FAILED), CompleteRepair sticky no-fallback (OQ-10), used_provider в PrimaryCallResult — ✅.
- llm-provider-abstraction.md §2.3: in-memory registry {healthy,permanent,last_check_at,consecutive_failures}, auth→permanent, quota→permanent-24h, 5xx→consecutive≥3, фоновый 30s healthcheck, /readyz-готовность (registry.isHealthy для 009) — ✅.
- error-handling.md §4.3 backoff (200/500/RetryAfter|1s), §5.3-§5.4 repair single-shot same-provider, §6 fallback-резюме — ✅. Doc-нестыковка §6.1 (INVALID_API_KEY/QUOTA/CONTENT_POLICY=fatal) vs §1.2-матрица+llmCodeCatalog (FallbackEligible=true) — роутер ветвится по флагу .FallbackEligible (catalog=SSOT, acceptance: fail только ContextTooLong/MalformedRequest); зафиксировано.
- observability.md §3.4/§3.10: 4 router-метрики через Metrics-seam (зеркалит centrally-declared metrics/llm.go), без organization_id, typed-label cardinality (~405 budget), UNKNOWN-sentinel bounded — ✅.

### Summary
Production-ready Provider Router. Hermetic (только stdlib + domain/{port,model}), zero новых go.mod-модулей. Соответствует §2.1/§2.3 + error-handling §4-§6 + observability §3.4/§3.10 + acceptance LIC-TASK-019. **Разблокирует LIC-TASK-023 (Schema Validator + Repair Loop) → 024 (BaseAgent) → весь 9-agent pipeline (025..034) → 036/037.**

### Notes / следующая задача
- **DOC FOLLOW-UP (для архитектора):** error-handling.md §6.1 перечисляет ErrLLMInvalidAPIKey/QuotaExceeded/ContentPolicyViolation как «fatal, не fallback», что противоречит §1.2-матрице, port.llmCodeCatalog (FallbackEligible=true) и acceptance LIC-TASK-019 («fail при ErrCodeContextTooLong/MalformedRequest» — только эти два). §6.1 — lossy-резюме; роутер следует флагу .FallbackEligible (catalog = SSOT). Рекомендуется сверка §6.1.
- **FORWARD (LIC-TASK-047 app-wiring):** (1) три seam-адаптера: RateLimiter→*ratelimit.Limiter.Wait (var _ assertion в wiring); UsageTracker→над *cost.Tracker (ObserveSuccess строит cost.Usage с Latency=time.Duration(resp.LatencyMs)*time.Millisecond — off-by-1000 hazard; ObserveCall→cost.Outcome(routerOutcome)); Metrics→над *metrics.LLMMetrics (HealthState→gauge 1 для state + 0 для других двух). (2) RouterConfig из config (AgentPrimary string→typed, FallbackOrder, interval 30s prod/60s staging). (3) Start(ctx) после wiring, Stop() ПЕРВЫМ в graceful shutdown. (4) circuit-breaker (gobreaker §3.3, lic_llm_provider_circuit_state) — отдельная будущая задача (OUT of 019). (5) cost-USD→OTel lic.llm span attribution — для span-owner (036/agent), роутер USD отбрасывает.
- **make docker-build не запускался:** нет Docker daemon (вне test_steps, как во всех прежних LIC-задачах).
- **Открыты (deps done) для следующей итерации:** 003, 009, 010, 020, 021, 023, 035, 038, 039, 041-046, 052. Рекомендация: **LIC-TASK-023** (Schema Validator + Repair Loop — теперь разблокирована 019+020; прямое продолжение критического пути к BaseAgent/024) либо параллельно-независимые 020/021/038/039. Прогресс: 16/55 done.

---

## LIC-TASK-020 — Embed system prompts & JSON schemas (embed.FS) [critical] — DONE 2026-05-16

### Выбор задачи
- done на момент старта: 001,002,004,005,006,007,008,011,012,013,014,015,016,017,018,019 (16/55).
- Кандидаты (critical, deps done): 003,009,020,021. Выбрана **020** — высший leverage: разблокирует всю agent-pipeline (022→023→024→025-034→036/037, ~23 задачи транзитивно). dep=[011] done.

### План реализации
1. Извлечь ПОБАЙТНО 9 промптов + 9 схем из ai-agents-pipeline.md §1-9 (SSOT, fenced-блоки).
2. Два sibling-пакета internal/agents/{prompts,schemas}: unexported embed.FS + explicit model.AgentID→basename SSOT-map + LoadPrompt/LoadSchema (verbatim, never panic) + Validate() (errors.Join, детерминированный порядок, fail-loud).
3. schemas доп.: well-formed JSON + $schema exact-pin draft-07 + non-empty title + draft-07 root type (string|array, НЕ только object).
4. Тесты (acceptance + fail-path) + CLAUDE.md в обоих пакетах.

### Архитектурное соответствие
- ai-agents-pipeline.md §1-9: 18 ассетов byte-for-byte = SSOT (подтверждено code-reviewer; bare-null enum §8, root array §6, кириллица/типографика целы).
- pricing-конвенция: hermetic (stdlib + только domain/model), strict fail-loud, детерминированный sorted error-order, Validate-returns-error (НЕ init-panic — embedded data ≠ compiled constants; code-architect Q4).
- code-architect design review: APPROVE + 3 must-fix (root array; $schema exact-pin; Validate-not-init-panic) — все применены.
- Scope-boundary: полная JSON-Schema meta-валидация осознанно отложена до LIC-TASK-023 (реальная JSON-Schema lib) — задокументировано.

### Ревью и правки
- golang-pro + code-reviewer: verbatim fidelity PASS (18/18). Применено: MF-1 (stale .gitkeep → git rm + правка CLAUDE.md), SF-1 (validBasename guard SSOT-таблицы), SF-2 (e.IsDir() skip), N-1 (agent id в errors), N-2a (FS-injectable cores loadPrompt/loadSchema/validate(fs.FS,map) + тесты детерминизма через testing/fstest.MapFS; публичный API не изменён).

### Тесты
- go test ./... зелёный; 19 тестов agents/prompts+agents/schemas PASS, включая -race. make build/lint/test — ok. make docker-build не запускался (нет Docker daemon; Dockerfile не менялся, go:embed-ассеты закоммичены).

### Notes / следующая задача
- Forward (LIC-TASK-024/047): prompts.Validate()/schemas.Validate() ДОЛЖНЫ вызываться на старте как fatal (как pricing.Load fail-fast) — задокументировано в обоих CLAUDE.md.
- Открыты (deps done): 003,009,010,021,022,023,035,038,039,041-046,052. Рекомендация след. итерации: **LIC-TASK-022** (Prompt Builder — dep=[020] done, прямой критический путь) или **023** (Schema Validator — dep=[019,020] done). Прогресс: **17/55 done**.

---

## LIC-TASK-022 — Prompt Builder (XML envelope + mandatory escaping, prompt-injection layer 2) [critical/security] — DONE 2026-05-16

### Выбор задачи
- done на момент старта: 17/55 (…,020). Доступных (deps satisfied) = 16, все critical.
- Объективная метрика транзитивного разблокирования: **022 = 21** (макс.), 023 = 20, остальные ≤10. Критический путь 022 → 024 (BaseAgent) → 025-033 (9 движков) → 034 → … → 047. dep=[020] done. Выбрана **LIC-TASK-022**.

### План реализации
1. Hermetic leaf-пакет `internal/agents/promptbuilder` (паттерн internal/llm/cost): stdlib + domain/{model,port} only; Prometheus инвертирован за Recorder seam + noopRecorder; adapter → LIC-TASK-047.
2. `escape.go` — ДВА раздельных эскейпера: `escapeText` (&<>, XML text-node) и `escapeAttr` (&<>" + strip C0/C1/DEL/newline, XML attribute). & первым, single-pass `strings.Replacer`.
3. `innogrn.go` — чистые FNS-checksum: `ValidateINN` (10/12), `ValidateOGRN` (13→LEGAL_ENTITY / 15→INDIVIDUAL_ENTREPRENEUR), pre-shape-gate, uint64 overflow-free, `safeEntityType` closed-set guard.
4. `builder.go` — закрытый тип `Part` (unexported поля; минтинг структурного XML только Builder; exported `Content()` всегда escaped), `Build()` ставит только AgentID/System/User per §6.7, `ValidationFacts()` → minted `<validation_facts>` + `[]PartyValidation` + метрика 1×/present-id.
5. Тесты (acceptance 2/3/4 + fail-path + -race) + per-package CLAUDE.md.

### Архитектурное соответствие
- high-architecture.md §6.7 (только System/User/AgentID), §6.7.1 (mandatory escaping всего user-controlled; не baked-tags/metadata), §6.7.2 (pre-LLM ИНН/ОГРН → validation_facts; lic_party_validation_total{type,valid}).
- ai-agents-pipeline.md §0.2 (контракт агента), §0.3 (5-layer defence, layer 2 = escaping в Prompt Builder), §3 (envelope агента-3).
- **SSOT-resolution:** validation_facts эмитится в форме `<inn_check>/<ogrn_check>` из embedded-промпта агента-3 (party_consistency.txt), НЕ в иллюстративной flat-`<party>` из §6.7.2 — модель читает промпт (code-architect-confirmed).
- code-architect design review: **APPROVE-WITH-MUST-FIX** (5 MF) — учтены полностью (MF-1 эскейпинг-порядок, MF-2 Part-closed-type вместо Raw bool, MF-3 раздельные эскейперы, MF-4 inn/ogrn attr-escape, MF-5 Recorder typed + present-only).
- House-style vs cost/prompts: hermetic, NewTypeName, var _ checks, fail-fast errors, CLAUDE.md, -race, WireStringsPinned — CONFORMS.

### Ревью и правки
- **security-engineer PASS-WITH-FINDINGS:** escaping фундаментально достаточен, нет CRITICAL. HIGH#2 (social mis-attribution через name) / HIGH#3 (all-zeros INN checksum-valid) — НЕ delimiter-bypass; semantic-trust validation_facts → routed downstream. Применены дешёвые in-scope меры: name rune-cap 256 (полное имя сохранено в PartyValidation; капается только model-facing атрибут), safeEntityType closed-set write-site guard (LOW#6). LOW#8 (size-bound) → router/agent-layer.
- **golang-pro APPROVE-WITH-NITS:** багов нет, вся корректность подтверждена (strings.Replacer single-pass — документированный контракт; uint64 overflow-free ≤14 цифр; %11%10 left-assoc; byte-loop безопасен — ASCII-gated). Добавлены invalid-UTF-8 регресс-тест + two-minted-parts тест.
- **code-reviewer CONFORMS:** 7/7 acceptance criteria PASS, все арх-пункты PASS, gaps нет.

### Тесты
- go test ./... зелёный (18 пакетов + promptbuilder); ~55 подтестов promptbuilder PASS включая -race (TestBuilder_Concurrent 16×64); acceptance steps 2/3/4 запинены. make build/lint/test — ok. make docker-build не запускался (нет Docker daemon; Dockerfile не менялся).

### Notes / следующая задача
- **Routed downstream (accepted residual risk, security record):** (1) агент-3 системный промпт / Reporting Engine ДОЛЖНЫ формулировать `valid=true` как «контрольная сумма корректна», НЕ «ИНН подтверждён» (HIGH#3, реальный фикс — будущий ЕГРЮЛ/ЕГРИП-lookup, вне v1); (2) `name` в validation_facts — document-derived, не system-fact (HIGH#2, prompt-side mitigation в LIC-TASK-027 агент-3); (3) total-size guard envelope — на router/agent-layer (LOW#8).
- **Forward (LIC-TASK-047 app-wiring):** Recorder-адаптер над metrics.CrossCutMetrics.PartyValidationTotal (RecordPartyValidation→.WithLabelValues(string(kind), metrics.BoolLabel(valid)).Inc()); per-agent Run (025-033) владеет ПОРЯДКОМ блоков envelope (должен совпадать с системным промптом агента §1-9); агент-3 вставляет ValidationFacts-Part между `<party_roles>` и `<party_details_block>`; system-аргумент из prompts.LoadPrompt(agentID).
- **make docker-build не запускался:** нет Docker daemon (вне test_steps, как во всех прежних LIC-задачах).
- **Открыты (deps done) для следующей итерации:** 003,009,010,021,023,035,038,039,041-046,052. Рекомендация: **LIC-TASK-023** (Schema Validator + Repair Loop — dep=[019,020] done, замыкает вместе с 022 разблокировку 024/BaseAgent — критический путь). Прогресс: **18/55 done**.

---

## LIC-TASK-023 — Schema Validator + Repair Loop (1-shot) [critical] — DONE 2026-05-17

- **Status:** done
- **Completed at:** 2026-05-17
- **Agent:** claude-opus-4-7[1m] (консультации: code-architect — design review; code-reviewer + golang-pro — финальное ревью параллельно)

### Выбор задачи
- done на старте: 18/55 (вкл. 022). Eligible pending (deps done, все critical): 003,009,010,021,023,035,038,039,041-046,052. schemavalidator/ содержал только .gitkeep (прошлая итерация спланировала, но НЕ сохранила код; git clean) → перезапуск.
- Объективная метрика транзитивного разблокирования максимальна у **023**: dep=[019,020] done; замыкает (вместе с 022) разблокировку 024/BaseAgent → каскад 025-033 → 034 → 036/037 → 047. Выбрана **LIC-TASK-023**.

### План реализации
1. Изучены: port/{router,llm,llm_errors}.go, model/{error_codes,errors,status,agent}.go, router/repair.go+CLAUDE.md, cost.go+CLAUDE.md (house-style seam), schemas.go+CLAUDE.md (LoadSchema verbatim; 023 владеет real JSON-Schema lib), metrics/{agent,labels}.go (RepairAttemptsTotal/RepairOutcomeTotal централизованы), error-handling.md §5, high-architecture §6.6/§6.8, observability §3.3, ai-agents-pipeline §retry/repair, 3 адаптера payload.go.
2. code-architect design review → **REJECT→resolved**: MF-1 (PriorTurns: 2-element shorthand §6.8/godoc lossy; верная 3-turn форма + проверка переносимости по 3 адаптерам + фикс stale-godoc), MF-2 (err!=nil⟹repair_provider_error строго дизъюнктно 2nd-violation⟹repair_failed), MF-3 (SchemaCompileError⟹INTERNAL_ERROR, не репейрится), MF-4 (passing primary ⟹ 0 repair-метрик). Q3-Q5/items 7,9 confirmed.
3. Реализация пакета (5 prod-файлов + CLAUDE.md + 4 test-файла), go.mod += gojsonschema v1.2.0, godoc-фиксы port/llm.go+router/repair.go.
4. make build/test/lint + go test -race; финальное ревью code-reviewer + golang-pro (параллельно); применение hardening.

### Прогресс
- ✅ **validator.go**: package-doc (single-non-hermetic-exception инвариант). `Validator`/`NewValidator`/`Validate(schema,content)`. gojsonschema.NewSchema (compile-fail→*SchemaCompileError) ПЕРЕД compiled.Validate (doc-parse-fail→*SchemaViolation "not valid JSON" §5.1; !Valid()→*SchemaViolation с result.Errors()). Stateless/concurrency-safe.
- ✅ **errors.go**: `SchemaViolation` (Errors[] sorted+dedup+trim+sentinel — детерминизм repair-prompt; Pretty()) / `SchemaCompileError` (Unwrap) + `AsSchemaViolation`/`AsSchemaCompileError` (nil-safe errors.As). var _ error checks.
- ✅ **seams.go**: `Metrics` seam (RepairAttempt/RepairOutcome) + noopMetrics; `RepairOutcome` local mirror (repaired_ok|repair_failed|repair_provider_error) + IsValid; адаптер→047.
- ✅ **repair.go**: `repairPromptTemplate` (error-handling.md §5.2 BYTE-EXACT, вкл. hard-wrap «без объяснений и\npreamble»). `Repairer` narrow seam (1 метод = port.CompleteRepair sig). `RepairLoop`/`NewRepairLoop` (fail-fast nil-repairer, nil-metrics→noop). `Run`: switch-init — valid→primary без метрик (MF-4); compile→INTERNAL_ERROR без repair (MF-3); violation→RepairAttempt + 1× CompleteRepair sticky (N=1) → rerr!=nil⟹repair_provider_error+AGENT_OUTPUT_INVALID (MF-2) / repaired valid⟹repaired_ok / 2nd violation⟹repair_failed+AGENT_OUTPUT_INVALID. `buildRepairRequest`: slices.Clone(origPriorTurns)+[{User,origUser},{Assistant,invalid}], User=repair_prompt, Temperature=0.0 (delta ровно {PriorTurns,User,Temperature} §5.3). LOW-3-hardening !ok-guard.
- ✅ **CLAUDE.md**: все решения с атрибуцией; MF-1 SSOT-реконсиляция; single-exception confinement; forward 024/047.
- ✅ **godoc-фиксы (MF-1)**: port/llm.go Turn + router/repair.go CompleteRepair — стале 2-element shorthand заменён на актуальную 3-turn конструкцию (comment-only, без trailing WS, gofmt-clean).
- ✅ Тесты: 22 функции + 6 подтестов (validator/repair/seams/errors + acceptance steps 1-4 + MF-1..4 + N=1 + delta + verbatim-prompt pin + wire-string pin vs metrics/labels.go + TestSingleThirdPartyImport import-guard + TestGofmtClean go/format self-check + -race ×16). go test ./... 0 FAIL; -race clean; go vet чисто; make build/test/lint зелёные; bin gitignored.

### Финальное ревью (применённые фиксы)
- **code-architect:** REJECT→resolved — MF-1..MF-4 реализованы и независимо подтверждены.
- **code-reviewer:** APPROVE — 0 CRITICAL/0 HIGH. MF-1..4 «genuinely (not nominally) satisfied». LOW-1 (raw json-err text в prompt — bounded, тот же провайдер, §5.2 «не цитируй ошибки»; вне scope), LOW-2 (trailing-garbage после валидного JSON принимается — поведение gojsonschema v1.2.0, вне §5.1-триггеров; awareness для LIC-TASK-024), LOW-3 (default-ветка !ok-guard) — **LOW-3 применён**.
- **golang-pro:** APPROVE — gojsonschema v1.2.0 API сверен по исходникам (после успешного NewSchema ошибка compiled.Validate ⟹ только document-parse fail — маппинг точен); slices.Clone Go1.26.1 гарантированно без aliasing caller-слайса; format-string безопасен (validationErrors — Sprintf ARG, не format); TestGofmtClean sound. nit-3 = LOW-3 — применён.

### Соответствие архитектуре
- error-handling.md §5.1 (триггеры: не-JSON / схема) §5.2 (repair-prompt byte-exact) §5.3 (тот же model/tokens, Temperature 0.0, delta) §5.4 (N=1) §5.5 (метрики) — ✅.
- high-architecture.md §6.6 шаги 4-6 / §6.8 (sticky used_provider, repair через PriorTurns, provider-error→immediate AGENT_OUTPUT_INVALID без fallback, 2nd→AGENT_OUTPUT_INVALID is_retryable=true) — ✅. MF-1 SSOT-реконсиляция §5.2-проза vs §6.8-shorthand (как 019 §6.1 / 022 validation_facts).
- observability.md §3.3 / error-handling §5.5: lic_agent_repair_attempts_total{agent,provider} + lic_agent_repair_outcome_total{agent,provider,outcome∈repaired_ok|repair_failed|repair_provider_error}; provider=used_provider sticky; typed-label cardinality (9×3×3=81) — ✅ (за seam).
- house-style (hermetic кроме осознанного gojsonschema-exception confined тестом; NewTypeName; var _ checks; fail-fast; CLAUDE.md; -race; WireStringsPinned; metrics-seam+noop→047) — CONFORMS.

### Summary
Production-ready Schema Validator + 1-shot Repair Loop. Соответствует §5/§6.6/§6.8 + acceptance LIC-TASK-023. Единственный осознанно non-hermetic internal/* пакет (xeipuuv/gojsonschema v1.2.0), confined import-set guard-тестом; телеметрия инвертирована за Metrics-seam (адаптер→047). **Разблокирует LIC-TASK-024 (BaseAgent runner — шаги 4-6 уже инкапсулированы в RepairLoop.Run) → весь 9-agent pipeline (025-033) → 034 → 036/037 → 047.**

### Notes / следующая задача
- **Forward (LIC-TASK-024 BaseAgent):** RepairLoop.Run = шаги 4-6 agent loop. Caller владеет stage (STAGE_AGENT_*), schema (schemas.LoadSchema(agentID)), originalReq (Prompt Builder 022), primary (router.Complete). nil err → response для aggregate; *DomainError (AGENT_OUTPUT_INVALID|INTERNAL_ERROR, оба is_retryable=true) → propagate в errgroup. LOW-2: если когда-либо нужна strict trailing-byte rejection — на BaseAgent/контент-pre-trim, НЕ в schemavalidator (вне §5.1).
- **Forward (LIC-TASK-047 app-wiring):** (1) Metrics-адаптер над *metrics.AgentMetrics: RepairAttempt→RepairAttemptsTotal.WithLabelValues(agent,provider).Inc(); RepairOutcome→RepairOutcomeTotal.WithLabelValues(agent,provider,outcome).Inc() (outcome — только экспортируемые RepairOutcome-константы .String()). (2) var _ Repairer = (port.ProviderRouterPort)(nil) в wiring (НЕ в пакете — иначе router-import ломает seam). (3) schemas.Validate() ОБЯЗАН быть fatal startup-check (SchemaCompileError в Run — defence-in-depth, в prod недостижим).
- **DOC FOLLOW-UP (для архитектора):** error-handling.md §6.8 / (исходные) port.Turn godoc давали lossy 2-element shorthand PriorTurns; исправлено в port/llm.go+router/repair.go в этой задаче. tasks.json acceptance-парафраз repair-prompt отличается от error-handling.md §5.2 — реализован §5.2 verbatim (SSOT). Рекомендуется привести §6.8 к 3-turn форме при следующей правке доков.
- **make docker-build не запускался:** нет Docker daemon (вне test_steps, как во всех прежних LIC-задачах; Dockerfile не менялся, новый go.mod-dep подхватывается go build в multi-stage).
- **Открыты (deps done) для следующей итерации:** 003,009,010,021,024,035,038,039,041-046,052. Рекомендация: **LIC-TASK-024** (BaseAgent runner — теперь разблокирован 022+023; прямой критический путь к 9-agent pipeline) либо параллельно-независимые 021/035/038/039. Прогресс: **19/55 done**.

## LIC-TASK-024 — BaseAgent runner (общий Run() workflow для всех 9 агентов) [critical] — DONE 2026-05-17

- **Status:** done
- **Completed at:** 2026-05-17
- **Agent:** claude-opus-4-7[1m] (консультации: code-architect — design review; code-reviewer + golang-pro — финальное ревью параллельно; code-reviewer — подтверждение устранения замечаний)

### Выбор задачи
- done на старте: 19/55 (вкл. 023). git HEAD=0df2ed4 чистый. base/ + tokenestimator/ содержали только .gitkeep.
- Eligible pending READY (deps done): 003,009,010,021,024,035,038,039,041-046,052. Максимум транзитивного разблокирования у **024**: dep=[022,023] done; бутылочное горлышко — разблокирует каскад 025-033 (9 агентов) → 034 (Stage Executor) → 036/037 (Pipeline Orchestrator/Pause-Resume) → 047 (wiring). Выбрана **LIC-TASK-024**.

### План реализации
1. Изучены: port/{agents,router,llm,llm_errors}.go, model/{agent,agent_input,error_codes,errors,status}.go, promptbuilder(022)+CLAUDE, schemavalidator(023: validator/repair/seams/errors)+CLAUDE («Forward req LIC-TASK-024»), prompts/schemas (LoadPrompt/LoadSchema), router(019)+CLAUDE+seams, metrics/{agent,labels}.go, tracer/{tracer,attrs,span}.go, config/agents.go, high-architecture §6.6/§6.8/§6.7, observability §3.3/§4.2/§4.3, ai-agents-pipeline §0.6+«Бюджеты и параметры LLM».
2. code-architect design review → 6 MUST-FIX + S1/S3/S4/S5/N1/N3 (S2 отклонён по feedback_constructors.md).
3. Реализация пакета (base.go+seams.go+CLAUDE.md + 3 test-файла), без новых go.mod-deps.
4. make build/test/lint + go test -race; финальное ревью code-reviewer+golang-pro; применение C1/H1/H2/H3/M/L; re-review подтверждение.

### Прогресс
- ✅ **seams.go**: `Outcome` local-mirror metrics.AgentInvocationOutcome (success|repair_success|invalid_output|provider_error|timeout, §3.3 SSOT closed enum) + IsValid; `Metrics` seam (Invocation/Duration/InputTokens/OutputTokens)+noop; `TokenEstimator` seam Fit(req)->(est,overBudget) **БЕЗ мутации req** (MF-3) + passthroughEstimator ⌈runes/4⌉; `Tracer`/`AgentSpan`/`LLMSpan` 2-span seam (parent lic.agent.<name> + child lic.llm.call, §4.2) + noop; Correlation/AgentSpanInput/AgentSpanOutput/LLMSpanOutput контейнеры.
- ✅ **base.go**: package-doc (hermeticity+concurrency инварианты). `Config`(AgentID/Stage/System/Schema/Model/MaxTokens/Temperature/Timeout), `Spec`(Parts(*Builder,AgentInput)->[]Part + Decode([]byte)->AgentResult; stateless-контракт в godoc), `Deps`+withDefaults (router.Deps-паттерн), `canonicalStage` AgentID→STAGE_AGENT_* bijection (N3 cross-check), `BaseAgent`, `var _ port.Agent`. `NewBaseAgent` fail-fast (nil spec/router, !IsValid agent/stage, stage-mismatch, empty system/schema/model, MaxTokens/Temp/Timeout) + строит RepairLoop тем же router-instance (sticky, S1 propagate err) + Validator. `Run`: span-2-уровня → spec.Parts/Build (MF-6 не глотать→INTERNAL_ERROR) → set Model/MaxTokens/Temperature/JSONSchema/JSONMode → estimator.Fit (overBudget→AGENT_INPUT_TOO_LARGE) → WithTimeout → router.Complete (classifyCompleteError) → validator.Validate: nil⇒success без RepairLoop (MF-4) / иначе⇒repair.Run → spec.Decode. Единственный defer-сайт эмиссии (MF-5): Invocation 1×, Duration всегда, InputTokens только fitRan, OutputTokens только success|repair_success, child-span finish перед parent. `classifyCompleteError` (S4: cctx.Err()==DeadlineExceeded решающий первый; H2 parent-cancel→AGENT_TIMEOUT documented lossy), `classifyRepairError`->(Outcome,turnIssued) (MF-2 explicit AsSchemaViolation/AsLLMProviderError; C1 ground-truth repair_attempts).
- ✅ **CLAUDE.md**: все решения с атрибуцией (MF-1..6, S2-отклонение, S4, H2, C1, M1/M2, forward 025-033/047).
- ✅ Тесты: 22 функции (base_test: success/timeout/timeout-wrapped/repair_success/repair_failed/repair_provider_error/all-providers-failed/context-too-long/build-defect×2/over-budget/fail-fast-table/all-agents/port.Agent/concurrent-race/classify/agent_id; seams_test: Outcome-pin vs metrics SSOT/passthrough/noop; internal_test: hermetic-import-guard/gofmt-self-check/Spec-shape). go test ./... 0 FAIL; -race clean; go vet чисто; make build/test/lint зелёные; .gitkeep git rm (прецедент prompts/schemas).

### Финальное ревью (применённые фиксы)
- **code-architect:** 6 MUST-FIX + S1/S3/S4/S5/N1/N3 — все применены; S2 (base.New) **отклонён** обоснованно (feedback_constructors.md явно требует NewTypeName; codebase: NewProviderRouter/NewRepairLoop/NewBuilder).
- **golang-pro:** **APPROVE** без блокеров — named-return+defer-LIFO корректны на всех путях; defer cancel() после телеметрии (child→parent close order ок); interface-to-interface assignability port.ProviderRouterPort→schemavalidator.Repairer валидна; errors.As traversal через *DomainError.Unwrap→Cause корректен, classifyRepairError disjoint; cctx.Err() семантика верна; Fit by-value без observable-aliasing в shipped-коде; concurrency-safe.
- **code-reviewer:** 1 CRITICAL + 3 HIGH + M/L → все устранены, **re-review APPROVED**: **C1** repair_attempts из ground-truth (classifyRepairError->(Outcome,turnIssued), не pre-guess; span-атрибут структурно не может разойтись с lic_agent_repair_attempts_total — оба после RepairLoop rl.metrics.RepairAttempt); **H1** LLMSpanOutput godoc one-logical-span + тест primary-фигур на child при repair_provider_error; **H2** parent-cancel→AGENT_TIMEOUT documented deliberate lossy (retryable верен per ASSUMPTION-LIC-19); **H3** TestRun_Timeout_WrappedProviderError (S4 end-to-end), span InputTokens/LatencyMs, MF-2 span-truth pin; **M1/M2/M3/L1/L2**.

### Соответствие архитектуре
- high-architecture.md §6.6 (Run шаги 1-6: PromptBuilder→TokenEstimator→Complete→Validator→RepairLoop→escalate) §6.8 (sticky used_provider через RepairLoop 023) — ✅. §6.7 head/tail-усечение EXTRACTED_TEXT — корректно вынесено upstream (per-artifact, LIC-TASK-021), Fit БЕЗ мутации envelope (MF-3, защита prompt-injection layer-2).
- observability.md §3.3 lic_agent_invocations_total{agent,outcome∈5} + duration/input_tokens/output_tokens (за seam, typed-label 9×5=45); §4.2 2-span иерархия parent lic.agent.<name> ⊃ child lic.llm.call; §4.3 split lic.agent.* vs lic.llm.* (cost_usd/cached/fallback_used forward-defer) — ✅.
- error-handling.md §5: RepairLoop переиспользован (не дублирован) — §5.2 byte-exact prompt/§5.3 delta/§5.4 N=1/§5.5 метрики остаются в 023. Catalog-коды (AGENT_TIMEOUT/AGENT_OUTPUT_INVALID/AGENT_INPUT_TOO_LARGE/LLM_*/INTERNAL_ERROR) через NewDomainError, Stage=cfg.Stage, agent_id на всех (N1) — ✅.
- house-style (hermetic stdlib+domain+sibling-leaf; NewTypeName; var _ checks; fail-fast; Deps.withDefaults; CLAUDE.md; -race; WireStringsPinned; seam+noop→047; gofmt self-check) — CONFORMS.

### Summary
Production-ready BaseAgent runner — реализует port.Agent, единый §6.6 Run()-loop. Хермётичен (stdlib+internal/domain+promptbuilder+schemavalidator; gojsonschema транзитивно, но re-exposed только через error — base без third-party surface); телеметрия за Metrics/Tracer(2-span)/TokenEstimator seam+noop → LIC-TASK-047. 6 MUST-FIX + C1 (ground-truth repair_attempts) + H1/H2/H3 устранены, golang-pro+code-reviewer APPROVED. **Разблокирует LIC-TASK-025..033 (9 агентов: каждый = Spec+Config+встроенный *BaseAgent) → 034 (Stage Executor) → 036/037 (Pipeline) → 047 (wiring).**

### Notes / следующая задача
- **Forward (LIC-TASK-025..033, 9 агентов):** реализовать `Spec`: `Parts` строит точный envelope агента (порядок блоков = system-prompt агента; user-контент через promptbuilder.Content; агент 3 — b.ValidationFacts между <party_roles> и <party_details_block>), `Decode` — в конкретный model.* тип. Spec ОБЯЗАН быть stateless (общий *BaseAgent в errgroup). Config: System=prompts.LoadPrompt, Schema=schemas.LoadSchema, Model/MaxTokens/Temperature из ai-agents-pipeline «Бюджеты и параметры LLM», Timeout=config.AgentsConfig.Timeouts, Stage=canonicalStage[AgentID]. Встроить *BaseAgent ⇒ тип удовлетворяет port.Agent для Stage Executor (034) бесплатно.
- **Forward (LIC-TASK-047 app-wiring):** (1) Metrics-адаптер над *metrics.AgentMetrics (Invocation/Duration/InputTokens/OutputTokens; outcome — только Outcome-константы .String(), запинено TestOutcome_WireStringsPinned). (2) Tracer-адаптер над *tracer.Tracer: StartAgent→StartSpanWithFields(ctx,"lic.agent."+basename,SpanFields,AttrAgentID); StartLLMCall→child "lic.llm.call"; LLMSpan.Finish→AttrLLMProvider/Model/Input/Output/LatencyMS; AgentSpan.Finish→AttrAgentOutcome/RepairAttempts + tracer.RecordError(span,err) (несёт un-lossy INTERNAL_ERROR — MF-2) + End(). (3) RepairMetrics (schemavalidator.Metrics) адаптер. (4) реальный TokenEstimator (LIC-TASK-021) в Deps.Estimator — per-artifact head/tail upstream в Spec.Parts; Fit только est+overBudget verdict.
- **Forward (LIC-TASK-021 Token Estimator):** Fit-контракт — оценка + overBudget, БЕЗ мутации req (envelope-corruption type-impossible); head/tail-усечение EXTRACTED_TEXT — на уровне model.AgentInput artifacts ДО spec.Parts, не на собранном envelope.
- **make docker-build не запускался:** нет Docker daemon (вне test_steps, как во всех прежних LIC-задачах; Dockerfile/go.mod не менялись).
- **Открыты (deps done) для следующей итерации:** 003,009,010,021,025-033,035,038,039,041-046,052. Рекомендация: каскад **LIC-TASK-025..033** (9 агентов, теперь разблокированы 024 — прямой критический путь к Stage Executor/034 → Pipeline/036) либо параллельно-независимые 021/035/038/039. Прогресс: **20/55 done**.

---

## LIC-TASK-025 — Agent 1: Contract Type Classifier [critical] — DONE 2026-05-17

### Выбор задачи
Из 35 pending выбрана LIC-TASK-025 (critical, dep 024=done). Бутылочное горлышко критического пути: 9 агентов (025-033) — ВСЕ нужны для 034 (Stage Executor) → 036/037 (Pipeline) → 047 (wiring). Numeric order ⇒ старт с Agent 1. Это **template для агентов 2-9**.

### План
1. Изучить контракты: base{base.go,seams.go,CLAUDE.md}, port.Agent/AgentResult, model.{ClassificationResult,ContractType(12 whitelist),AgentInput,AgentID,Stage}, promptbuilder.{Builder,Part,Content,Build}, prompts.LoadPrompt, schemas.LoadSchema, type_classifier.{txt,json}, ai-agents-pipeline §1, config.agents, DP-артефакты ExtractedText/DocumentStructure, router→provider.chooseModel.
2. code-architect design review (5 cross-cutting Q + MF/SHOULD/NICE) → SSOT.
3. Реализация пакета (classifier.go+spec.go+CLAUDE.md + 2 test-файла), без новых go.mod-deps.
4. make build/test/lint + go test -race; ревью golang-pro+code-reviewer; применить hardening.

### Прогресс
- ✅ **classifier.go**: package-doc (hermeticity invariant — no internal/config / no document-processing). Консты §1-бюджета `maxOutputTokens=400`/`temperature=0.0` (NICE: §0.6 говорит 200 — §1 per-agent таблица binding). `Classifier` встраивает `*base.BaseAgent`; `var _ port.Agent = (*Classifier)(nil)`. `NewClassifier(modelID, timeout, base.Deps)`: prompts.LoadPrompt/schemas.LoadSchema (fail-fast %w), base.Config (AgentID=AgentTypeClassifier, Stage=StageAgentTypeClassifier), делегирует base.NewBaseAgent. Godoc-forward-note про base/router Model-defect (Q1).
- ✅ **spec.go**: консты `headRunes=4000`/`tailRunes=1000`/`elision` (рун-safe компакт, sep-обоснование). Локальные minimal-decode `extractedText{pages[].text}` (fullText() byte-identical DP.FullText) / `documentStructure{sections[].title}` (titles() trim+skip-empty join). `compact` (≤5000 verbatim, иначе head+elision+tail рунами). `classifierSpec struct{}` (stateless, value-receiver) + `var _ base.Spec`. `Parts`: 2 экранированных Content (document_structure→contract_document, порядок=промпт); EXTRACTED_TEXT mandatory (absent/malformed/empty→err), DOCUMENT_STRUCTURE mandatory-in-shape (absent/malformed→err) но 0 sections OK. `Decode`: json.Unmarshal→*model.ClassificationResult + re-check ContractType.IsValid() primary+alternatives (drift-guard).
- ✅ **CLAUDE.md**: все решения с атрибуцией (Q1-Q5, MF/SHOULD/NICE), 3 forward-notes (base/router Model-defect; upstream DM-gate→DM_ARTIFACTS_MISSING; shared artifacts-decoder steward для 026).
- ✅ Тесты: 14 функций (classifier_test: NewClassifier-OK/fail-fast-table; Parts-envelope+injection-escaping(positive+negative); compaction-reaches-envelope(end-to-end >5000 рун); compact-rune-safe(threshold/+1/Cyrillic); Parts-errors-table(7: absent/malformed/empty/pages-null/wrong-type/no-struct/malformed-struct + tolerated sections null|[]|blank); Decode(valid/full-round-trip/3 bad); Run-integration-valid-envelope(Шаг 2: budget params/JSONSchema/JSONMode); Run-invalid-type-repair-triggered(Шаг 3: реальная embedded-схема→sticky repair); concurrent-race; internal_test: hermetic-import-guard + gofmt-self-check). go test ./... 0 FAIL (21 пакет ok); -race clean; go vet чисто; make build/test/lint зелёные; .gitkeep git rm (прецедент prompts/schemas).

### Финальное ревью (применённые фиксы)
- **code-architect (SSOT):** Q1 Config.Model=injected resolved primary (НЕ литерал) + forward-note base/router defect (base.Run req.Model=cfg.Model безусловно; router без изменений; provider.chooseModel переопределяет env-pinned default ⇒ fallback не-primary получает primary id, нарушает ADR-LIC-03; настоящий fix — LIC-TASK-024/router/047). Q2 head/tail=Spec.Parts fixed-char, руны, sep, ≤5000 verbatim. Q3 2 Content (inner <sections> иллюстративен — ratified promptbuilder precedent). Q4 EXTRACTED_TEXT mandatory / DOCUMENT_STRUCTURE mandatory-in-shape но 0 sections OK; forward-note upstream DM-gate (034). Q5 no internal/config (router.RouterConfig precedent); Decode re-check IsValid(). Все MF/SHOULD/NICE применены.
- **golang-pro:** **APPROVE** без блокеров — rune-boundaries корректны на всех edge; fullText() byte-identical DP; classifierSpec stateless/concurrent-safe; %w-wrapping верно; embed без nil-риска; NewClassifier(base.Deps) — правильный выбор (YAGNI vs options). F1 (compact strings.Builder) — необязательная микро-опт, `+`-форма яснее, оставлено. F2/F3 (test-hardening pages null/wrong-type/sections null) — применены.
- **code-reviewer:** **APPROVED**, 0 CRITICAL / 0 HIGH. Применены **MEDIUM-1** (full-struct Decode round-trip: rationale/alternatives.confidence/prompt_injection_detected=true), **MEDIUM-2** (compaction-branch end-to-end envelope assertion). LOW-1 (schema confidence-bound — делегировано schemavalidator suite, корректно). LOW-2 (shared artifacts-decoder steward) → forward-note для LIC-TASK-026. LOW-3/4 — traced, holds, no-op.

### Соответствие архитектуре
- ai-agents-pipeline.md §1: вход EXTRACTED_TEXT head4000+tail1000 + DOCUMENT_STRUCTURE.sections[].title; конверт <input><document_structure><contract_document>; бюджет Claude-sonnet/temp0.0/max400/timeout5s; выход ClassificationResult{contract_type∈12, confidence∈[0,1], alternatives[], rationale, prompt_injection_detected} — ✅ (порядок блоков=системный промпт; schema-validation встроена base; repair через реальную embedded-схему пинуется тестом).
- §6.6 Run-loop / §6.7 prompt-injection layer-2 (все user-байты через promptbuilder.Content escaped; planted </contract_document> экранируется — positive+negative assertion) — ✅.
- house-style (hermetic stdlib+domain+sibling-leaf+prompts/schemas; NewTypeName; var _ port.Agent/base.Spec; fail-fast пробрасывается; CLAUDE.md; -race; TestHermeticImports allowlist; gofmt self-check) — CONFORMS.

### Summary
Production-ready Agent 1 — тонкая обёртка над BaseAgent: Classifier{*base.BaseAgent} ⇒ port.Agent бесплатно; единственная per-agent логика — stateless classifierSpec (Parts §1-конверт + Decode типизированный+drift-guard). Хермётичен (НЕТ internal/config/document-processing). golang-pro APPROVE + code-reviewer APPROVED (0 CRITICAL/HIGH); MEDIUM-1/2 + golang-pro F2/F3 hardening применены. **Задаёт template для агентов 2-9 (026-033).**

### Notes / следующая задача
- **Forward (LIC-TASK-026..033, 8 агентов):** копировать паттерн (thin *base.BaseAgent embed + NewTypeName(modelID,timeout,base.Deps) + stateless empty-struct Spec + локальные minimal-artifact-decode + Content-only Parts в порядке промпта + Decode enum/whitelist drift-guard + TestHermeticImports/TestGofmtClean). Агент 3 (027) — вставить b.ValidationFacts между <party_roles> и <party_details_block>. Каждый агент: System=prompts.LoadPrompt, Schema=schemas.LoadSchema, Model/MaxTokens/Temperature из ai-agents-pipeline §N «Бюджеты», Timeout=config.AgentsConfig.Timeouts (через wiring), Stage=canonicalStage[AgentID].
- **Forward (LIC-TASK-026 решение):** оценить вынос shared `internal/agents/artifacts` (hermetic, stdlib+domain) с ОДНИМ FullText-mirror, запинённым тестом к DP-семантике, чтобы «byte-identical to DP» утверждалось 1×, а не by-comment 9× (code-reviewer LOW-2).
- **Forward (LIC-TASK-024/router/047):** base.Run пишет req.Model=cfg.Model безусловно; правильный fix — base оставляет req.Model=="" (каждый адаптер chooseModel→свой env-pinned default) ЛИБО router переписывает req.Model per-provider. До фикса fallback-correctness — known limitation.
- **Forward (LIC-TASK-034 Stage Executor):** artifact-bundle gate ОБЯЗАН отбраковывать missing/empty mandatory EXTRACTED_TEXT/DOCUMENT_STRUCTURE как DM_ARTIFACTS_MISSING (retryable) ДО запуска Agent 1, чтобы Parts-err→INTERNAL_ERROR был только истинным LIC-invariant-breach.
- **make docker-build не запускался:** нет Docker daemon (вне test_steps, как во всех прежних LIC-задачах; Dockerfile/go.mod не менялись).
- **Прогресс: 21/55 done.** Открыты (deps done): 003,009,010,021,026-033,035,038,039,041-046,052. Рекомендация: продолжить каскад **LIC-TASK-026** (Agent 2 Key Params) — прямой критический путь, template готов.

---

## LIC-TASK-026 — Agent 2: Key Parameters Extractor [critical] — DONE 2026-05-17

### Выбор задачи
Из 34 pending выбрана LIC-TASK-026 (critical, dep 024=done). Прямой критический путь: 9 агентов (025-033) → 034 Stage Executor → 036/037 Pipeline → 047 wiring. 025 (Agent 1) done ⇒ numeric order ⇒ Agent 2. НЕ блокируется 021 (base использует seam TokenEstimator с passthrough-дефолтом; реальный estimator подключается в 047). Template = typeclassifier (025).

### План
1. Изучить: base.go/CLAUDE.md (MF-3: >80K head/tail = upstream-задача 021, НЕ Spec.Parts), typeclassifier spec.go/тесты (шаблон), model.{KeyParameters,KeyParametersInternalExtras,PartyRole,KeyDate,PartyRoleType(9 enum),AgentInput}, key_params.{txt(envelope <semantic_tree>→<contract_document>),json}, ai-agents-pipeline §2, configuration.md (LIC_AGENT_KEY_PARAMS_*), promptbuilder.Content/Build, DP model.ExtractedText.FullText()/SemanticTree wire-shape.
2. code-architect design review (5 cross-cutting Q: shared artifacts-helper / SEMANTIC_TREE passthrough / EXTRACTED_TEXT compaction / artifact strictness / Decode drift-guard).
3. Реализация: новый hermetic pkg internal/agents/artifacts + pkg internal/agents/keyparams (keyparams.go+spec.go+CLAUDE.md + internal_test + keyparams_test); artifacts.go+test+CLAUDE.md. Без новых go.mod-deps.
4. make build/test/lint + go test -race; соответствие архитектуре; tasks.json/progress.md/session.log; git commit.

### Прогресс
- ✅ **internal/agents/artifacts** (НОВЫЙ shared hermetic pkg — резолв typeclassifier forward-note #3 / code-reviewer LOW-2): `ExtractedText{Pages[].Text}` + `FullText()` byte-faithful mirror DP.model.ExtractedText.FullText() (0→"", 1→verbatim, N→join '\n'). Stdlib-only (строжайший allowlist в agents-слое — НЕ импортит даже internal/domain). `TestFullText_DPSemantics` — ЕДИНСТВЕННОЕ утверждение «byte-identical to DP» для всего agents-слоя (3 кейса + no-trim + empty-interior-page); `TestExtractedText_DecodesDPWireShape` (DP wire JSON / pages:null / wrong-type→err). internal_test: hermetic(stdlib-only) + gofmt self-check. Agent 1 СОЗНАТЕЛЬНО не рефакторен (re-touch reviewed DONE = scope-creep, риск регресса pinned-тестов) — duplicate записан как forward-cleanup.
- ✅ **internal/agents/keyparams** (Agent 2): `Extractor{*base.BaseAgent}` ⇒ port.Agent; `var _ port.Agent`. `NewExtractor(modelID,timeout,base.Deps)`: prompts.LoadPrompt/schemas.LoadSchema(AgentKeyParams) fail-fast %w, base.Config (AgentID=AgentKeyParams, Stage=StageAgentKeyParams), делегирует base.NewBaseAgent. §2-бюджет консты `maxOutputTokens=2000`/`temperature=0.0` (provider/timeout — wiring's job через base.Deps/ctor-param, hermetic Agent-1 precedent). Godoc forward-note base/router Model-defect (общий с typeclassifier #1).
- ✅ **spec.go**: `extractorSpec struct{}` (stateless, value-receiver) + `var _ base.Spec`. `Parts`: конверт `Content("semantic_tree", string(rawTree))` → `Content("contract_document", fullText)` — порядок ИНВЕРСНЫЙ Agent 1 (semantic_tree ПЕРВЫЙ, key_params.txt:35-40). SEMANTIC_TREE = byte-faithful passthrough сырых байт (НЕ decode/re-encode — нужны node.id для clause_ref), гейт = json.Valid. EXTRACTED_TEXT = artifacts.FullText() ПОЛНЫЙ, БЕЗ компакции (>80K = upstream 021, base MF-3). Строгость: SEMANTIC_TREE mandatory (absent/empty/!json.Valid→err), empty-but-valid дерево ТОЛЕРИРУЕТСЯ; EXTRACTED_TEXT mandatory (absent/malformed/empty→err). 2 forward-notes (LIC-TASK-021 — head/tail upstream; LIC-TASK-034 — DM_ARTIFACTS_MISSING gate). `Decode`: json.Unmarshal→*model.KeyParameters + drift-guard PartyRoleType.IsValid() (ЕДИНСТВЕННЫЙ enum-surface; nil-guard internal_extras = absent валиден). INN/OGRN AS-IS, без checksum (Agent 3's job).
- ✅ **CLAUDE.md** обоих пакетов: все решения с атрибуцией (Q1-Q5 code-architect), forward-notes. typeclassifier/CLAUDE.md forward-note #3 аннотирован «RESOLVED by LIC-TASK-026» (doc-only, без code/test-изменений DONE-задачи).
- ✅ Тесты: artifacts (2 функц + internal_test) + keyparams (11 функц: NewExtractor-OK/fail-fast-table; envelope-order+layer2-escaping(raw-literal — НЕ json.Marshal, т.к. json HTML-эскейпит <); tree-passthrough-byte-faithful(реалистичный DP json.Marshal путь); full-text-no-compaction(100k рун verbatim, без elision); Parts-errors-table(8) + tolerated empty-tree(3); Decode(full round-trip / INN-OGRN as-is incl invalid-checksum / nil internal_extras / role-drift / malformed); Run-integration(Шаг 1/2: budget/JSONSchema/JSONMode + INN preserved); Run-invalid-role-repair-triggered(реальная embedded-схема enum→sticky repair); concurrent-race). `go test ./...` 0 FAIL (24 пакета ok); -race clean; `go vet` чисто; `make build/lint/test` зелёные; .gitkeep git rm (прецедент prompts/schemas).

### Финальное ревью (subagent)
- **code-architect (design SSOT, 5 Q):** Q1 создать hermetic internal/agents/artifacts, потреблять ТОЛЬКО из Agent 2, Agent 1 НЕ рефакторить (scope-creep), forward-cleanup записан. Q2 SEMANTIC_TREE byte-faithful passthrough + json.Valid (decode/re-encode вернул бы divergence + потерял node.id). Q3 EXTRACTED_TEXT полный, БЕЗ Agent-2-компакции (80K = upstream 021, base MF-3; иначе 2-й divergent truncation-site). Q4 строгость как Agent-1; empty-but-valid дерево толерируется (гейт = well-formedness, не richness). Q5 role — единственный drift-surface; nil-guard internal_extras; INN/OGRN без guard (Agent 3). Cross-cutting: envelope-order инверсный (high-impact), passthrough всё равно через Content (escape), prompt_injection_detected — model-reported (не guard'ить), nil vs empty internal_extras (минимальный валидный ответ). ВСЕ применены.

### Соответствие архитектуре
- ai-agents-pipeline.md §2: вход SEMANTIC_TREE(полностью) + EXTRACTED_TEXT(полностью, усечение upstream); конверт <input><semantic_tree><contract_document>; бюджет Claude-sonnet/temp0.0/max2000/timeout8s; выход KeyParameters{parties,subject,price,duration,penalties,jurisdiction,internal_extras{applicable_law,termination,acceptance_procedure,party_roles[],key_dates[]},prompt_injection_detected}; INN/OGRN as-is — ✅ (порядок=системный промпт; schema-validation+repair встроены base, пинуется реальной embedded-схемой).
- base/seams.go MF-3 / §6.6 Run-loop / §6.7 prompt-injection layer-2 (все user-байты через promptbuilder.Content escaped; planted </semantic_tree>/</contract_document> экранируется) — ✅.
- configuration.md (LIC_AGENT_KEY_PARAMS_TIMEOUT=8s) — ✅ (wiring's job, ctor-param — hermetic Agent-1 precedent).
- house-style (hermetic; NewTypeName; var _ port.Agent/base.Spec; fail-fast %w; CLAUDE.md; -race; TestHermeticImports allowlist; gofmt self-check) — CONFORMS.

### Summary
Production-ready Agent 2 — тонкая обёртка над BaseAgent: Extractor{*base.BaseAgent} ⇒ port.Agent; единственная per-agent логика — stateless extractorSpec (Parts §2-конверт + Decode типизированный+role-drift-guard). Принято steward-решение: создан shared hermetic internal/agents/artifacts (ОДИН DP-faithful FullText-mirror, запинён 1 тестом — «byte-identical to DP» утверждается 1×, не by-comment 9×); Agent 1 сознательно не тронут. Design reviewed by subagent code-architect (5 cross-cutting Q, все применены). 24 пакета ok, 0 FAIL, -race clean, vet чисто.

### Notes / следующая задача
- **Forward (LIC-TASK-027..033):** template Agent 2 закрепляет паттерн; consuming EXTRACTED_TEXT агенты ДОЛЖНЫ переиспользовать artifacts.ExtractedText (не локальную копию). Агент 3 (027) — вставить b.ValidationFacts между <party_roles> и <party_details_block>.
- **Forward (LIC-TASK-021):** Agent 2 сознательно эмитит ПОЛНЫЙ EXTRACTED_TEXT; правило >80K head/tail — в upstream artifact-bundle prep (model.AgentInput), НЕ в Spec.Parts (base MF-3).
- **Forward (later cleanup):** мигрировать typeclassifier с локальных extractedText/fullText на internal/agents/artifacts, удалить дубликат, держать envelope/compaction-тесты Agent 1 зелёными.
- **Forward (LIC-TASK-034 Stage Executor):** artifact-bundle gate ОБЯЗАН отбраковывать missing/empty mandatory SEMANTIC_TREE/EXTRACTED_TEXT как DM_ARTIFACTS_MISSING (retryable) ДО Agent 2.
- **make docker-build не запускался:** нет Dockerfile (= pending LIC-TASK-003) + нет Docker daemon (вне scope; go.mod не менялся).
- **Прогресс: 22/55 done.** Открыты (deps done): 003,009,010,021,027-033,035,038,039,041-046,052. Рекомендация: продолжить каскад **LIC-TASK-027** (Agent 3 Party Data Consistency) — прямой критический путь, template готов (Agent 3 — единственный с b.ValidationFacts-блоком).

---

## LIC-TASK-027 — Agent 3: Party Data Consistency [critical] — DONE 2026-05-17

### Выбор задачи
Из 33 pending выбрана LIC-TASK-027 (critical, dep 024=done). Прямой критический путь: 9 агентов (025-033) → 034 Stage Executor → 036/037 Pipeline → 047 wiring. 025/026 done ⇒ numeric order ⇒ Agent 3. Template = keyparams/typeclassifier; Agent 3 — единственный агент со структурным `b.ValidationFacts`-блоком (base.Spec godoc / promptbuilder forward-req #2).

### План
1. Изучить: keyparams/typeclassifier (template — strictness/Decode-guard/hermetic-тесты), base.{Spec,Config,Deps}, promptbuilder.{ValidationFacts,Party,PartyValidation,Content}, artifacts.ExtractedText, model.{PartyConsistencyFindings,PartyFinding,PartyFindingType,KeyParameters,PartyRole,RiskLevel,AgentInput}, party_consistency.{txt,json}, ai-agents-pipeline §3 (envelope party_roles→validation_facts→party_details_block→contract_document; budget max1000/temp0.0/timeout6s), DP model.DocumentStructure.PartyDetails wire-shape.
2. code-architect design review (5 Q: party_roles source&strictness / rendering+DOCUMENT_STRUCTURE decode location / EXTRACTED_TEXT compaction / validation_facts minting / Decode drift-guard).
3. Реализация pkg internal/agents/partyconsistency (partyconsistency.go+spec.go+CLAUDE.md + internal_test + partyconsistency_test). Без новых go.mod-deps.
4. code-reviewer финальное ревью; make build/test/lint + -race; соответствие архитектуре; tasks.json/progress.md/session.log; git commit.

### Прогресс
- ✅ **partyconsistency.go**: `Checker{*base.BaseAgent}` ⇒ port.Agent; `var _ port.Agent`. `NewChecker(modelID,timeout,base.Deps)`: prompts.LoadPrompt/schemas.LoadSchema(AgentPartyConsistency) fail-fast %w, base.Config (AgentID=AgentPartyConsistency, Stage=StageAgentPartyConsistency), делегирует base.NewBaseAgent. §3-бюджет консты `maxOutputTokens=1000`/`temperature=0.0` (provider=LIC_AGENT_PARTY_CONSISTENCY_PROVIDER / timeout=LIC_AGENT_PARTY_CONSISTENCY_TIMEOUT=6s — wiring's job через base.Deps/ctor-param). Godoc forward-note base/router Model-defect (общий с typeclassifier #1 / keyparams).
- ✅ **spec.go**: `checkerSpec struct{}` (stateless) + `var _ base.Spec`. Локальный minimal `documentStructure{PartyDetails []partyDetails}` (json-тег `representative` per DP wire SSOT — НЕ signatory; CC-3). nil-safe `derefString` (*string→"", CHANGE-1). `Parts`: strictness-phase ДО любого `b.*` (nil KeyParameters / absent|empty|malformed DOCUMENT_STRUCTURE / absent|malformed|empty EXTRACTED_TEXT → err → INTERNAL_ERROR); assembly: party_roles=json.Marshal(PartyRoles) via Content (nil/empty→`[]`), `b.ValidationFacts(parties)` minted (всегда, даже []→`<validation_facts></validation_facts>`; []PartyValidation discarded v1 YAGNI), party_details_block=json.Marshal via Content (nil→`[]`), contract_document=artifacts.FullText() ПОЛНЫЙ БЕЗ компакции (§3 без fixed head/tail; >80K = upstream 021, base MF-3). Порядок = party_consistency.txt:595-604. `Decode`: *model.PartyConsistencyFindings + dual-enum drift-guard per finding (PartyFindingType 7-value AND RiskLevel high|medium|low); free/nullable/model-reported поля НЕ guard'ятся; no severity re-map; low толерируется (enum-membership, не prompt-policy).
- ✅ **internal_test.go**: TestHermeticImports (allowlist = keyparams set: artifacts/base/promptbuilder/prompts/schemas/model/port; БЕЗ config/infra/llm/DP-модуля) + TestGofmtClean (go/format self-check). CC-2 — без этого теста no-cross-module-rule, обосновывающее локальный decode, неэнфорсимо.
- ✅ **CLAUDE.md**: все решения с атрибуцией (code-architect Q1-Q5 + CHANGE-1/2 + CC-1..4); 6 forward-notes — #2 pipeline-ordering 034 и #3 DM-artifact-gate 034 РАЗДЕЛЬНО (CC-1, не схлопывать); #5 []PartyValidation; #6 non-critical graceful-degradation = tier-policy 034.
- ✅ Тесты: 17 функц (NewChecker-OK/fail-fast-table; envelope-order+minted-validation_facts; ValidationFacts-checksum-интеграция valid=true/false + nil-INN/OGRN no-panic + count inn×2/ogrn×1; layer-2-escaping contract_document; party_details-injection-neutralised (json \u-escape, count==1, LOW-1); full-text-no-compaction 100k; tolerated-empty party_roles/validation_facts/party_details ×2; Parts-errors-table ×7 via nil-Builder; representative-tag round-trip CC-3; Decode full/empty/low-tolerated/not-json/type-drift/severity-drift; Run-integration Шаг 1/2 budget/JSONSchema/JSONMode; Run-invalid-type-repair-triggered; concurrent-race ×32). `go test ./...` 0 FAIL (24 пакета ok); -race clean; `go vet` чисто; `make build/lint/test` зелёные; .gitkeep git rm (прецедент prompts/schemas/keyparams).

### Финальное ревью (subagent)
- **code-architect (design SSOT, 5 Q):** Q1 party_roles source=in.KeyParameters.internal_extras.party_roles; nil KeyParameters→INTERNAL_ERROR (pipeline-ordering breach, forward-note 034 DISTINCT от DM-gate), nil InternalExtras|empty PartyRoles TOLERATED — ACCEPT. Q2 рендер JSON via Content (nil→`[]`); DOCUMENT_STRUCTURE decode ЛОКАЛЬНО (artifacts централизует ТОЛЬКО EXTRACTED_TEXT; Agent-1 disjoint-projection precedent), mandatory-in-shape/empty-tolerated — ACCEPT. Q3 EXTRACTED_TEXT full, NO compaction (§3 без fixed-rule; 021 upstream) — ACCEPT. Q4 b.ValidationFacts only happy-path post-strictness (nil-Builder тесты держатся), []PartyValidation discard v1, порядок верный — ACCEPT-WITH-CHANGES (CHANGE-1 nil-safe deref *string; CHANGE-2 всегда через ValidationFacts([]), не спец-кейсить пустые roles). Q5 dual-enum guard Type+Severity per finding, free-поля не трогать, no re-map — ACCEPT. CC-1 раздельные forward-notes 034; CC-2 Hermetic+gofmt тесты; CC-3 json-тег representative; CC-4 §3 budget SSOT. ВСЕ применены.
- **code-reviewer:** **APPROVE-WITH-NITS** — 0 CRITICAL / 0 HIGH / 0 MEDIUM. Все 5 must-fix верифицированы реализованными с pinning-тестами. LOW-1 (escaping-тест не покрывал party_details/representative вектор) → добавлен TestSpec_Parts_PartyDetailsInjectionNeutralised (json \u-escape, literal-close count==1). LOW-2 (хрупкая absence-assertion) → добавлены count inn_check×2/ogrn_check×1. LOW-3 (partially-populated party_details omitempty) — записан, low value, не блокирует. Nit «SOLE»→«sole v1» consumer применён (godoc + CLAUDE.md).

### Соответствие архитектуре
- ai-agents-pipeline.md §3: вход DOCUMENT_STRUCTURE.party_details + EXTRACTED_TEXT + KeyParameters.internal_extras.party_roles; конверт <input><party_roles><validation_facts><party_details_block><contract_document>; бюджет Claude-sonnet/temp0.0/max1000/timeout6s; выход PartyConsistencyFindings{findings[{type∈PARTY_*,severity,description,party_name,clause_ref,legal_basis}],summary,prompt_injection_detected}; severity high=PARTY_AUTHORITY_MISSING/medium=rest (модель ставит per промпт, Result Aggregator не re-map'ит — Decode только enum-валидирует) — ✅ (порядок=системный промпт; schema-validation+1-shot-repair встроены base, пинуется реальной embedded-схемой).
- high-architecture.md §6.7.2 / promptbuilder forward-req #2: pre-LLM LLM-free ИНН/ОГРН checksum в `<validation_facts>` (`<inn_check>/<ogrn_check>` форма embedded-промпта), Part между <party_roles> и <party_details_block> — ✅.
- base/seams.go MF-3 / §6.6 Run-loop / §6.7 prompt-injection layer-2 (все user-байты через Content escaped; planted </contract_document> экранируется; party_details json-\u-escape двухслойно) — ✅.
- configuration.md (LIC_AGENT_PARTY_CONSISTENCY_PROVIDER/TIMEOUT=6s) — ✅ (wiring's job, ctor-param — hermetic precedent).
- house-style (hermetic; NewTypeName; var _ port.Agent/base.Spec; fail-fast %w; CLAUDE.md; -race; TestHermeticImports allowlist; gofmt self-check) — CONFORMS.

### Summary
Production-ready Agent 3 — тонкая обёртка над BaseAgent: Checker{*base.BaseAgent} ⇒ port.Agent; единственная per-agent логика — stateless checkerSpec (Parts §3-конверт с минтингом `<validation_facts>` + Decode типизированный + dual-enum drift-guard). Первый и единственный v1-потребитель promptbuilder.ValidationFacts. Design reviewed by subagent code-architect (5 Q ACCEPT + 2 CHANGE + 4 CC, все применены); реализация reviewed by subagent code-reviewer (APPROVE-WITH-NITS, 0 CRIT/HIGH/MED, LOW-1/2+nit применены). 24 пакета ok, 0 FAIL, -race clean, vet чисто.

### Notes / следующая задача
- **Forward (LIC-TASK-034 Stage Executor) — ДВА РАЗДЕЛЬНЫХ инварианта (CC-1):** (1) pipeline-ordering — Stage Executor ОБЯЗАН заполнить in.KeyParameters результатом Agent 2 (Stage 1) до диспатча Agent 3 (Stage 2); nil ⇒ wiring-defect → INTERNAL_ERROR. (2) DM-artifact-bundle gate — missing/empty mandatory DOCUMENT_STRUCTURE/EXTRACTED_TEXT = DM_ARTIFACTS_MISSING (retryable) ДО Agent 3. НЕ схлопывать.
- **Forward (LIC-TASK-034 tier-policy):** Agent 3 — non-critical (graceful degradation при timeout); сам timeout = base (per-agent ctx → AGENT_TIMEOUT retryable); деградировать-vs-fail — tier-политика Stage Executor (error-handling.md), не Spec.
- **Forward (LIC-TASK-021):** Agent 3 сознательно эмитит ПОЛНЫЙ EXTRACTED_TEXT; §3 без fixed/token-bounded компакции — только generic upstream per-artifact 021 на model.AgentInput (base MF-3).
- **Forward (LIC-TASK-047 wiring):** base.Deps.Builder = promptbuilder.NewBuilder(rec) с Recorder-адаптером над metrics.CrossCutMetrics.PartyValidationTotal — иначе `<validation_facts>`-минтинг Agent 3 не запишет lic_party_validation_total. NewChecker(resolvedPrimaryModel, Timeouts[AgentPartyConsistency], deps); регистрация в Stage Executor по model.AgentPartyConsistency (Stage 2).
- **Forward (future cross-agent verification):** ValidationFacts возвращает []PartyValidation, v1 discard'ит; задача, нуждающаяся в Go-side ground-truth без re-parse XML, потребляет здесь (forward-note 5).
- **Forward (later cleanup):** мигрировать typeclassifier с локальных extractedText/fullText на artifacts.ExtractedText, удалить дубликат (унаследовано от 026).
- **make docker-build не запускался:** нет Dockerfile (= pending LIC-TASK-003) + нет Docker daemon (вне scope; go.mod не менялся).
- **Прогресс: 23/55 done.** Открыты (deps done): 003,009,010,021,028-033,035,038,039,041-046,052. Рекомендация: продолжить каскад **LIC-TASK-028** (Agent 4 Mandatory Conditions Checker) — прямой критический путь (9 агентов→034), template закреплён (025/026/027); Agent 4 вход SEMANTIC_TREE+EXTRACTED_TEXT + ClassificationResult.contract_type+KeyParameters.

---

## LIC-TASK-028 — Agent 4: Legal Mandatory Conditions Checker [critical] — DONE 2026-05-17

### Выбор задачи
Из 32 pending выбрана LIC-TASK-028 (critical, dep 024=done) — прямое продолжение каскада, рекомендованное предыдущей итерацией. Критический путь: 9 агентов (025-033) → 034 Stage Executor → 036/037 Pipeline → 047 wiring. 025/026/027 done ⇒ numeric order ⇒ Agent 4. Template = typeclassifier/keyparams/partyconsistency. Agent 4 — первый агент, объединяющий ДВА upstream-результата (Classification+KeyParameters) + passthrough-tree + full-text.

### План
1. Изучить: keyparams/partyconsistency (template), base.{Spec,Config,Deps,canonicalStage}, model.{MandatoryConditionsReport,MandatoryCondition,MandatoryConditionStatus,IsValidMandatoryConditionCode,ClassificationResult,ContractType,KeyParameters,AgentInput}, artifacts.ExtractedText, mandatory_conditions.{txt,json}, ai-agents-pipeline §4 (envelope classification_result→key_parameters→semantic_tree→contract_document; budget Claude/temp0.0/max3000/timeout8s), configuration.md / config.AgentMandatoryConditions=8s.
2. code-architect design review (D1 classification minimal-projection vs whole / D2 key_parameters whole / D3 strictness no-tolerated-empty / D4 Decode dual-guard surfaces / D5 block order+wiring).
3. Реализация pkg internal/agents/mandatoryconditions (mandatoryconditions.go+spec.go+CLAUDE.md + internal_test + mandatoryconditions_test). Без новых go.mod-deps.
4. code-reviewer ревью; make build/test/lint + -race; соответствие архитектуре; tasks.json/progress.md/session.log; git commit.

### Прогресс
- ✅ **mandatoryconditions.go**: `Checker{*base.BaseAgent}` ⇒ port.Agent; `var _ port.Agent`. `NewChecker(modelID,timeout,base.Deps)`: prompts.LoadPrompt/schemas.LoadSchema(AgentMandatoryConditions) fail-fast %w, base.Config (AgentID=AgentMandatoryConditions, Stage=StageAgentMandatoryConditions), делегирует base.NewBaseAgent. §4-бюджет консты `maxOutputTokens=3000`/`temperature=0.0` (provider=LIC_AGENT_MANDATORY_CONDITIONS_PROVIDER / timeout=LIC_AGENT_MANDATORY_CONDITIONS_TIMEOUT=8s — wiring's job через base.Deps/ctor-param; config.AgentMandatoryConditions=8s). Godoc forward-note base/router Model-defect (общий с typeclassifier #1 / keyparams / partyconsistency).
- ✅ **spec.go**: `checkerSpec struct{}` (stateless) + `var _ base.Spec`. Минимальный `classificationProjection{ContractType model.ContractType}` (D1; типизирован — enum-rename = compile-error). `Parts(_ *promptbuilder.Builder, in)`: strictness-phase, порядок=конверт (CC-4) — nil Classification / nil KeyParameters (pipeline-ordering breach, fwd-note 2) → SEMANTIC_TREE absent|empty|!json.Valid → EXTRACTED_TEXT absent|malformed|empty (artifact-gate, fwd-note 3 DISTINCT); assembly: classification_result=json.Marshal(minimal {"contract_type":X}) via Content; key_parameters=json.Marshal(in.KeyParameters) ВЕСЬ incl. internal_extras via Content (D2; nil InternalExtras толерируется omitempty); semantic_tree=string(rawTree) byte-faithful passthrough (keyparams precedent; {} толерируется); contract_document=artifacts.FullText() ПОЛНЫЙ БЕЗ компакции (§4 без fixed head/tail; >80K=upstream 021, base MF-3). Порядок=mandatory_conditions.txt:117-122. `Decode`: *model.MandatoryConditionsReport + per-condition dual SSOT drift-guard model.IsValidMandatoryConditionCode(Code) (frozen ^MC_[A-Z0-9_]+$ — discharges acceptance Шаг 2 в Go) AND Status.IsValid() (FOUND_OK|FOUND_AMBIGUOUS|MISSING); free-string ContractType НЕ guard'ится (схема bare string; over-reach + отверг бы schema-valid); no re-map.
- ✅ **internal_test.go**: TestHermeticImports (allowlist = keyparams set: artifacts/base/promptbuilder/prompts/schemas/model/port; БЕЗ config/infra/llm/DP-модуля) + TestGofmtClean (go/format self-check).
- ✅ **CLAUDE.md**: все решения с атрибуцией (code-architect D1-D5 + CC-1..6); 4 forward-notes — #2 pipeline-ordering 034 и #3 DM-artifact-gate 034 РАЗДЕЛЬНО (не схлопывать); CC-3 (Code-regex ≠ free-string) и CC-6 (no INN/OGRN handling, no validation_facts) задокументированы.
- ✅ Тесты: 16 функц (NewChecker-OK/fail-fast-table; envelope-order 4-блока; CC-1 byte-exact минимальная проекция + leak-assertions confidence/alternatives/rationale/0.91/SALE; D2 whole-KeyParameters incl. internal_extras + nil-extras omitempty; tree byte-faithful passthrough + {} толерируется; layer-2-escaping contract_document; CC-2 upstream-JSON injection neutralised (json \u-escape, count==1); full-text-no-compaction 100k; Parts-errors-table ×8 via nil-Builder; Decode full/empty/free-contract_type/not-json/code-regex-drift×2/status-enum-drift; Run-integration Шаг 1/2 budget/JSONSchema/JSONMode + все code ^MC_; Run-invalid-code-repair-triggered; concurrent-race ×32). `go test ./...` 0 FAIL (25 пакетов ok); -race clean (16/16); `go vet` чисто; `make build/lint/test` зелёные; .gitkeep git rm.

### Финальное ревью (subagent)
- **code-architect (design SSOT, D1-D5):** D1 classification_result=минимальная {"contract_type":X} проекция (типизировать model.ContractType) — ACCEPT. D2 key_parameters=ВЕСЬ KeyParameters JSON incl. internal_extras (Agent 4 — документированный consumer; SSOT-асимметрия с .contract_type) — ACCEPT. D3 оба upstream hard-mandatory, NO tolerated-empty (contract_type — драйвер каталога, нет «(если есть)»), tree json.Valid passthrough, full text — ACCEPT. D4 dual-guard Code-regex+Status-enum per condition, ContractType (bare string) НЕ guard, no re-map — ACCEPT. D5 4 Content-блока в порядке промпта, `_ *promptbuilder.Builder` (не минтит) — ACCEPT. CC-1 pinning-тест минимальной проекции; CC-2 layer-2 на upstream-JSON; CC-3 doc Code-regex≠free-string; CC-4 порядок strictness=envelope; CC-5 преконды (model/config/canonicalStage/assets — проверены OK); CC-6 doc internal_extras passthrough. ВСЕ применены.
- **code-reviewer:** **ACCEPT** — 0 CRITICAL / 0 HIGH / 0 MEDIUM. Верифицированы конверт (vs mandatory_conditions.txt:117-122), минимальная проекция, dual drift-guard (vs model + embedded schema, no drift), strictness/nil-Builder, hermeticity, тесты (CC-1/CC-2/repair/-race). LOW-1 (Decode-side guard не покрыт end-to-end через Run — нужен base-seam для schema-valid-but-Go-invalid; идентично keyparams/partyconsistency) — pre-existing consistent cross-sibling limitation, фикс для parity НЕ требуется, записан non-blocking. NIT-1/NIT-2 (test-helper наблюдения) — harmless, без изменений.

### Соответствие архитектуре
- ai-agents-pipeline.md §4: вход SEMANTIC_TREE+EXTRACTED_TEXT + ClassificationResult.contract_type+KeyParameters; конверт <input><classification_result><key_parameters><semantic_tree><contract_document>; бюджет Claude-sonnet/temp0.0/max3000/timeout8s; выход MandatoryConditionsReport{contract_type,conditions[{code ^MC_[A-Z0-9_]+$,label,status∈FOUND_OK|FOUND_AMBIGUOUS|MISSING,legal_basis,found_in,issue_description}],summary,prompt_injection_detected} — ✅ (порядок=системный промпт; schema-validation+1-shot-repair встроены base).
- §0.3 5-layer prompt-injection (system prompt embedded + XML-envelope Content layer-2 escaping + JSON-schema validator base MF-1 + prompt_injection_detected флаг в модели) — ✅.
- high-architecture.md §6.6/§6.7.2 (тонкая обёртка над base; per-agent только Spec) / base forward-req 1/2/3 (Spec stateless, Config из SSOT, embed *BaseAgent) / base/seams.go MF-3 — ✅.
- configuration.md (LIC_AGENT_MANDATORY_CONDITIONS_PROVIDER/TIMEOUT=8s) — ✅ (wiring's job, ctor-param — hermetic precedent).
- house-style (hermetic; NewTypeName; var _ port.Agent/base.Spec; fail-fast %w; CLAUDE.md; -race; TestHermeticImports allowlist; gofmt self-check; enumerated cross-check drift-guard) — CONFORMS.

### Summary
Production-ready Agent 4 — тонкая обёртка над BaseAgent: Checker{*base.BaseAgent} ⇒ port.Agent; единственная per-agent логика — stateless checkerSpec (Parts §4-конверт 4-блока + Decode типизированный + dual SSOT drift-guard Code-regex+Status-enum). Первый агент с ДВУМЯ upstream-результатами (минимальная проекция Classification + весь KeyParameters) + byte-faithful tree passthrough + full text. Design reviewed by subagent code-architect (D1-D5 ACCEPT ×5 + CC-1..6, все применены); реализация reviewed by subagent code-reviewer (ACCEPT, 0 CRIT/HIGH/MED, LOW-1 non-blocking cross-sibling). 25 пакетов ok, 0 FAIL, -race clean, vet чисто.

### Notes / следующая задача
- **Forward (LIC-TASK-034 Stage Executor) — ДВА РАЗДЕЛЬНЫХ инварианта:** (1) pipeline-ordering — Stage Executor ОБЯЗАН заполнить in.Classification (Agent 1) И in.KeyParameters (Agent 2, Stage 1) до диспатча Agent 4 (Stage 3); nil любого ⇒ wiring-defect → INTERNAL_ERROR. (2) DM-artifact-bundle gate — missing/empty SEMANTIC_TREE/EXTRACTED_TEXT = DM_ARTIFACTS_MISSING (retryable) ДО Agent 4. НЕ схлопывать.
- **Forward (LIC-TASK-021):** Agent 4 сознательно эмитит ПОЛНЫЙ EXTRACTED_TEXT; §4 без fixed/token-bounded компакции — только generic upstream per-artifact 021 на model.AgentInput (base MF-3).
- **Forward (LIC-TASK-047 wiring):** NewChecker(resolvedPrimaryModel, Timeouts[config.AgentMandatoryConditions], deps); регистрация в Stage Executor по model.AgentMandatoryConditions (Stage 3, параллельно с Agent 5 riskdetection, после Stage 1 Agent1+Agent2).
- **Forward (later cleanup):** мигрировать typeclassifier с локальных extractedText/fullText на artifacts.ExtractedText, удалить дубликат (унаследовано от 026).
- **make docker-build не запускался:** нет Dockerfile (= pending LIC-TASK-003) + go.mod не менялся (вне scope).
- **Прогресс: 24/55 done.** Открыты (deps done): 003,009,010,021,029-033,035,038,039,041-046,052. Рекомендация: продолжить каскад **LIC-TASK-029** (Agent 5 Risk Detection & Severity Scoring) — прямой критический путь (9 агентов→034), template закреплён (025-028); Agent 5 — параллелен Agent 4 в Stage 3.


---

## LIC-TASK-029 — Agent 5: Risk Detection & Severity Scoring [critical] — DONE 2026-05-17

### Выбор задачи
Из 31 pending выбрана LIC-TASK-029 (critical, dep 024=done) — прямое продолжение каскада. Критический путь: 9 агентов (025-033) → 034 Stage Executor → 036/037 Pipeline → 047 wiring. 025-028 done ⇒ numeric order ⇒ Agent 5. Центральный агент: формирует основной RISK_ANALYSIS.risks[]. Template = mandatoryconditions (Agent 4). Отличия от Agent 4: (a) 5-блочный конверт с ОПЦИОНАЛЬНЫМ <processing_warnings>; (b) prompt_injection→PROMPT_INJECTION_ATTEMPT risk; (c) Decode drift-guard для RiskAnalysis.

### План
1. Изучить: mandatoryconditions/partyconsistency (template + 5-block/fixed-shape прецедент), base.{Spec,Config,Deps,canonicalStage}, model.{RiskAnalysis,Risk,RiskLevel,RiskCategory,IsValidRiskID,ClassificationResult,KeyParameters,AgentInput,ArtifactProcessingWarnings}, artifacts.ExtractedText, risk_detection.{txt,json}, ai-agents-pipeline §5/§0.1/§0.3/§0.5, configuration.md / config.AgentRiskDetection=12s.
2. code-architect design review (D1 classification+keyparams=Agent4 / D2 tree+text=Agent4 / D3 НОВЫЙ optional processing_warnings tier / D4 Decode drift-guard / D5 prompt-injection prompt-driven vs Go).
3. Реализация pkg internal/agents/riskdetection (riskdetection.go+spec.go+CLAUDE.md + internal_test + riskdetection_test). Без новых go.mod-deps.
4. code-reviewer ревью; make build/test/lint + -race; соответствие архитектуре; tasks.json/progress.md/session.log; git commit.

### Прогресс
- ✅ **riskdetection.go**: `Detector{*base.BaseAgent}` ⇒ port.Agent; `var _ port.Agent`. `NewDetector(modelID,timeout,base.Deps)`: prompts.LoadPrompt/schemas.LoadSchema(AgentRiskDetection) fail-fast %w, base.Config (AgentID=AgentRiskDetection, Stage=StageAgentRiskDetection), делегирует base.NewBaseAgent. §5-бюджет консты `maxOutputTokens=3500`/`temperature=0.0` (provider=LIC_AGENT_RISK_DETECTION_PROVIDER=claude / timeout=LIC_AGENT_RISK_DETECTION_TIMEOUT=12s — wiring's job через base.Deps/ctor-param; config.AgentRiskDetection=12s). Godoc forward-note 1 base/router Model-defect (общий с typeclassifier #1 / keyparams / partyconsistency / mandatoryconditions).
- ✅ **spec.go**: `detectorSpec struct{}` (stateless) + `var _ base.Spec`. `emptyProcessingWarnings=[]byte("[]")` sentinel. Минимальный `classificationProjection{ContractType model.ContractType}` (D1; типизирован). `Parts(_ *promptbuilder.Builder, in)`: strictness-phase ГРУППАМИ ПО КЛАССАМ (CC-1 — сознательная дивергенция от Agent-4 CC-4, т.к. 3 класса не 2): (1) pipeline-ordering nil Classification/KeyParameters (fwd-note 2) → (2) mandatory DM-gate SEMANTIC_TREE absent|empty|!json.Valid + EXTRACTED_TEXT absent|malformed|empty (fwd-note 3) → (3) ОПЦИОНАЛЬНЫЙ PROCESSING_WARNINGS (fwd-note 5, НЕ note 3): absent/empty/whitespace/bare-`null`-token (json.Valid("null")==true, явный fold CC-2) → литерал `[]`; present&json.Valid → byte-faithful verbatim (исходные нетримленные байты); present&!json.Valid → error. Assembly-phase = порядок конверта risk_detection.txt:52-58: classification_result(minimal {"contract_type":X}) → key_parameters(ВЕСЬ json.Marshal incl. internal_extras) → processing_warnings → semantic_tree(string(raw) byte-faithful) → contract_document(artifacts.FullText() ПОЛНЫЙ БЕЗ компакции). Всё через promptbuilder.Content (layer-2 escaped; b unused — Agent 5 не минтит). `Decode`: *model.RiskAnalysis + per-risk ТОЛЬКО Level drift-guard model.RiskLevel.IsValid() (high|medium|low = §5 schema enum точно — partyconsistency Severity прецедент). Category НЕ guard (§5 optional + RiskCategory 22-merged шире → over-reach + отверг бы schema-valid); ID НЕ guard (IsValidRiskID merged ^R-(P|M)?[0-9]{3,}$ шире agent-схемы; нет model-SSOT = ^R-[0-9]{3,}$; schema pattern через base MF-1 = SSOT). prompt_injection — PURE passthrough (D5, no transform).
- ✅ **internal_test.go**: TestHermeticImports (allowlist БАЙТ-В-БАЙТ = mandatoryconditions: artifacts/base/promptbuilder/prompts/schemas/model/port; БЕЗ config/infra/llm/DP-модуля; БЕЗ локального PW-mirror — passthrough class, CC-4) + TestGofmtClean.
- ✅ **CLAUDE.md**: все решения с атрибуцией (code-architect D1-D5 + CC-1..6); 6 forward-notes — #2 pipeline-ordering & #3 DM-artifact-gate & #5 optional-PW-tolerance ВСЕ РАЗДЕЛЬНО (не схлопывать); #6 LIC-TASK-035 — единственный post-processor флага.
- ✅ Тесты: 18 функц (NewDetector-OK/fail-fast; envelope-order 5-блоков+shape; byte-exact минимальная проекция + leak-assertions; whole-KeyParameters incl. internal_extras + nil-extras omitempty; ProcessingWarnings tiers (verbatim / absent·empty·whitespace·null·padded-null → ровно `[]` / present-malformed→err); absent-PW≠error; tree byte-faithful + {} толерируется; layer-2-escaping; CC-2 upstream-JSON injection count==1; full-text-no-compaction 100k; Parts-errors-table ×9 incl. present-malformed-PW; Decode valid/empty-risks/free-omitted-merged-category-OK/level-drift×2/not-json; D5 prompt-injection passthrough; Run-integration Шаг 1 budget/JSONSchema/JSONMode + Шаг 2 монотонные ids R-001..R-003; Run-invalid-level-repair-triggered; concurrent-race ×32). `go test ./...` 0 FAIL (27 пакетов ok); -race clean; `go vet` чисто; `make build/lint/test` зелёные; .gitkeep git rm.

### Финальное ревью (subagent)
- **code-architect (design SSOT, D1-D5):** D1 classification_result минимальная проекция + key_parameters ВЕСЬ (Agent-4 verbatim), nil→error — ACCEPT. D2 tree byte-faithful + text full no-compaction, оба mandatory — ACCEPT. D3 processing_warnings: fixed-5-block, byte-faithful passthrough, absent/empty/null→`[]`, present-malformed→err, НЕ DM-gate — **ACCEPT-WITH-CHANGE** (empty sentinel = `[]` НЕ `{}` — тип-консистентно с промптом `[…]`). D4 Decode guard ТОЛЬКО Level; Category/ID НЕ guard (looser/optional) — ACCEPT. D5 prompt_injection prompt-driven, Decode no-transform passthrough, LIC-TASK-035 = единственный flag post-processor — ACCEPT. CC-1 strictness группами по классам + документировать дивергенцию; CC-2 явный bare-null fold; CC-3 пин `<processing_warnings>[]`; CC-4 allowlist=Agent4 байт-в-байт, БЕЗ local PW-mirror; CC-5 Stage3‖4 budget; CC-6 enumerate unguarded fields в Decode godoc. ВСЕ применены.
- **code-reviewer:** **ACCEPT-WITH-NIT** — 0 CRITICAL / 0 HIGH / 0 MEDIUM. Верифицированы PW-tier (все edge: absent/empty/whitespace/null/padded-null/present-malformed/present-valid-verbatim incl. surrounding-whitespace; comma-ok map-index корректнее .Has), Decode Level-only (empty risks decodes clean; Category/ID не-guard корректно обоснованы), byte-exact проекция, layer-2 на всех 5 блоках, allowlist=Agent4, покрытие = строгий супермножество Agent-4 + PW-tiers + D5. LOW-1 (Decode-guard не покрыт end-to-end через Run — = Agent 4, нужен base-seam) non-action, =прецедент. NIT-1/2/3 (имя слайса `absent`, размещение хелпера `rm`, нумерация forward-note в package-doc) — косметика non-blocking, оставлены как прецедент Agent 4 (минимизация churn на reviewed-коде).

### Соответствие архитектуре
- ai-agents-pipeline.md §5: вход SEMANTIC_TREE+EXTRACTED_TEXT+PROCESSING_WARNINGS + ClassificationResult.contract_type+KeyParameters; конверт <input><classification_result><key_parameters><processing_warnings><semantic_tree><contract_document>; бюджет Claude-sonnet/temp0.0/max3500/timeout12s; выход RiskAnalysis{risks[{id ^R-[0-9]{3,}$,level∈high|medium|low,description,clause_ref,legal_basis,category(13-enum optional),rationale}],summary,prompt_injection_detected} — ✅ (порядок=системный промпт; schema-validation+1-shot-repair встроены base).
- §0.1 Stage 3 (parallel): [4] Mandatory Conditions ‖ [5] Risk Detection — ✅. §0.3 5-layer prompt-injection (system prompt embedded + XML-envelope Content layer-2 + JSON-schema validator base MF-1 + prompt_injection_detected флаг + warning downstream) — ✅. §0.5:90 risk_analysis.risks[]=Агент5+findings3,4 через Result Aggregator — ✅ (D5: Agent5 эмитит, LIC-TASK-035 мержит/конвертит флаг).
- high-architecture.md §6.6/§6.7.2 (тонкая обёртка над base) / base forward-req 1/2/3 / base/seams.go MF-3 — ✅.
- configuration.md:126 LIC_AGENT_RISK_DETECTION_PROVIDER=claude / :140 TIMEOUT=12s — ✅ (wiring's job, ctor-param — hermetic precedent).
- house-style (hermetic; NewTypeName; var _ port.Agent/base.Spec; fail-fast %w; CLAUDE.md; -race; TestHermeticImports allowlist; gofmt self-check; enumerated cross-check drift-guard) — CONFORMS.

### Summary
Production-ready Agent 5 — тонкая обёртка над BaseAgent: Detector{*base.BaseAgent} ⇒ port.Agent; единственная per-agent логика — stateless detectorSpec (Parts §5-конверт 5-блоков + Decode типизированный + Level-only drift-guard). Центральный risk-агент. Новое vs Agent 4: ОПЦИОНАЛЬНЫЙ 3-й блок processing_warnings (fixed-5-block, byte-faithful, absent/null→`[]` sentinel, present-malformed→err) + strictness группами по 3 классам (CC-1) + prompt-injection prompt-driven passthrough (D5). Design reviewed by subagent code-architect (D1-D5 ACCEPT ×5 incl. D3 ACCEPT-WITH-CHANGE + CC-1..6, все применены); реализация reviewed by subagent code-reviewer (ACCEPT-WITH-NIT, 0 CRIT/HIGH/MED, LOW-1 non-action, 3 NIT косметика). 27 пакетов ok, 0 FAIL, -race clean, vet чисто.

### Notes / следующая задача
- **Forward (LIC-TASK-034 Stage Executor) — ТРИ РАЗДЕЛЬНЫХ инварианта:** (note 2) pipeline-ordering — заполнить in.Classification+in.KeyParameters (Stage 1) до Agent 5 (Stage 3 ‖ Agent 4); (note 3) DM-artifact-bundle gate — missing/empty MANDATORY SEMANTIC_TREE/EXTRACTED_TEXT = DM_ARTIFACTS_MISSING ДО Agent 5; (note 5) ОПЦИОНАЛЬНЫЙ PROCESSING_WARNINGS — absent НОРМА (чистый текст PDF), толерируется→`[]`, НЕ требовать в bundle-gate. НЕ схлопывать 2/3/5.
- **Forward (LIC-TASK-021):** Agent 5 сознательно эмитит ПОЛНЫЙ EXTRACTED_TEXT; §5 без fixed/token-bounded компакции — только generic upstream per-artifact 021 (base MF-3).
- **Forward (LIC-TASK-035 Result Aggregator):** ЕДИНСТВЕННЫЙ post-processor — конвертит prompt_injection_detected=true → DETAILED_REPORT.warnings.PROMPT_INJECTION_DETECTED и убирает флаг из risks[]; мержит findings 3 (R-PNNN) / 4 (R-MNNN) в risks[]; strip rationale.
- **Forward (LIC-TASK-047 wiring):** NewDetector(resolvedPrimaryModel, Timeouts[config.AgentRiskDetection], deps); регистрация в Stage Executor по model.AgentRiskDetection (Stage 3, параллельно Agent 4 mandatoryconditions, после Stage 1 Agent1+Agent2).
- **Forward (later cleanup):** мигрировать typeclassifier на artifacts.ExtractedText, удалить дубликат (унаследовано от 026).
- **make docker-build не запускался:** требует Docker (= pending LIC-TASK-003) + go.mod не менялся (вне scope).
- **Прогресс: 25/55 done.** Открыты (deps done): 003,009,010,021,030-033,035,038,039,041-046,052. Рекомендация: продолжить каскад **LIC-TASK-030** (Agent 6 Recommendation) — прямой критический путь (9 агентов→034); template закреплён (025-029).

## LIC-TASK-030 — Agent 6: Recommendation [critical] — DONE 2026-05-17

### Выбор задачи
Из 30 pending выбрана LIC-TASK-030 (critical, dep 024=done) — прямое продолжение каскада агентов. Критический путь: 9 агентов (025-033) → 034 Stage Executor → 036→037→040→047 → 048-055. 025-029 done ⇒ Agent 6 следующий в проверенной последовательности, минимальный риск. Замечание: recommendation/ содержала только пустой .gitkeep (работа прошлой итерации НЕ была сохранена) ⇒ реализация с нуля. Template = mandatoryconditions (Agent 4, 4-блочный). Отличия от Agent 4: (a) НЕТ <contract_document>/EXTRACTED_TEXT → hermetic divergence (НЕ импортит artifacts); (b) risk_analysis ← in.MergedRiskAnalysis (post-LIC-TASK-035 merge, НЕ raw); (c) temp=0.2 (ПЕРВЫЙ агент с temp>0); (d) схема БЕЗ pattern на risk_id → Decode-guard терминальный (НЕ repair).

### План
1. Изучить: riskdetection (Agent 5, 5-block шаблон), mandatoryconditions (Agent 4, 4-block + Code-guard прецедент), base.{Spec,Config,Deps,canonicalStage,Run шаг7/8}, model.{Recommendations,Recommendation,IsValidRiskID,AgentInput.MergedRiskAnalysis/MandatoryConditions/KeyParameters,AgentRecommendation,StageAgentRecommendation}, recommendation.{txt §6,json}, ai-agents-pipeline §6 (temp=0.2/max=3000/timeout=10s), config.AgentRecommendation=10s, promptbuilder.Content/Build.
2. code-architect design review (D1 hermetic divergence drop artifacts / D2 4-block envelope / D3 strictness pipeline-ordering+DM-gate / D4 Decode FORMAT-guard terminal-not-repair / D5 budget temp=0.2).
3. Реализация pkg internal/agents/recommendation (recommendation.go+spec.go+CLAUDE.md + internal_test + recommendation_test). Без новых go.mod-deps.
4. code-reviewer ревью; make build/test/lint + -race; соответствие архитектуре; tasks.json/progress.md/session.log; git commit.

### Прогресс
- ✅ **recommendation.go**: `Recommender{*base.BaseAgent}` ⇒ port.Agent; `var _ port.Agent`. `NewRecommender(modelID,timeout,base.Deps)`: prompts.LoadPrompt/schemas.LoadSchema(AgentRecommendation) fail-fast %w, base.Config (AgentID=AgentRecommendation, Stage=StageAgentRecommendation), делегирует base.NewBaseAgent. §6-бюджет консты `maxOutputTokens=3000`/`temperature=0.2` — ПЕРВЫЙ агент с temp>0 (§6 «немного выше 0 — для разнообразия формулировок»; base валидирует [0,1]; godoc запрещает «нормализацию» к 0.0). Provider=LIC_AGENT_RECOMMENDATION_PROVIDER=claude / timeout=config.AgentRecommendation=10s — wiring's job (ctor-param). Godoc forward-note 1 base/router Model-defect (общий с typeclassifier #1 / keyparams / partyconsistency / mandatoryconditions / riskdetection).
- ✅ **spec.go**: `recommenderSpec struct{}` (stateless) + `var _ base.Spec`. `Parts(_ *promptbuilder.Builder, in)`: strictness-phase ЗЕРКАЛИТ конверт (mandatoryconditions CC-4 — только mandatory-блоки в 2 классах, нет optional как у Agent 5): nil in.KeyParameters / nil in.**MergedRiskAnalysis** (НЕ raw RiskAnalysis — MF-D3.1 load-bearing) / nil in.MandatoryConditions = pipeline-ordering (fwd-note 2) → SEMANTIC_TREE absent|empty|!json.Valid (DM-gate, fwd-note 3). Empty Conditions[]/Risks[] толерируется (MF-D3.2). Assembly = порядок recommendation.txt:32-37: key_parameters → risk_analysis → mandatory_conditions_report → semantic_tree; каждый upstream-блок ВЕСЬ json.Marshal (без проекции), tree byte-faithful string(raw). НЕТ <contract_document>. Всё через promptbuilder.Content (layer-2 escaped, MF-D2.1; b unused — Agent 6 не минтит). `Decode`: *model.Recommendations + per-rec FORMAT-guard `model.IsValidRiskID` (FROZEN ^R-(P|M)?[0-9]{3,}$ SSOT, risk_analysis.go — godoc явно покрывает recommendations[].risk_id; БЕЗ локального regexp, MF-D4.3). КРУКС D4: схема recommendation.json БЕЗ pattern на risk_id ⇒ malformed id SCHEMA-VALID ⇒ base шаг-7 проходит ⇒ 1-shot repair НЕ срабатывает ⇒ Decode-guard (шаг-8) = ЕДИНСТВЕННЫЙ/ТЕРМИНАЛЬНЫЙ FORMAT-enforcement → терминальный INTERNAL_ERROR (НЕ repair); сознательная ИНВЕРСИЯ riskdetection (Agent-4 Code-guard class, НЕ Agent-5 Risk.ID over-reach class). FORMAT only — EXISTENCE/dedup downstream (LIC-TASK-035 RECOMMENDATION_ORPHAN_REF).
- ✅ **internal_test.go**: TestHermeticImports — allowlist СТРОГОЕ 6-ENTRY ПОДМНОЖЕСТВО БЕЗ artifacts (base/promptbuilder/prompts/schemas/model/port) + явный assert «artifacts НЕ должен присутствовать» (D1 divergence, MF-D1.2 — НЕ «byte-identical to mandatoryconditions»); + TestGofmtClean.
- ✅ **CLAUDE.md**: все решения с атрибуцией (code-architect D1-D5 + MF-D*); 4 forward-notes раздельно (#2 pipeline-ordering & #3 DM-gate & #4 LIC-TASK-035 existence/dedup — не схлопывать).
- ✅ Тесты: 16 функц (NewRecommender-OK/fail-fast×3; envelope-order 4-блока + НЕТ <contract_document> + shape; whole upstream blocks merged-RA incl summary/R-PNNN/R-MNNN + KP incl internal_extras + nil-extras omitempty + MC whole; tree byte-faithful + {} толерируется; layer-2-escaping; upstream-JSON injection count==1; Parts-errors-table ×7 incl. **nil-merged/non-nil-raw** MF-D3.1; empty-slices толерируется MF-D3.2; Decode valid R-001/R-P001/R-M001 + []-OK + bad×4(not-json/X-001/R-1/empty); orphan-not-guarded MF-D4.4; **TestRun_MalformedRiskID_TerminalNotRepaired** MF-D4.1 крукс: schema-valid bad id → repairCalled=false → terminal INTERNAL_ERROR; Run-integration Шаг1 budget temp=0.2/max=3000/JSONSchema/JSONMode + Шаг2 R-/R-P/R-M; concurrent-race ×32). `go test ./...` 0 FAIL (все пакеты ok); -race clean; `go vet` чисто; `make build/lint/test` зелёные; .gitkeep удалён.

### Финальное ревью (subagent)
- **code-architect (design SSOT, D1-D5):** D1 hermetic divergence drop artifacts (strict 6-entry subset, non-EXTRACTED_TEXT-consumer class) — ACCEPT. D2 4-block §6 envelope, b unused — ACCEPT. D3 strictness pipeline-ordering(KP/MergedRA/MC nil)+DM-gate(SEMANTIC_TREE), MergedRiskAnalysis НЕ raw load-bearing, empty-slices толер — ACCEPT. D4 Decode IsValidRiskID FORMAT-guard, schema-silent → terminal-not-repair (Agent-4 Code-guard class) — ACCEPT. D5 budget temp=0.2 (первый temp>0), forward-notes — ACCEPT. Must-fix (док/тесты): MF-D1.2/D2.1/D3.1/D3.2/D4.1/D4.2/D4.3/D4.4/D5.1 — ВСЕ применены; 2 неоспоримых code-constraint MF-D4.3 (model.IsValidRiskID, не локальный regexp) + MF-D2.1 (4 блока через Content) — соблюдены.
- **code-reviewer:** **APPROVE** — 0 CRITICAL / 0 HIGH. Верифицированы: D1 6-entry allowlist + active-fail на artifacts; D2 4 Content-блока §6-порядок + escapeText безусловный; D3 MF-D3.1 nil-merged/non-nil-raw non-tautological; D4 крукс против реального gojsonschema (X-001 genuinely schema-valid → repair НЕ срабатывает) + orphan-not-guarded; D5 canonicalStage fail-fast + temp=0.2∈[0,1]; recommenderSpec stateless -race; error-wrapping %w консистентно с сиблингами; godoc/CLAUDE.md = коду. M1/M2/M3/L1-L3/N1-N2 — унаследованные от смерженных сиблингов doc/test косметические нити, non-blocking; сознательно оставлены ради верности паттерну (изменение = недокументированная дивергенция от reviewed-теста-сиблинга).

### Соответствие архитектуре
- ai-agents-pipeline.md §6: вход SEMANTIC_TREE (от DM) + MergedRiskAnalysis.risks[] (incl. встроенные findings 3/4) + MandatoryConditionsReport + KeyParameters; конверт <input><key_parameters><risk_analysis><mandatory_conditions_report><semantic_tree>; бюджет Claude-sonnet/temp0.2/max3000/timeout10s; выход Recommendations[{risk_id ^R-(P|M)?[0-9]{3,}$,original_text≤2000,recommended_text≤3000,explanation≤800}] — ✅ (порядок=системный промпт; schema-validation+1-shot-repair встроены base; risk_id format в Go т.к. схема молчит).
- §0.1 Stage 4 (sequential): [6] Recommendation — ✅ (ПОСЛЕ Result Aggregator merge LIC-TASK-035). §6 КРИТЕРИЙ 2: risk_id existence + RECOMMENDATION_ORPHAN_REF = LIC-TASK-035 (Agent 6 guard'ит только FORMAT) — ✅. §0.3 5-layer prompt-injection (system prompt embedded + XML-envelope Content layer-2 на всех 4 блоках + JSON-schema validator base MF-1 + downstream) — ✅. high-architecture.md §6.6/§6.7.2 (тонкая обёртка над base) / §6.11.1 (merged R-NNN/R-PNNN/R-MNNN namespace) — ✅.
- configuration.md LIC_AGENT_RECOMMENDATION_PROVIDER=claude / TIMEOUT=10s (config.AgentRecommendation=10s) — ✅ (wiring's job, ctor-param — hermetic precedent).
- house-style (hermetic; NewTypeName NewRecommender; var _ port.Agent/base.Spec; fail-fast %w; CLAUDE.md; -race; TestHermeticImports allowlist; gofmt self-check; enumerated cross-check drift-guard) — CONFORMS.

### Summary
Production-ready Agent 6 — тонкая обёртка над BaseAgent: Recommender{*base.BaseAgent} ⇒ port.Agent; единственная per-agent логика — stateless recommenderSpec (Parts §6-конверт 4-блока + Decode типизированный + risk_id FORMAT drift-guard). Генерирует Recommendations для рисков (merged) и недостающих обязательных условий. Новое vs Agent 4: (1) hermetic divergence — НЕ импортит artifacts (нет EXTRACTED_TEXT), strict 6-entry subset allowlist; (2) risk_analysis ← in.MergedRiskAnalysis (post-035 merge, MF-D3.1); (3) temp=0.2 — ПЕРВЫЙ агент с temp>0; (4) Decode-guard ТЕРМИНАЛЬНЫЙ (схема молчит на risk_id → нет repair, инверсия riskdetection). Design reviewed by subagent code-architect (D1-D5 ACCEPT ×5 + 9 MF, все применены); реализация reviewed by subagent code-reviewer (APPROVE, 0 CRIT/HIGH, Medium/Low = унаследованные сиблинг-нити non-blocking). Все пакеты ok, 0 FAIL, -race clean, vet чисто, make build/lint/test зелёные.

### Notes / следующая задача
- **Forward (LIC-TASK-034 Stage Executor) — ДВА РАЗДЕЛЬНЫХ инварианта:** (note 2) pipeline-ordering — заполнить in.KeyParameters + in.MandatoryConditions + in.**MergedRiskAnalysis** (post-LIC-TASK-035 merge, НЕ raw in.RiskAnalysis) до Agent 6 (Stage 4 sequential), регистрация по model.AgentRecommendation; (note 3) DM-artifact-bundle gate — missing/empty SEMANTIC_TREE = DM_ARTIFACTS_MISSING ДО Agent 6. НЕ схлопывать 2/3.
- **Forward (LIC-TASK-035 Result Aggregator):** мержит findings 3 (R-PNNN) / 4 (R-MNNN) в risks[] → in.MergedRiskAnalysis ДО Agent 6; ЕДИНСТВЕННЫЙ владелец risk_id EXISTENCE/dedup → DETAILED_REPORT.warnings.RECOMMENDATION_ORPHAN_REF (Agent 6 guard'ит только FORMAT).
- **Forward (LIC-TASK-047 wiring):** NewRecommender(resolvedPrimaryModel, Timeouts[config.AgentRecommendation], deps); регистрация в Stage Executor по model.AgentRecommendation (Stage 4 sequential).
- **make docker-build не запускался:** требует Docker (= pending LIC-TASK-003) + go.mod не менялся (вне scope).
- **Прогресс: 26/55 done.** Открыты (deps done): 003,009,010,021,031,032,033,035,038,039,041-046,052. Рекомендация: продолжить каскад **LIC-TASK-031** (Agent 7 Business Summary) или **LIC-TASK-032** (Agent 8 Detailed Report) — оставшиеся блокеры 034; template закреплён (025-030).
