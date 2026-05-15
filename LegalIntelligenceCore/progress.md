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
