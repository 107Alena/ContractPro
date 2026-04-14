# Верхнеуровневая архитектура API/Backend Orchestrator

В рамках документа описана архитектура **компонента API/Backend Orchestrator** сервиса **ContractPro** до уровня компонентов.

---

# 1. Ключевые требования и ограничения

## 1.1 Бизнес-контекст

ContractPro — AI-сервис проверки договоров в юрисдикции РФ. Пользователи загружают договоры, система анализирует риски, формирует рекомендации и выдаёт отчёты.

**API/Backend Orchestrator** — единая точка входа для frontend-приложений и внешних интеграций. Это **координирующий слой** между пользователем и доменными сервисами. Оркестратор НЕ является доменным сервисом — он не содержит бизнес-логики анализа, хранения или управления пользователями.

**Ключевые функции оркестратора:**

1. Приём файлов от пользователя, валидация и загрузка в Object Storage.
2. Координация создания документов и версий в DM.
3. Публикация команд на обработку и сравнение в DP.
4. Агрегация данных из нескольких доменов для frontend.
5. Аутентификация и авторизация (JWT, RBAC).
6. Доставка статусов обработки в реальном времени (SSE).
7. Проксирование административных операций в OPM.
8. Tenant isolation enforcement на входе в систему.
9. Rate limiting, input validation, CORS, audit logging.

## 1.2 Функциональные требования, влияющие на оркестратор

| Требование | Влияние на оркестратор |
|------------|----------------------|
| UR-1 (загрузка договора, статус обработки) | Оркестратор принимает файл, загружает в S3, создаёт документ/версию в DM, публикует команду в DP, возвращает 202 Accepted, доставляет статус через SSE |
| UR-2 (поддержка форматов) | Оркестратор валидирует MIME-тип файла на входе (PDF в v1, DOC/DOCX запланированы) |
| UR-3 (автоопределение типа) | Оркестратор проксирует результат классификации LIC из DM для пользователя |
| UR-4 (список рисков) | Оркестратор запрашивает артефакт RISK_ANALYSIS из DM и формирует ответ для frontend |
| UR-5 (пояснения по рискам) | Оркестратор агрегирует RISK_ANALYSIS + RECOMMENDATIONS из DM |
| UR-6 (рекомендации формулировок) | Оркестратор проксирует артефакт RECOMMENDATIONS из DM |
| UR-7 (краткое резюме) | Оркестратор проксирует артефакт SUMMARY из DM, фильтруя по роли пользователя |
| UR-8 (сравнение с шаблоном/политикой) | Оркестратор запрашивает политику из OPM и результат анализа из DM |
| UR-9 (повторная проверка версий) | Оркестратор создаёт новую версию с `origin_type=RE_CHECK` в DM и публикует команду в DP |
| UR-10 (выгрузка отчёта) | Оркестратор запрашивает артефакт EXPORT_PDF/EXPORT_DOCX из DM (presigned URL) |
| UR-11 (обратная связь) | Оркестратор принимает feedback от пользователя (хранение — отдельный вопрос, ASSUMPTION-ORCH-08) |
| UR-12 (настройка строгости) | Оркестратор проксирует запросы администратора в OPM |
| FR-5.3.1 (сравнение версий) | Оркестратор публикует `CompareDocumentVersionsRequested` в DP |
| FR-6.2 (разграничение по ролям) | Оркестратор реализует RBAC middleware, фильтрует ответы по роли |
| FR-6.3 (журналирование) | Оркестратор ведёт audit log на своём уровне |

## 1.3 Нефункциональные требования, влияющие на оркестратор

| NFR | Влияние на оркестратор |
|-----|----------------------|
| NFR-1.3 (UI ≤ 2 сек p95) | Оркестратор — на критическом пути. Sync-ответы (списки, метаданные, результаты) должны укладываться в ≤ 500 мс p95 (бюджет: 500 мс оркестратор + 100 мс DM + ~1.4 сек запас для сети и frontend rendering) |
| NFR-1.4 (горизонтальное масштабирование) | Оркестратор должен быть stateless (или near-stateless с Redis) для горизонтального масштабирования без downtime |
| NFR-2.1 (SLA ≥ 98%) | Оркестратор — SPOF для пользователей. Обязательна HA-конфигурация (≥ 2 инстанса) |
| NFR-2.5 (деградация при частичном отказе) | При недоступности OPM — возврат default-политик. При недоступности DM — 503. При медленном DP — уведомление пользователя через SSE |
| NFR-3.1 (TLS) | TLS termination на уровне reverse proxy / load balancer перед оркестратором |
| NFR-3.3 (tenant isolation) | Все запросы фильтруются по `organization_id` из JWT |
| NFR-3.4 (журнал действий) | Audit log: кто, когда, что сделал (загрузка, просмотр, экспорт, изменение настроек) |
| NFR-5.1 (2–3 действия) | API должен минимизировать количество вызовов для основных сценариев |
| NFR-5.2 (ошибки на русском) | Оркестратор маппит внутренние ошибки в user-friendly сообщения на русском |
| NFR-6.2 (REST API с аутентификацией) | REST API с Bearer JWT |

## 1.4 Архитектурные ограничения

1. **EDA** — межсервисное взаимодействие через RabbitMQ (событийная архитектура).
2. **At-least-once delivery** — оркестратор должен быть идемпотентным при приёме событий.
3. **Единые correlation fields** — `correlation_id`, `job_id`, `document_id`, `version_id`, `organization_id`, `requested_by_user_id`.
4. **Go 1.26** — единый язык с DP и DM.
5. **RabbitMQ** — брокер сообщений (развёрнут для DP и DM).
6. **Redis** — KV-store для session state, rate limiting, SSE pub/sub.
7. **Yandex Object Storage** — S3-compatible, для загрузки исходных файлов.
8. **EventMeta envelope** — все события содержат `correlation_id` и `timestamp` (совместимость с DP/DM).
9. **DM sync API — единственный потребитель: оркестратор** (ASSUMPTION-15 из DM).
10. **Файл загружается в Object Storage ДО создания версии в DM** (ASSUMPTION-4 из DM).

## 1.5 Междоменные зависимости оркестратора

```
API/Backend Orchestrator
    │
    ├── (sync REST) ──► DM: CRUD документов, чтение метаданных/артефактов, создание версий
    ├── (async pub) ──► DP: команды process-document, compare-versions
    ├── (async sub) ◄── DP: status-changed, processing-completed/failed, comparison-completed/failed
    ├── (async sub) ◄── LIC: status-changed (статус юридического анализа)
    ├── (async sub) ◄── RE: status-changed (статус формирования отчётов)
    ├── (async sub) ◄── DM: version-reports-ready, version-partially-available, version-created,
    │                        version-artifacts-ready, version-analysis-ready
    ├── (sync REST) ──► OPM: чтение/обновление политик (proxy для R-3)
    ├── (sync REST) ──► UOM: аутентификация (JWT validation), получение профиля пользователя
    ├── (S3 API)   ──► Object Storage: загрузка исходных файлов
    └── (Redis)    ──► Redis: SSE pub/sub, rate limiting, upload tracking
```

**Направление зависимостей:**
- **DM** — основная зависимость. Без DM оркестратор не может выполнить большинство операций.
- **DP** — только async. Оркестратор отправляет команды и получает события, не ждёт синхронного ответа.
- **LIC, RE** — только async (подписка на `status-changed`). Оркестратор получает статусы для мгновенного обнаружения сбоев и гранулярного отображения прогресса. При отсутствии событий — fallback на DM Watchdog (ASSUMPTION-ORCH-14).
- **OPM** — опциональная зависимость. При недоступности — fallback на default-политики.
- **UOM** — критическая зависимость для аутентификации. Без UOM — 503 для всех запросов. В v1 JWT валидируется локально (по публичному ключу), профиль кэшируется.

## 1.6 Архитектурные риски

| ID | Риск | Вероятность | Влияние | Митигация |
|----|------|-------------|---------|-----------|
| ORCH-R-1 | DM недоступен → все пользовательские операции блокированы | Средняя | Критическое | Circuit breaker для DM. Health check DM в readiness probe. При недоступности — 503 с retry-after. Кэширование горячих данных (списки документов) в Redis (short TTL 30s). |
| ORCH-R-2 | Потеря SSE-соединения → пользователь не видит обновления статуса | Высокая | Среднее | SSE auto-reconnect (встроен в браузер). Fallback: polling endpoint `/status`. Клиент переключается на polling при 3 неудачных SSE-reconnect. |
| ORCH-R-3 | Object Storage недоступен при загрузке файла → пользователь не может загрузить договор | Средняя | Высокое | Retry upload (3 попытки, exponential backoff). Alert при error rate > 1%. |
| ORCH-R-4 | RabbitMQ недоступен → команды DP не публикуются, события не доходят | Низкая | Критическое | RabbitMQ HA (кластер). Retry publish. При длительной недоступности — readiness probe fails → LB перестаёт направлять трафик. |
| ORCH-R-5 | JWT secret compromised → полная компрометация auth | Низкая | Критическое | Ротация ключей. Short-lived access tokens (15 мин). Refresh token с отзывом. |
| ORCH-R-6 | Горизонтальное масштабирование ломает SSE → клиент подключён к инстансу A, событие приходит на B | Высокая | Высокое | Redis Pub/Sub как broadcast layer между инстансами. Каждый инстанс подписан на Redis channel. |
| ORCH-R-7 | Slow/Large file upload блокирует горутину → исчерпание ресурсов | Средняя | Среднее | Лимит размера (20 МБ). Streaming upload (не буферизация в память). Request timeout 60s для upload. Concurrent upload limiter. |

---

# 2. Архитектурные допущения

| ID | Допущение | Обоснование |
|----|-----------|-------------|
| ASSUMPTION-ORCH-01 | Оркестратор — единственная точка входа для frontend и внешних интеграций. Frontend не обращается к DM, DP, OPM, UOM напрямую. | ASSUMPTION-15 из DM. Упрощает безопасность, аудит, rate limiting. |
| ASSUMPTION-ORCH-02 | JWT-токен содержит claims: `user_id`, `organization_id`, `role` (`LAWYER`, `BUSINESS_USER`, `ORG_ADMIN`). Токен подписан RSA/ECDSA. Оркестратор валидирует подпись локально по публичному ключу без обращения к UOM. | Стандартный подход для stateless auth. Минимизирует зависимость от UOM на каждый запрос. |
| ASSUMPTION-ORCH-03 | UOM предоставляет эндпоинты: `POST /auth/login`, `POST /auth/refresh`, `POST /auth/logout`, `GET /users/me`. Оркестратор проксирует их. | UOM ещё не спроектирован; это минимальный контракт, необходимый оркестратору. |
| ASSUMPTION-ORCH-04 | OPM предоставляет REST API для CRUD политик и чек-листов организации: `GET /policies`, `PUT /policies/{id}`, `GET /checklists`, `PUT /checklists/{id}`. | OPM ещё не спроектирован; оркестратор проксирует запросы R-3. |
| ASSUMPTION-ORCH-05 | Исходный файл загружается в Object Storage в bucket, общий с DM, с prefix `uploads/{organization_id}/{document_id}/{uuid}`. DM получает `storage_key` при создании версии и валидирует его наличие через HEAD-запрос (согласно DM sequence-diagrams.md, сценарий 8.2). | ASSUMPTION-4 из DM. Один bucket упрощает инфраструктуру. |
| ASSUMPTION-ORCH-06 | Real-time уведомления реализуются через SSE (Server-Sent Events), а не WebSocket или long polling. Обоснование — см. ADR-3. | Unidirectional push достаточен для статусных обновлений. SSE проще WebSocket в реализации и поддержке. |
| ASSUMPTION-ORCH-07 | Оркестратор НЕ хранит данные в PostgreSQL. Все persistent-данные — в доменных сервисах (DM, UOM, OPM). Оркестратор использует Redis для ephemeral state (SSE subscriptions, rate limit counters, upload tracking). | Оркестратор — координирующий слой, не source of truth. |
| ASSUMPTION-ORCH-08 | Пользовательский feedback (UR-11) сохраняется в DM как отдельный тип артефакта (`USER_FEEDBACK`) привязанный к `version_id`. Контракт на этот артефакт будет согласован с командой DM. В v1 как fallback — оркестратор хранит feedback в Redis с TTL 30 дней и публикует в RabbitMQ для последующей обработки. | Feedback должен быть персистентным, но DM пока не имеет этого типа артефакта. |
| ASSUMPTION-ORCH-09 | Нагрузка на старте: ~1000 договоров/сутки ≈ 42 загрузки/час ≈ 0.7 загрузок/мин. Пиковая нагрузка: до 10 загрузок/мин. Средний размер файла: 5–10 МБ. | Из ТЗ и ASSUMPTION-10 DM. |
| ASSUMPTION-ORCH-10 | Оркестратор подписывается на промежуточные DM-события (`version-artifacts-ready`, `version-analysis-ready`) для отображения прогресса обработки пользователю. Это не нарушает контракты DM — RabbitMQ поддерживает множество потребителей через отдельные queue bindings. | Без промежуточных событий пользователь видит только «обработка» и «готово», что неинформативно для 60–120 секундного процесса. |
| ASSUMPTION-ORCH-11 | Для загрузки файла используется multipart/form-data с одним файлом + JSON-метаданные (title). Загрузка нескольких файлов одновременно не поддерживается в v1. | Один договор = один файл (PDF). Batch upload — отдельный сценарий, за рамками v1. |
| ASSUMPTION-ORCH-12 | Оркестратор маппит DM-понятие «документ» в пользовательское «договор» (contract). API для frontend использует термин `contracts` в URL-путях, внутренне транслируя в DM API `documents`. | Пользователь оперирует понятием «договор», а не «документ». |
| ASSUMPTION-ORCH-13 | LIC и RE публикуют собственные статусные события `lic.events.status-changed` и `re.events.status-changed` (по аналогии с DP `dp.events.status-changed`). Формат: `{correlation_id, timestamp, job_id, document_id, version_id, organization_id, status, error_code, error_message, is_retryable}`. Статусы: `IN_PROGRESS`, `COMPLETED`, `FAILED`. | LIC и RE ещё не спроектированы; это архитектурное требование к будущим доменам. Единообразие с DP. Обеспечивает мгновенное обнаружение сбоев LIC/RE оркестратором (без ожидания 30-минутного DM Watchdog). |
| ASSUMPTION-ORCH-14 | DM Stale Version Watchdog поддерживает per-stage таймауты вместо единого глобального. Рекомендуемые значения: `PENDING → PROCESSING_ARTIFACTS_RECEIVED` — 5 мин, `PROCESSING_ARTIFACTS_RECEIVED → ANALYSIS_ARTIFACTS_RECEIVED` — 10 мин, `ANALYSIS_ARTIFACTS_RECEIVED → REPORTS_READY` — 5 мин. DM Watchdog выступает как safety net на случай тихого сбоя LIC/RE (crash без публикации события). | Текущий `DM_STALE_VERSION_TIMEOUT=30m` слишком велик для pipeline, который обычно занимает 2–5 мин. Per-stage таймауты позволяют обнаруживать застрявшие версии быстрее. Требует доработки DM — задача для команды DM. |

---

# 3. Границы компонента

## 3.1 Что входит в оркестратор

1. **HTTP REST API** для frontend и внешних интеграций.
2. **Файловая загрузка** — приём multipart/form-data, валидация, upload в Object Storage.
3. **Аутентификация** — JWT-валидация, извлечение auth context.
4. **Авторизация** — RBAC middleware (по роли из JWT).
5. **Координация загрузки** — создание документа/версии в DM, публикация команды в DP.
6. **Координация повторной проверки** — создание новой версии с `origin_type=RE_CHECK`.
7. **Координация сравнения** — публикация `CompareDocumentVersionsRequested` в DP.
8. **Агрегация данных** — объединение метаданных из DM, статусов, артефактов для frontend.
9. **Real-time уведомления** — SSE endpoint, Redis Pub/Sub broadcast.
10. **Маппинг статусов** — трансляция внутренних `artifact_status` DM и DP-статусов в user-friendly формат.
11. **Проксирование** — OPM (политики), UOM (auth), DM (артефакты).
12. **Rate limiting** — per-organization лимиты.
13. **Input validation** — размер файла, MIME-тип, sanitization.
14. **Audit logging** — журнал пользовательских действий.
15. **Приём обратной связи** (UR-11).

## 3.2 Что не входит в оркестратор

| Функция | Принадлежит |
|---------|-------------|
| OCR, text extraction, structure extraction, semantic tree, diff | DP |
| Классификация договора, анализ рисков, рекомендации, резюме | LIC |
| Формирование отчётов (PDF/DOCX) | RE |
| Хранение документов, версий, артефактов, audit trail по артефактам | DM |
| Управление пользователями, ролями, аутентификация (выдача JWT) | UOM |
| Управление политиками, чек-листами, правилами проверки | OPM |
| Обработка платежей | Payment Processing |
| Бизнес-логика анализа любого рода | LIC, DP |
| State machine `artifact_status` (переходы) | DM |

## 3.3 Границы относительно DM

| Ответственность | Orchestrator | DM |
|----------------|-------------|-----|
| Загрузка файла в Object Storage | ✓ | — |
| Создание документа (sync REST) | ✓ (вызывает) | ✓ (реализует) |
| Создание версии (sync REST) | ✓ (вызывает) | ✓ (реализует) |
| Чтение метаданных и артефактов | ✓ (вызывает, агрегирует) | ✓ (реализует) |
| Хранение артефактов | — | ✓ |
| Маппинг artifact_status → user status | ✓ | — |
| Архивация / soft delete документа | ✓ (проксирует) | ✓ (реализует) |

## 3.4 Границы относительно DP

| Ответственность | Orchestrator | DP |
|----------------|-------------|-----|
| Генерация `job_id`, `correlation_id` | ✓ | — |
| Публикация `ProcessDocumentRequested` | ✓ | ✓ (обрабатывает) |
| Публикация `CompareDocumentVersionsRequested` | ✓ | ✓ (обрабатывает) |
| Обработка документа | — | ✓ |
| Получение статусных событий | ✓ (подписан) | ✓ (публикует) |

## 3.5 Границы относительно OPM

| Ответственность | Orchestrator | OPM |
|----------------|-------------|-----|
| CRUD политик и чек-листов | ✓ (проксирует запросы R-3) | ✓ (реализует) |
| Хранение политик | — | ✓ |
| Применение политик при анализе | — | OPM + LIC |

## 3.6 Границы относительно UOM

| Ответственность | Orchestrator | UOM |
|----------------|-------------|-----|
| Приём login/refresh/logout | ✓ (проксирует) | ✓ (реализует, выдаёт JWT) |
| JWT-валидация на каждый запрос | ✓ (локально) | — |
| Управление пользователями и ролями | — | ✓ |

## 3.7 Границы относительно RE и LIC

Оркестратор **не взаимодействует** с LIC и RE напрямую. Цепочка DP → DM → LIC → DM → RE → DM полностью автономна. Оркестратор подписывается на DM-события о готовности артефактов и читает результаты из DM sync API.

## 3.8 Границы относительно Payment Processing

В v1 оркестратор не взаимодействует с Payment Processing. В будущих версиях оркестратор может проксировать платёжные операции и проверять наличие активной подписки перед разрешением загрузки.

---

# 4. Архитектурная концепция

## 4.1 Назначение компонента

API/Backend Orchestrator — **координирующий слой** (не доменный сервис), являющийся единственной точкой входа для пользователей и внешних систем в платформу ContractPro. Оркестратор:

- принимает пользовательские запросы через REST API;
- аутентифицирует и авторизует пользователей;
- координирует multi-step операции, затрагивающие несколько доменов;
- агрегирует данные из нескольких доменов в единый ответ для frontend;
- доставляет статусные обновления в реальном времени;
- обеспечивает tenant isolation, rate limiting, input validation.

## 4.2 Роль оркестратора в общей системе

```
                    ┌──────────────────┐
                    │    Frontend /     │
                    │  External API     │
                    └────────┬─────────┘
                             │  HTTPS (REST + SSE)
                             ▼
                ┌────────────────────────────┐
                │  API / Backend Orchestrator │
                │  (координирующий слой)      │
                └──┬──────┬───────┬───────┬──┘
                   │      │       │       │
          sync REST│      │async  │sync   │S3 API
                   │      │RabbitMQ│REST   │
                   ▼      ▼       ▼       ▼
                  DM     DP      OPM   Object
               (stateful)(stateless)(stateful) Storage
                   │                │
                   │  async events  │
                   ▼                │
                  LIC ────────► DM ◄── RE
               (stateless)      (hub)  (stateless)
```

**Потоки данных:**

1. Frontend → Orchestrator → DM: создание документа/версии (sync).
2. Frontend → Orchestrator → Object Storage: загрузка файла.
3. Orchestrator → RabbitMQ → DP: команда на обработку (async).
4. DP → DM → LIC → DM → RE → DM: полностью автономный pipeline (async).
5. DM → RabbitMQ → Orchestrator: событие о готовности (async).
6. Orchestrator → SSE → Frontend: real-time уведомление.
7. Frontend → Orchestrator → DM: чтение результатов (sync).

## 4.3 Принципы проектирования

| # | Принцип | Обоснование |
|---|---------|-------------|
| 1 | **Stateless orchestration** | Оркестратор не хранит persistent state. Все данные — в DM, OPM, UOM. Redis используется только для ephemeral state (SSE, rate limits). Обеспечивает горизонтальное масштабирование. |
| 2 | **Smart proxy, dumb pipe** | Оркестратор умно маршрутизирует и агрегирует, но не содержит бизнес-логику. Бизнес-логика — в доменах. |
| 3 | **Fail-fast with graceful degradation** | При недоступности зависимости — быстрый ответ с понятной ошибкой. При частичной доступности — отдача того, что доступно (например, метаданные без артефактов). |
| 4 | **Correlation everywhere** | `correlation_id` генерируется при первом запросе и пробрасывается через все sync и async вызовы. |
| 5 | **Tenant-first** | `organization_id` из JWT — обязательный контекст в каждом downstream-вызове. |
| 6 | **User-facing abstractions** | API оркестратора адаптирован для пользователя: «договоры» вместо «документов», user-friendly статусы, ошибки на русском языке. |
| 7 | **Defense in depth** | Валидация на каждом уровне: JWT → RBAC → input validation → rate limit → downstream validation (DM). |

---

# 5. Модель данных оркестратора

Оркестратор **не является source of truth** ни для каких бизнес-данных. Все persistent-данные хранятся в доменных сервисах. Оркестратор использует Redis для ephemeral state.

## 5.1 Redis — ephemeral state

### SSE Connection Registry

```
Key:    sse:org:{organization_id}:user:{user_id}
Type:   SET
Value:  {instance_id}:{connection_id}
TTL:    Auto-cleanup при disconnect
```

Используется для маршрутизации событий к нужному SSE-соединению через Redis Pub/Sub.

### SSE Pub/Sub Channel

```
Channel: sse:broadcast:{organization_id}
Payload: JSON {event_type, document_id, version_id, status, timestamp}
```

Каждый инстанс оркестратора подписан на channel'ы организаций, чьи пользователи к нему подключены. При получении RabbitMQ-события инстанс публикует в Redis channel; все инстансы с SSE-клиентами этой организации получают уведомление и пушат в SSE.

### Rate Limit Counters

```
Key:    rl:{organization_id}:{endpoint_class}
Type:   STRING (counter)
TTL:    1s (sliding window token bucket)
```

### Upload Tracking

```
Key:    upload:{correlation_id}
Type:   HASH {status, document_id, version_id, storage_key, created_at}
TTL:    1h
```

Отслеживание состояния multi-step upload процесса (upload → DM create → DP command). Используется для idempotency и для возврата статуса при повторном запросе.

## 5.2 Маппинг статусов

Оркестратор транслирует внутренние статусы в user-friendly формат:

| Внутренний статус (источник) | User Status | Описание для пользователя (рус.) |
|------------------------------|-------------|----------------------------------|
| Upload завершён, версия создана в DM | `UPLOADED` | Договор загружен |
| Команда опубликована в DP | `QUEUED` | В очереди на обработку |
| DP: `StatusChanged(IN_PROGRESS)` | `PROCESSING` | Извлечение текста и структуры |
| DM: `version-artifacts-ready` | `ANALYZING` | Юридический анализ |
| LIC: `StatusChanged(IN_PROGRESS)` | `ANALYZING` | Юридический анализ выполняется |
| LIC: `ClassificationUncertain` | `AWAITING_USER_INPUT` | Требуется подтверждение типа договора (FR-2.1.3) |
| HTTP: `POST /confirm-type` | `ANALYZING` | Подтверждение получено, анализ возобновлён |
| Watchdog: `AWAITING_USER_INPUT > ORCH_USER_CONFIRMATION_TIMEOUT` | `FAILED` (`USER_CONFIRMATION_TIMEOUT`) | Время на подтверждение истекло |
| DM: `version-analysis-ready` | `GENERATING_REPORTS` | Формирование отчётов |
| RE: `StatusChanged(IN_PROGRESS)` | `GENERATING_REPORTS` | Формирование отчётов выполняется |
| DM: `version-reports-ready` / `FULLY_READY` | `READY` | Результаты готовы |
| DM: `version-partially-available` | `PARTIALLY_FAILED` | Частично доступно (есть ошибки) |
| DP: `ProcessingFailed` | `FAILED` | Ошибка обработки |
| LIC: `StatusChanged(FAILED)` | `ANALYSIS_FAILED` | Ошибка юридического анализа |
| RE: `StatusChanged(FAILED)` | `REPORTS_FAILED` | Ошибка формирования отчётов |
| DP: `REJECTED` | `REJECTED` | Файл отклонён (формат/размер) |

---

# 6. Внутренние компоненты

## 6.1 Архитектура компонентов

```
+==========================================================================+
|                    API / Backend Orchestrator                             |
|                                                                          |
|  INGRESS (sync — HTTP)                                                   |
|  ~~~~~~~~~~~~~~~~~~~~~                                                   |
|  [HTTP Router] → [JWT Auth Middleware] → [RBAC Middleware]               |
|       → [Rate Limiter] → [Request Handlers]                             |
|                                                                          |
|  INGRESS (async — RabbitMQ)                                              |
|  ~~~~~~~~~~~~~~~~~~~~~~~~~~                                              |
|  [Event Consumer] → [Event Router]                                       |
|                                                                          |
|  INGRESS (real-time — SSE)                                               |
|  ~~~~~~~~~~~~~~~~~~~~~~~~~                                               |
|  [SSE Connection Manager] ← Redis Pub/Sub                               |
|                                                                          |
|  APPLICATION                                                             |
|  ~~~~~~~~~~~                                                             |
|  [Contract Upload Coordinator]                                           |
|  [Processing Status Tracker]                                             |
|  [Results Aggregator]                                                    |
|  [Comparison Coordinator]                                                |
|  [Re-check Coordinator]                                                  |
|  [Export Service]                                                        |
|  [Feedback Service]                                                      |
|  [Admin Proxy Service]                                                   |
|  [Type Confirmation Handler]                                             |
|  [Permissions Resolver]                                                  |
|                                                                          |
|  EGRESS                                                                  |
|  ~~~~~~                                                                  |
|  [DM Client]          — sync HTTP к DM REST API                          |
|  [Command Publisher]   — async команды в DP через RabbitMQ               |
|  [SSE Broadcaster]     — push событий через Redis Pub/Sub → SSE         |
|                                                                          |
|  INFRASTRUCTURE                                                          |
|  ~~~~~~~~~~~~~~                                                          |
|  [Object Storage Client]  (S3-compatible)                                |
|  [Redis Client]           (pub/sub, rate limits, upload tracking)        |
|  [Broker Client]          (RabbitMQ consumer + publisher)                |
|  [Observability SDK]      (logging, metrics, tracing)                    |
|  [Health Check Handler]   (/healthz, /readyz)                            |
+==========================================================================+
```

## 6.2 HTTP Router

**Назначение:** Точка входа для HTTP-запросов от frontend и внешних интеграций.

**Ответственность:**
- Маршрутизация HTTP-запросов к соответствующим Request Handlers.
- Middleware chain: CORS → JWT Auth → RBAC → Rate Limiter → Request Handler.
- Поддержка `multipart/form-data` для загрузки файлов.
- SSE endpoint (long-lived connection).

**Технология:** Go standard library `net/http` + `chi` router (совместимость с DM).

## 6.3 JWT Auth Middleware

**Назначение:** Аутентификация каждого HTTP-запроса.

**Ответственность:**
- Извлечение JWT из заголовка `Authorization: Bearer <token>`.
- Валидация подписи по публичному ключу (RSA/ECDSA).
- Проверка `exp`, `iat`, `nbf`.
- Извлечение claims: `user_id`, `organization_id`, `role`.
- Установка auth context в Go context для downstream handlers.
- Отклонение запроса (401) при невалидном/отсутствующем токене.

**Исключения (без JWT):** `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh`, `GET /healthz`, `GET /readyz`, `GET /metrics`.

## 6.4 RBAC Middleware

**Назначение:** Авторизация на основе роли пользователя.

**Ответственность:**
- Проверка роли (`LAWYER`, `BUSINESS_USER`, `ORG_ADMIN`) против требований endpoint.
- R-1 (юрист) — полный доступ к результатам, экспорту.
- R-2 (бизнес-пользователь) — доступ к сводке, ограниченный доступ к деталям.
- R-3 (администратор) — доступ к настройкам политик + все права R-1.
- При недостаточных правах — 403 Forbidden.

**Матрица доступа:**

| Endpoint | LAWYER | BUSINESS_USER | ORG_ADMIN |
|----------|--------|---------------|-----------|
| Upload contract | ✓ | ✓ | ✓ |
| List/get contracts | ✓ | ✓ | ✓ |
| Get results (full) | ✓ | — | ✓ |
| Get summary | ✓ | ✓ | ✓ |
| Export report | ✓ | по политике | ✓ |
| Compare versions | ✓ | — | ✓ |
| Re-check | ✓ | — | ✓ |
| Feedback | ✓ | ✓ | ✓ |
| Admin (policies) | — | — | ✓ |
| Archive/delete | ✓ | — | ✓ |

## 6.5 Rate Limiter Middleware

**Назначение:** Защита от перегрузки на уровне организации.

**Ответственность:**
- Token bucket per organization: раздельные лимиты для read (GET) и write (POST/PUT/DELETE).
- При превышении — 429 Too Many Requests с заголовком `Retry-After`.
- Хранение состояния — Redis.

## 6.6 Event Consumer

**Назначение:** Получение асинхронных событий из RabbitMQ.

**Ответственность:**
- Подписка на топики DP, LIC, RE и DM.
- Десериализация JSON.
- Маршрутизация к Processing Status Tracker.
- ACK после успешной обработки.

**Подписки:**

| Топик | Событие | Описание |
|-------|---------|----------|
| `dp.events.status-changed` | StatusChangedEvent | Изменение статуса задачи DP |
| `dp.events.processing-completed` | ProcessingCompletedEvent | DP обработка завершена |
| `dp.events.processing-failed` | ProcessingFailedEvent | DP обработка провалилась |
| `dp.events.comparison-completed` | ComparisonCompletedEvent | DP сравнение завершено |
| `dp.events.comparison-failed` | ComparisonFailedEvent | DP сравнение провалилось |
| `lic.events.status-changed` | LICStatusChangedEvent | Изменение статуса юридического анализа LIC (ASSUMPTION-ORCH-13) |
| `re.events.status-changed` | REStatusChangedEvent | Изменение статуса формирования отчётов RE (ASSUMPTION-ORCH-13) |
| `dm.events.version-artifacts-ready` | VersionProcessingArtifactsReady | Артефакты DP сохранены в DM |
| `dm.events.version-analysis-ready` | VersionAnalysisArtifactsReady | Результаты LIC сохранены |
| `dm.events.version-reports-ready` | VersionReportsReady | Отчёты RE сохранены |
| `dm.events.version-partially-available` | VersionPartiallyAvailable | Частичная ошибка |
| `dm.events.version-created` | VersionCreated | Новая версия создана |

## 6.7 SSE Connection Manager

**Назначение:** Управление long-lived SSE-соединениями для доставки статусных обновлений в реальном времени.

**Ответственность:**
- Приём SSE-соединения от frontend (`GET /api/v1/events/stream`).
- Аутентификация через JWT (query parameter `token` или заголовок).
- Регистрация соединения в Redis (SSE Connection Registry).
- Подписка на Redis Pub/Sub channel организации.
- Push событий клиенту в формате SSE (`data: JSON\n\n`).
- Heartbeat (`:ping\n\n`) каждые 15 секунд для поддержания соединения.
- Cleanup при disconnect.

**Формат SSE-события:**

```
event: status_update
data: {"document_id":"uuid","version_id":"uuid","status":"ANALYZING","message":"Юридический анализ","timestamp":"2026-04-06T12:00:00Z"}

```

## 6.8 Contract Upload Coordinator

**Назначение:** Координация multi-step процесса загрузки договора.

**Ответственность:**
1. Валидация файла (размер ≤ 20 МБ, MIME-тип `application/pdf`).
2. Генерация `correlation_id` (UUID v4).
3. Загрузка файла в Object Storage → получение `storage_key`.
4. Вычисление SHA-256 checksum файла.
5. Вызов DM: `POST /documents` → создание Document (если новый).
6. Вызов DM: `POST /documents/{id}/versions` с `source_file_key`, `origin_type=UPLOAD`.
7. Генерация `job_id` (UUID v4).
8. Публикация `ProcessDocumentRequested` в `dp.commands.process-document`.
9. Сохранение upload tracking в Redis.
10. Возврат HTTP 202 Accepted с `document_id`, `version_id`, `job_id`.

**Входы:** multipart/form-data (file + title).
**Выходы:** HTTP 202 + async processing.
**Зависимости:** Object Storage Client, DM Client, Command Publisher, Redis Client.

**Обработка ошибок:**
- Ошибка S3 upload → cleanup, retry до 3 раз, 502 при исчерпании.
- Ошибка DM → cleanup uploaded file, 502.
- Ошибка RabbitMQ publish → retry до 3 раз. При исчерпании — версия уже создана в DM со статусом PENDING; команда будет отправлена при retry (оператором или автоматически). Логирование CRITICAL.

## 6.9 Processing Status Tracker

**Назначение:** Отслеживание статуса обработки и доставка обновлений через SSE.

**Ответственность:**
1. Приём событий от Event Consumer (DP, LIC, RE и DM).
2. Маппинг внутреннего статуса → user-friendly статус (таблица из раздела 5.2).
3. Публикация SSE-события через SSE Broadcaster.
4. Обработка финальных статусов (READY, FAILED, ANALYSIS_FAILED, REPORTS_FAILED, PARTIALLY_FAILED).
5. Мгновенное обнаружение сбоев LIC/RE через `lic.events.status-changed(FAILED)` и `re.events.status-changed(FAILED)` — без ожидания DM Watchdog (ASSUMPTION-ORCH-13).

**Входы:** Десериализованные RabbitMQ-события.
**Выходы:** SSE-события через Redis Pub/Sub.
**Зависимости:** SSE Broadcaster, Redis Client.

## 6.10 Results Aggregator

**Назначение:** Агрегация данных из DM для формирования user-friendly ответов.

**Ответственность:**
1. Получение метаданных версии из DM.
2. Получение артефактов (RISK_ANALYSIS, RISK_PROFILE, SUMMARY, RECOMMENDATIONS, KEY_PARAMETERS, CLASSIFICATION_RESULT, AGGREGATE_SCORE) из DM.
3. Фильтрация по роли пользователя (R-2 видит только SUMMARY и AGGREGATE_SCORE).
4. Формирование единого агрегированного ответа для frontend.

**Входы:** HTTP request + auth context.
**Выходы:** Агрегированный JSON response.
**Зависимости:** DM Client.

## 6.11 Comparison Coordinator

**Назначение:** Координация запуска сравнения версий.

**Ответственность:**
1. Валидация: обе версии принадлежат одному документу и одной организации (через DM).
2. Генерация `job_id`, `correlation_id`.
3. Публикация `CompareDocumentVersionsRequested` в `dp.commands.compare-versions`.
4. Возврат HTTP 202 Accepted.

**Входы:** HTTP request (base_version_id, target_version_id).
**Выходы:** HTTP 202 + async processing.
**Зависимости:** DM Client (валидация), Command Publisher.

## 6.12 Re-check Coordinator

**Назначение:** Координация повторной проверки версии.

**Ответственность:**
1. Получение метаданных текущей версии из DM (для `source_file_key`).
2. Создание новой версии в DM с `origin_type=RE_CHECK`, тем же `source_file_key`.
3. Генерация `job_id`, `correlation_id`.
4. Публикация `ProcessDocumentRequested` в `dp.commands.process-document`.
5. Возврат HTTP 202 Accepted.

**Входы:** HTTP request (version_id).
**Выходы:** HTTP 202 + async processing.
**Зависимости:** DM Client, Command Publisher.

## 6.13 Export Service

**Назначение:** Координация экспорта отчётов.

**Ответственность:**
1. Запрос артефакта `EXPORT_PDF` или `EXPORT_DOCX` из DM.
2. DM возвращает 302 Redirect на presigned S3 URL (для blob-артефактов).
3. Оркестратор проксирует 302 клиенту (или отдаёт presigned URL в JSON).

**Входы:** HTTP request (version_id, format).
**Выходы:** HTTP 302 (redirect) или JSON с URL.
**Зависимости:** DM Client.

## 6.14 Feedback Service

**Назначение:** Приём и сохранение пользовательской обратной связи (UR-11).

**Ответственность:**
1. Приём feedback (полезность: boolean, комментарий: string).
2. Валидация: version_id существует и принадлежит организации.
3. Сохранение feedback (ASSUMPTION-ORCH-08).
4. Возврат HTTP 201 Created.

**Входы:** HTTP request (version_id, is_useful, comment).
**Выходы:** HTTP 201.
**Зависимости:** DM Client (валидация), Redis Client (fallback storage).

## 6.15 Admin Proxy Service

**Назначение:** Проксирование административных запросов в OPM.

**Ответственность:**
1. Проверка роли: только `ORG_ADMIN`.
2. Проксирование запросов к OPM с `organization_id` из JWT.
3. Возврат ответа OPM клиенту.

**Входы:** HTTP request от R-3.
**Выходы:** Проксированный ответ OPM.
**Зависимости:** OPM HTTP Client.

## 6.15a Type Confirmation Handler

**Назначение:** Обработка цикла подтверждения типа договора при низкой уверенности классификации (UR-3 / FR-2.1.3 — сценарий 8.11).

**Ответственность:**
1. Подписка на `lic.events.classification-uncertain` (через Event Consumer): перевод версии в `AWAITING_USER_INPUT`, запуск watchdog-таймера, публикация SSE-события `type_confirmation_required`.
2. HTTP-handler для `POST /contracts/{id}/versions/{vid}/confirm-type`: проверка состояния (Redis), валидация `contract_type` против whitelist LIC, публикация команды `UserConfirmedType` в LIC, перевод версии в `ANALYZING`, идемпотентность по `version_id` (Redis ключ `orch-user-confirmed-type:{version_id}`, TTL 60с).
3. Watchdog: периодическое сканирование Redis ключей `confirmation:wait:{version_id}` с истёкшим TTL → перевод соответствующих версий в `FAILED` с error_code `USER_CONFIRMATION_TIMEOUT`, SSE push.

**Входы:** HTTP request от R-1/R-3, событие `lic.events.classification-uncertain`, истечение Redis TTL.
**Выходы:** HTTP 202 / 400 / 409 клиенту; команда `UserConfirmedType` в LIC; SSE push (`type_confirmation_required`, `status_update`).
**Зависимости:** Processing Status Tracker, Command Publisher, SSE Broadcaster, Redis Client.

## 6.16 DM Client

**Назначение:** Sync HTTP-клиент для DM REST API.

**Ответственность:**
- Выполнение HTTP-запросов к DM API (`/api/v1/documents`, `/api/v1/documents/{id}/versions`, и т.д.).
- Передача auth context (`organization_id`, `user_id`) в заголовках.
- Retry с exponential backoff при транзиентных ошибках (5xx, timeout).
- Circuit breaker для защиты от каскадных отказов.
- Timeout per request: 5s (read), 10s (write).

## 6.17 Command Publisher

**Назначение:** Публикация команд для DP через RabbitMQ.

**Ответственность:**
- Сериализация команд в JSON с EventMeta envelope.
- Публикация в топики `dp.commands.process-document` и `dp.commands.compare-versions`.
- Publisher confirms для гарантии доставки.
- Retry при ошибке (до 3 раз).

## 6.18 SSE Broadcaster

**Назначение:** Broadcast статусных обновлений через Redis Pub/Sub к SSE-соединениям.

**Ответственность:**
- Публикация события в Redis Pub/Sub channel `sse:broadcast:{organization_id}`.
- Подписка на Redis channels для организаций с активными SSE-соединениями.
- Доставка событий в SSE Connection Manager.

## 6.19 Object Storage Client

**Назначение:** Загрузка файлов в Yandex Object Storage (S3-compatible).

**Ответственность:**
- Streaming upload (не буферизация в память).
- PutObject с content-type, checksum.
- DeleteObject для cleanup при ошибке.
- Retry при транзиентных ошибках.

## 6.20 Health Check Handler

**Назначение:** HTTP endpoints для liveness и readiness probes.

**Ответственность:**
- `/healthz` — liveness: процесс жив. Всегда 200.
- `/readyz` — readiness: Redis + RabbitMQ + DM (HTTP ping) доступны. 200/503.
- `/metrics` — Prometheus metrics endpoint.

## 6.21 Permissions Resolver

**Назначение:** Computed-флаги разрешений пользователя для frontend (`UserProfile.permissions` в `GET /users/me`). Frontend не интерпретирует raw policy из OPM — потребляет готовые boolean'ы.

**Ответственность:**
1. При запросе `GET /users/me`: после получения профиля из UOM — собрать `UserPermissions`-структуру.
2. Для каждого флага определить значение по правилу: `role-based default` → (для условных) `OPM policy lookup` → (при недоступности OPM) `env fallback`.
3. Кешировать computed permissions per `(organization_id, role)` в Redis с TTL `ORCH_PERMISSIONS_CACHE_TTL` (default 5 мин). Инвалидация — при `PUT /admin/policies/{id}` (Admin Proxy Service публикует событие в Redis Pub/Sub `permissions:invalidate:{org_id}`).
4. **Non-blocking при OPM down:** если OPM недоступен и кеш пустой — вернуть fallback-значения с WARN-логом. `GET /users/me` не блокируется на OPM.

**Текущие флаги (v1):**

| Флаг | LAWYER | BUSINESS_USER | ORG_ADMIN |
|------|--------|---------------|-----------|
| `export_enabled` | `true` (безусловно) | OPM policy `business_user_export` → fallback `ORCH_OPM_FALLBACK_BUSINESS_USER_EXPORT` (default `false`) | `true` (безусловно) |

**Расчёт `export_enabled` для BUSINESS_USER:**
1. Cache lookup: Redis `permissions:{org_id}:BUSINESS_USER` → если hit, вернуть `cached.export_enabled`.
2. Cache miss: OPM Client `GET /api/v1/policies?organization_id={org_id}` (с timeout 2с, circuit breaker).
3. OPM success: найти policy `name == "business_user_export"`, прочитать `enabled` boolean. Записать в Redis с TTL.
4. OPM failure (timeout, 5xx, circuit open): использовать `ORCH_OPM_FALLBACK_BUSINESS_USER_EXPORT`. Не кешировать (чтобы при восстановлении OPM получить актуальное значение). WARN-лог с `correlation_id`.

**Входы:** запрос `GET /users/me` (после JWT auth + UOM proxy).
**Выходы:** `UserPermissions` JSON.
**Зависимости:** OPM Client (sync HTTP), Redis Client (cache + pub/sub).

**Метрики:**
- `orch_permissions_cache_hit_total{flag, org_id_hash}` — counter
- `orch_permissions_opm_fallback_total{flag, reason}` — counter (`reason` ∈ `timeout, opm_unavailable, circuit_open, no_policy`)

---

# 7. Архитектура сервиса

Оркестратор реализуется как **один Go-сервис** (Monolith), обрабатывающий HTTP-запросы, SSE-соединения и async-события через RabbitMQ. Внутри — слоёная архитектура с чётким разделением ответственности.

> Анализ вариантов — см. ADR-1.

### Диаграмма сервиса

```
                     ┌──────────────────────────────────┐
                     │       Frontend / External API     │
                     └─────────┬───────────┬────────────┘
                               │           │
                      HTTPS    │           │  SSE (HTTPS)
                      (REST)   │           │
┌──────────────────────────────┴───────────┴────────────────────────────┐
│                     API / Backend Orchestrator                        │
│                                                                      │
│  ┌────────────────────────────────────────────────────────┐          │
│  │  HTTP Router + Middleware Chain                         │          │
│  │  (CORS → JWT Auth → RBAC → Rate Limiter)              │          │
│  └────┬──────────────────────────────────┬───────────────┘          │
│       │                                  │                           │
│       │  REST handlers                   │  SSE handler              │
│       ▼                                  ▼                           │
│  ┌──────────────────┐          ┌─────────────────────┐              │
│  │ Request Handlers  │          │ SSE Connection Mgr  │              │
│  │ (upload, results, │          │ (long-lived conns)  │              │
│  │  compare, admin)  │          └────────┬────────────┘              │
│  └────────┬─────────┘                   │                           │
│           │                              │ Redis Pub/Sub             │
│           ▼                              ▼                           │
│  ┌──────────────────────────────────────────────────────┐           │
│  │                Application Services                   │           │
│  │                                                       │           │
│  │  • Contract Upload Coordinator                        │           │
│  │  • Processing Status Tracker                          │           │
│  │  • Results Aggregator                                 │           │
│  │  • Comparison Coordinator                             │           │
│  │  • Re-check Coordinator                               │           │
│  │  • Export Service                                     │           │
│  │  • Feedback Service                                   │           │
│  │  • Admin Proxy Service                                │           │
│  └──────────┬─────────────────────┬──────────────────────┘           │
│             │                     │                                   │
│             ▼                     ▼                                   │
│  ┌──────────────────┐  ┌──────────────────────────┐                 │
│  │ Egress Clients    │  │ Event Consumer           │                 │
│  │ • DM Client       │  │ (RabbitMQ → Status       │                 │
│  │ • Command Pub     │  │  Tracker → SSE)          │                 │
│  │ • OPM Client      │  └──────────┬───────────────┘                 │
│  │ • UOM Client      │             │                                  │
│  └──────┬───────────┘             │                                  │
│         │                          │                                  │
│  INFRASTRUCTURE                    │                                  │
│  ┌─────────────────────────────────┴────────────────────┐            │
│  │ Redis │ RabbitMQ │ Object Storage │ Observability SDK │            │
│  │ (SSE, │ (events, │ (file upload)  │ (logs, metrics,   │            │
│  │  RL)  │  cmds)   │                │  traces)          │            │
│  └───────┴──────────┴────────────────┴───────────────────┘            │
│                                                                      │
│  CROSS-CUTTING: Health Check Handler                                 │
└──────────────────────────────────────────────────────────────────────┘
```

---

# 8. Сценарии работы

Sequence diagrams для каждого сценария — см. [sequence-diagrams.md](sequence-diagrams.md).

## 8.1 Загрузка договора и запуск проверки (UR-1, UR-2)

**Trigger:** Пользователь загружает PDF через UI.

### Happy path

1. Frontend: `POST /api/v1/contracts/upload` (multipart/form-data: file + title).
2. HTTP Router → JWT Auth Middleware → RBAC → Rate Limiter.
3. Contract Upload Coordinator:
   a. Валидация файла: размер ≤ 20 МБ, MIME-тип `application/pdf`.
   b. Генерация `correlation_id` (UUID v4).
   c. Streaming upload файла в Object Storage → `storage_key = uploads/{org_id}/{uuid}/{filename}`.
   d. Вычисление SHA-256 checksum.
   e. DM Client: `POST /api/v1/documents` с `{title}` → `document_id`. (Если загрузка новой версии существующего документа — пропускается, `document_id` из URL.)
   f. DM Client: `POST /api/v1/documents/{document_id}/versions` с `{source_file_key, source_file_name, source_file_size, source_file_checksum, origin_type=UPLOAD}` → `version_id`, `version_number`.
   g. Генерация `job_id` (UUID v4).
   h. Command Publisher: публикация `ProcessDocumentRequested` в `dp.commands.process-document` с `{correlation_id, job_id, document_id, version_id, organization_id, requested_by_user_id, source_file_key}`.
   i. Redis: сохранение upload tracking `{correlation_id, document_id, version_id, job_id, status=QUEUED}`.
4. HTTP 202 Accepted: `{document_id, version_id, job_id, status: "QUEUED"}`.

### Альтернативные ветки

**Файл слишком большой / неверный формат:** → HTTP 400 с `{error_code: "FILE_TOO_LARGE" | "UNSUPPORTED_FORMAT", message: "Файл превышает максимальный размер 20 МБ" | "Поддерживается только формат PDF"}`.

**Object Storage недоступен:** → Retry 3 раза. При исчерпании → HTTP 502 с `{error_code: "STORAGE_UNAVAILABLE", message: "Сервис временно недоступен. Попробуйте через несколько минут."}`.

**DM недоступен:** → Cleanup: удаление файла из S3. HTTP 502 с `{error_code: "SERVICE_UNAVAILABLE"}`.

**DM вернул 409 (документ archived/deleted):** → HTTP 409 с проксированной ошибкой DM.

**RabbitMQ publish failed:** → Retry 3 раза. При исчерпании → версия уже создана в DM (PENDING). Логирование CRITICAL. HTTP 202 (пользователь получит документ в статусе PENDING; оператор может переотправить команду).

## 8.2 Получение статуса обработки (SSE + polling fallback)

**Trigger:** Frontend подключается к SSE после загрузки.

### Happy path (SSE)

1. Frontend: `GET /api/v1/events/stream` с JWT.
2. SSE Connection Manager: валидация JWT, регистрация соединения в Redis.
3. Подписка на Redis Pub/Sub channel `sse:broadcast:{org_id}`.
4. Ожидание событий.
5. Event Consumer получает `dp.events.status-changed` (IN_PROGRESS) → Processing Status Tracker → SSE Broadcaster → Redis Pub/Sub → SSE Connection Manager → push в SSE: `event: status_update\ndata: {status: "PROCESSING", ...}\n\n`.
6. Event Consumer получает `dm.events.version-artifacts-ready` → Status Tracker → SSE push: `{status: "ANALYZING"}`.
7. Event Consumer получает `dm.events.version-analysis-ready` → SSE push: `{status: "GENERATING_REPORTS"}`.
8. Event Consumer получает `dm.events.version-reports-ready` → SSE push: `{status: "READY"}`.

### Polling fallback

1. Frontend: `GET /api/v1/contracts/{id}/versions/{vid}/status`.
2. Оркестратор: DM Client `GET /documents/{id}/versions/{vid}` → получение `artifact_status`.
3. Маппинг `artifact_status` → user status.
4. HTTP 200: `{status: "ANALYZING", message: "Юридический анализ", updated_at: "..."}`.

### Альтернативные ветки

**SSE disconnect:** → Клиент автоматически переподключается (встроено в EventSource API). При 3 неудачных попытках — переключение на polling.

**Событие пришло на другой инстанс:** → Redis Pub/Sub broadcast. Все инстансы получают событие.

## 8.3 Получение результатов проверки (UR-4, UR-5, UR-6, UR-7)

**Trigger:** Пользователь открывает результаты проверки.

### Happy path

1. Frontend: `GET /api/v1/contracts/{id}/versions/{vid}/results`.
2. JWT Auth → RBAC.
3. Results Aggregator:
   a. DM Client: `GET /documents/{id}/versions/{vid}` → метаданные версии (artifact_status, version_number, origin_type).
   b. Проверка `artifact_status ∈ {REPORTS_READY, FULLY_READY}`. Если нет — HTTP 200 с `{status: "PROCESSING", available_data: null}`.
   c. DM Client: `GET /documents/{id}/versions/{vid}/artifacts/RISK_ANALYSIS` → риски.
   d. DM Client: `GET /documents/{id}/versions/{vid}/artifacts/RISK_PROFILE` → риск-профиль.
   e. DM Client: `GET /documents/{id}/versions/{vid}/artifacts/SUMMARY` → резюме.
   f. DM Client: `GET /documents/{id}/versions/{vid}/artifacts/RECOMMENDATIONS` → рекомендации.
   g. DM Client: `GET /documents/{id}/versions/{vid}/artifacts/KEY_PARAMETERS` → ключевые параметры.
   h. DM Client: `GET /documents/{id}/versions/{vid}/artifacts/CLASSIFICATION_RESULT` → тип договора.
   i. DM Client: `GET /documents/{id}/versions/{vid}/artifacts/AGGREGATE_SCORE` → сводная оценка.
   j. Фильтрация по роли: R-2 видит только `SUMMARY`, `AGGREGATE_SCORE`, `KEY_PARAMETERS`.
4. HTTP 200: агрегированный JSON.

**Оптимизация:** Параллельные запросы к DM для разных артефактов (goroutines).

### Альтернативные ветки

**Версия в статусе PENDING/PROCESSING:** → HTTP 200 с `{status: "PROCESSING", available_data: null}`. Не ошибка — результаты ещё не готовы.

**Версия в статусе PARTIALLY_AVAILABLE:** → HTTP 200 с доступными артефактами + `{status: "PARTIALLY_FAILED", available_data: {...}, error: "Часть анализа не была завершена"}`.

**Артефакт не найден в DM (404):** → Пропуск артефакта в ответе (не ошибка — артефакт может отсутствовать).

## 8.4 Повторная проверка версии (UR-9)

**Trigger:** Пользователь запрашивает повторную проверку.

### Happy path

1. Frontend: `POST /api/v1/contracts/{id}/versions/{vid}/recheck`.
2. JWT Auth → RBAC (LAWYER, ORG_ADMIN).
3. Re-check Coordinator:
   a. DM Client: `GET /documents/{id}/versions/{vid}` → `source_file_key`, `source_file_name`, `source_file_size`, `source_file_checksum`.
   b. DM Client: `POST /documents/{id}/versions` с `{source_file_key, origin_type=RE_CHECK, parent_version_id=vid}` → новые `version_id`, `version_number`.
   c. Генерация `job_id`, `correlation_id`.
   d. Command Publisher: `ProcessDocumentRequested` в DP.
4. HTTP 202 Accepted: `{document_id, version_id: new_vid, job_id, status: "QUEUED"}`.

### Альтернативные ветки

**Исходная версия ещё обрабатывается (PENDING):** → HTTP 409 Conflict: `"Дождитесь завершения текущей обработки"`.

## 8.5 Сравнение версий (FR-5.3.1)

**Trigger:** Пользователь запрашивает сравнение двух версий.

### Happy path

1. Frontend: `POST /api/v1/contracts/{id}/compare` с `{base_version_id, target_version_id}`.
2. JWT Auth → RBAC (LAWYER, ORG_ADMIN).
3. Comparison Coordinator:
   a. DM Client: `GET /documents/{id}/versions/{base_vid}` — валидация существования.
   b. DM Client: `GET /documents/{id}/versions/{target_vid}` — валидация существования.
   c. Проверка: обе версии принадлежат одному документу.
   d. Генерация `job_id`, `correlation_id`.
   e. Command Publisher: `CompareDocumentVersionsRequested` в `dp.commands.compare-versions`.
4. HTTP 202 Accepted: `{job_id, status: "QUEUED"}`.

### Получение результата сравнения

1. Frontend: `GET /api/v1/contracts/{id}/versions/{base_vid}/diff/{target_vid}`.
2. Results Aggregator:
   a. DM Client: `GET /documents/{id}/diffs/{base_vid}/{target_vid}`.
   b. Если diff найден — HTTP 200 с результатом.
   c. Если diff не найден — HTTP 404 (ещё не готов или не запускался).

## 8.6 Экспорт отчёта (UR-10)

**Trigger:** Пользователь запрашивает экспорт.

### Happy path

1. Frontend: `GET /api/v1/contracts/{id}/versions/{vid}/export/pdf` (или `/docx`).
2. JWT Auth → RBAC.
3. Export Service:
   a. DM Client: `GET /documents/{id}/versions/{vid}/artifacts/EXPORT_PDF`.
   b. DM возвращает 302 Redirect на presigned S3 URL.
   c. Оркестратор проксирует 302 клиенту.
4. Frontend получает presigned URL → браузер скачивает файл.

### Альтернативные ветки

**Отчёт ещё не готов:** → DM вернёт 404. Оркестратор → HTTP 404: `"Отчёт ещё формируется. Попробуйте позже."`.

## 8.7 Управление документами (list, get, archive, delete)

### Список документов

1. Frontend: `GET /api/v1/contracts?page=1&size=20&status=ACTIVE`.
2. Results Aggregator → DM Client: `GET /documents?page=1&size=20&status=ACTIVE`.
3. HTTP 200 с пагинированным списком.

### Получение документа

1. Frontend: `GET /api/v1/contracts/{id}`.
2. DM Client: `GET /documents/{id}` → DocumentWithCurrentVersion.
3. HTTP 200 с агрегированными данными (документ + текущая версия + user-friendly статус).

### Архивация

1. Frontend: `POST /api/v1/contracts/{id}/archive`.
2. DM Client: `POST /documents/{id}/archive`.
3. HTTP 200.

### Soft delete

1. Frontend: `DELETE /api/v1/contracts/{id}`.
2. DM Client: `DELETE /documents/{id}`.
3. HTTP 200.

## 8.8 Обратная связь (UR-11)

### Happy path

1. Frontend: `POST /api/v1/contracts/{id}/versions/{vid}/feedback` с `{is_useful: true, comment: "Полезный анализ"}`.
2. JWT Auth.
3. Feedback Service:
   a. Валидация: version_id существует (DM Client: `GET /documents/{id}/versions/{vid}`).
   b. Сохранение feedback (ASSUMPTION-ORCH-08).
4. HTTP 201 Created.

## 8.9 Настройки строгости администратором (UR-12)

### Happy path

1. Frontend: `GET /api/v1/admin/policies`.
2. JWT Auth → RBAC (только ORG_ADMIN).
3. Admin Proxy Service → OPM Client: `GET /api/v1/policies?organization_id={org_id}`.
4. HTTP 200 с политиками.

### Обновление политики

1. Frontend: `PUT /api/v1/admin/policies/{policy_id}`.
2. JWT Auth → RBAC (ORG_ADMIN).
3. Admin Proxy Service → OPM Client: `PUT /api/v1/policies/{policy_id}` с `organization_id`.
4. HTTP 200.

### Альтернативные ветки

**OPM недоступен:** → HTTP 502: `"Сервис настроек временно недоступен"`.

## 8.10 Загрузка новой версии существующего документа

### Happy path

1. Frontend: `POST /api/v1/contracts/{id}/versions/upload` (multipart/form-data: file).
2. Contract Upload Coordinator (аналогично 8.1, но пропускает создание Document):
   a. Валидация файла.
   b. Upload в S3.
   c. DM Client: `POST /documents/{id}/versions` с `origin_type=RE_UPLOAD`.
   d. Публикация `ProcessDocumentRequested`.
3. HTTP 202 Accepted.

## 8.11 Подтверждение типа договора при низкой уверенности (UR-3 / FR-2.1.3)

**Trigger:** LIC в процессе анализа определил, что `ClassificationResult.confidence < threshold` (порог конфигурируется LIC-side). Pipeline LIC приостановлен в ожидании выбора пользователя.

### Happy path

1. LIC публикует `ClassificationUncertain` в `lic.events.classification-uncertain` с `{suggested_type, confidence, threshold, alternatives[]}`.
2. Event Consumer → Type Confirmation Handler:
   a. Валидация envelope (correlation_id, version_id, organization_id).
   b. Processing Status Tracker: установка статуса `AWAITING_USER_INPUT` в Redis.
   c. Запуск watchdog-таймера на `ORCH_USER_CONFIRMATION_TIMEOUT` (default 24h, ключ `confirmation:wait:{version_id}` в Redis с TTL).
   d. SSE Broadcaster: публикация события `type_confirmation_required` в Redis Pub/Sub channel `sse:broadcast:{org_id}` с payload `{document_id, version_id, status: "AWAITING_USER_INPUT", suggested_type, confidence, threshold, alternatives}`.
3. Frontend получает SSE-событие → отображает пользователю модалку выбора типа.
4. Frontend: `POST /api/v1/contracts/{id}/versions/{vid}/confirm-type` с `{contract_type, confirmed_by_user: true}`.
5. JWT Auth → RBAC (LAWYER, ORG_ADMIN — см. матрицу RBAC).
6. Type Confirmation Handler:
   a. Чтение статуса из Redis. Если `status != AWAITING_USER_INPUT` → HTTP 409 `VERSION_NOT_AWAITING_INPUT`.
   b. Валидация `contract_type` (whitelist, контракт LIC). Невалидно → HTTP 400 `VALIDATION_ERROR`.
   c. Загрузка оригинального `correlation_id` и `job_id` из upload tracking (Redis).
   d. Command Publisher: публикация `UserConfirmedType` в `orch.commands.user-confirmed-type` с `{correlation_id, job_id, document_id, version_id, organization_id, confirmed_by_user_id, contract_type}`.
   e. Processing Status Tracker: статус → `ANALYZING`. Снятие watchdog-таймера.
   f. SSE push: `{status: "ANALYZING", message: "Анализ возобновлён"}`.
7. HTTP 202 Accepted: `{document_id, version_id, status: "ANALYZING"}`.
8. LIC получает `UserConfirmedType` → возобновляет анализ → дальше как с шага 14 happy path сценария 8.2.

### Альтернативные ветки

**Версия не в `AWAITING_USER_INPUT`** (уже подтверждена другим пользователем, истёк таймаут или ещё анализируется): → HTTP 409 `VERSION_NOT_AWAITING_INPUT`: `"Подтверждение типа уже не требуется или ещё рано"`. UI должен синхронизировать статус через polling/SSE.

**Невалидный `contract_type`** (вне whitelist LIC): → HTTP 400 `VALIDATION_ERROR` с `details.fields: [{field: "contract_type", code: "NOT_IN_WHITELIST"}]`.

**Истёк таймаут подтверждения:** Watchdog обнаруживает истёкший ключ Redis (через периодический сканер или Redis Keyspace Notifications). Действия:
1. Processing Status Tracker: статус → `FAILED` с error_code `USER_CONFIRMATION_TIMEOUT`.
2. Command Publisher: публикация `UserConfirmedType` НЕ выполняется. Вместо этого — внутренняя нотификация LIC через отдельный механизм (TBD: либо «taimout» событие, либо LIC сам отслеживает свой timeout). В v1 — LIC может бесконечно держать состояние, оркестратор ему ничего не сообщает (LIC очистит состояние по своему собственному TTL).
3. SSE push: `{status: "FAILED", error_code: "USER_CONFIRMATION_TIMEOUT", message: "Время на подтверждение типа договора истекло"}`.

**RBAC: BUSINESS_USER** пытается подтвердить тип: → HTTP 403 `FORBIDDEN`. По умолчанию подтверждение типа доступно только LAWYER и ORG_ADMIN (бизнес-пользователь может загружать, но юридические решения принимает юрист).

**Дублирующий вызов** (двойной клик пользователя): Idempotency через ключ `orch-user-confirmed-type:{version_id}` в Redis (TTL 60с). Повторный вызов в окне идемпотентности → HTTP 202 с теми же данными, без повторной публикации команды.

---

# 9. ADR (Architectural Decision Records)

## ADR-1: Monolith Orchestrator Service

**Контекст:** Нужно выбрать архитектурный стиль для оркестратора.

**Варианты:**

| Вариант | Описание | Плюсы | Минусы |
|---------|----------|-------|--------|
| A. Monolith | Один Go-сервис: HTTP + SSE + RabbitMQ consumer | Простота деплоя, единая кодовая база, минимум latency. | Масштабирование только целиком. |
| B. BFF (Backend for Frontend) | BFF для frontend + отдельный Orchestrator | Разделение UI-concerns и координации. | Лишний hop, двойная инфраструктура, overengineering для текущего масштаба. |
| C. API Gateway + Orchestrator | Kong/Envoy → отдельный Orchestrator service | Gateway берёт на себя auth, rate limiting. | Операционная сложность, дополнительная инфраструктура. |

**Решение:** Вариант A — Monolith Orchestrator Service.

**Обоснование:**
1. Нагрузка (~1000 договоров/сутки, ~50 RPS на API) не требует разделения.
2. Единый Go-сервис проще в разработке, тестировании и деплое.
3. Горизонтальное масштабирование достигается запуском нескольких инстансов за load balancer.
4. При росте нагрузки в 10–50× можно выделить SSE-handler в отдельный сервис без изменения API.
5. Консистентно с подходом в DM (Monolith DM Service).

## ADR-2: REST API для frontend

**Контекст:** Нужно выбрать протокол для frontend API.

**Варианты:**

| Вариант | Плюсы | Минусы |
|---------|-------|--------|
| REST + JSON | Стандарт, простота, NFR-6.2, OpenAPI. | Overfetching / underfetching. |
| GraphQL | Гибкие запросы, один endpoint. | Сложность кэширования, файловая загрузка через REST всё равно, новый стек. |
| gRPC | Высокая производительность, строгие контракты. | Плохая поддержка в браузерах (нужен gRPC-Web), сложнее для frontend. |

**Решение:** REST + JSON.

**Обоснование:**
1. NFR-6.2 явно требует REST API.
2. DM уже реализует REST API — единообразие.
3. OpenAPI 3.0 спецификация для документации и кодогенерации.
4. Загрузка файлов (multipart) — нативная для REST.
5. SSE — нативный для HTTP.
6. При необходимости отдельные high-traffic endpoints можно добавить через gRPC позже.

## ADR-3: SSE для real-time статусов

**Контекст:** Нужен механизм доставки статусных обновлений от сервера к клиенту.

**Варианты:**

| Вариант | Плюсы | Минусы |
|---------|-------|--------|
| WebSocket | Full-duplex, широкая поддержка. | Overengineering (нужен только server→client), сложнее scaling, connection management. |
| SSE | Нативный auto-reconnect, HTTP/2, простой протокол. | Unidirectional (server→client), ограничение 6 connections/domain в HTTP/1.1. |
| Long polling | Простейшая реализация, работает везде. | Высокий latency, нагрузка на сервер. |

**Решение:** SSE (Server-Sent Events) с Redis Pub/Sub для horizontal scaling.

**Обоснование:**
1. Статусные обновления — unidirectional (server→client). Full-duplex WebSocket избыточен.
2. SSE поддерживает auto-reconnect из коробки (EventSource API в браузере).
3. HTTP/2 снимает ограничение на 6 connections (мультиплексирование).
4. SSE проще WebSocket: нет handshake, работает через стандартные HTTP proxy, CDN, load balancers.
5. Горизонтальное масштабирование через Redis Pub/Sub: каждый инстанс подписан на channels организаций, чьи пользователи к нему подключены.
6. NFR-1.3 (≤ 2 сек) выполняется: событие RabbitMQ → Redis Pub/Sub → SSE push ≈ < 100 мс.
7. Polling endpoint доступен как fallback.

**Риски и митигации:**
- Потеря Redis → SSE degradation. Fallback: polling. Alert при Redis unavailable.
- SSE connection leak → goroutine leak. Heartbeat timeout (30s) + explicit cleanup.

## ADR-4: Upload через оркестратор (proxy)

**Контекст:** Два варианта загрузки файла: через оркестратор или direct-to-S3 с presigned URL.

**Варианты:**

| Вариант | Плюсы | Минусы |
|---------|-------|--------|
| Proxy через оркестратор | Серверная валидация, единый flow, нет S3 credentials на клиенте. | Оркестратор пропускает трафик (до 20 МБ на запрос). |
| Direct-to-S3 (presigned URL) | Разгрузка оркестратора, параллельная загрузка. | Сложный 2-step flow, валидация только post-upload, S3 CORS. |

**Решение:** Proxy через оркестратор.

**Обоснование:**
1. ASSUMPTION-4 из DM: файл загружается оркестратором ДО создания версии.
2. Серверная валидация (размер, MIME, magic bytes) до сохранения в S3.
3. 20 МБ макс. — допустимая нагрузка для proxy (streaming, не буферизация).
4. Единый atomic-like flow: upload → DM create → DP command.
5. Нет необходимости выдавать presigned upload URL клиенту (упрощение).
6. При росте нагрузки можно перейти на direct-to-S3 + callback.

## ADR-5: JWT-аутентификация

**Контекст:** Нужно выбрать стратегию аутентификации.

**Варианты:**

| Вариант | Плюсы | Минусы |
|---------|-------|--------|
| JWT (stateless) | Локальная валидация, масштабируемость, стандарт. | Нет мгновенного отзыва (до expire). |
| Session-based | Мгновенный отзыв, простой logout. | Stateful, session store, scaling. |
| OAuth2 + OIDC | Стандарт для SSO, federation. | Избыточно для v1, дополнительная инфраструктура. |

**Решение:** JWT (access token, 15 мин) + refresh token (30 дней, с возможностью отзыва в UOM).

**Обоснование:**
1. ASSUMPTION-3 из DM: DM получает `organization_id`, `user_id` как доверенный контекст. JWT — стандартный способ передачи.
2. Stateless валидация — масштабируемость, минимальная зависимость от UOM.
3. Short-lived access token (15 мин) минимизирует окно уязвимости при компрометации.
4. Refresh token с отзывом в UOM — механизм для logout и блокировки.
5. OAuth2/OIDC может быть добавлен позже (UOM как authorization server) без изменения API оркестратора.

**JWT claims (минимальный набор):**

```json
{
  "sub": "user_id (UUID)",
  "org": "organization_id (UUID)",
  "role": "LAWYER | BUSINESS_USER | ORG_ADMIN",
  "exp": 1712400000,
  "iat": 1712399100,
  "jti": "unique token id (UUID)"
}
```

## ADR-6: Same-origin deployment topology

**Контекст:** Frontend (SPA) и Orchestrator (REST API) в production-окружении могут быть развёрнуты как:
- (A) **Same-origin** — единый nginx раздаёт SPA-статику и проксирует `/api/*` на Orchestrator. Браузер видит один origin.
- (B) **Cross-origin** — frontend на `app.contractpro.ru`, API на `api.contractpro.ru`. Браузер активирует CORS-механизм.

**Варианты:**

| Вариант | Плюсы | Минусы |
|---------|-------|--------|
| Same-origin (A) | CORS не активируется; cookies без `SameSite=None`; `connect-src 'self'` достаточно для CSP; OpenTelemetry `traceparent` / `tracestate` инжектируются без preflight; меньше DevOps complexity (один nginx, один cert) | Frontend и Orchestrator деплоятся вместе (общий release-цикл) |
| Cross-origin (B) | Независимое масштабирование frontend (CDN) и API; раздельные SLA; API как публичный продукт для интеграций | Сложная CORS-конфигурация; preflight overhead; CSRF при cookies; отдельные cert/DNS; OTel-headers требуют явного allow-list |

**Решение:** Same-origin (A) для v1.

**Обоснование:**
1. ContractPro в v1 — внутренний продукт для юристов организаций; публичный API для внешних интеграций (ЭДО, CRM) **не в скоупе v1**.
2. Frontend `§13.2 nginx.conf` уже спроектирован под A: единый nginx раздаёт `/` (SPA) и проксирует `/api/v1/*` + `/api/v1/events/stream` (SSE passthrough) на Orchestrator.
3. Упрощает 152-ФЗ compliance — единый TLS-перимтр, единый audit-perimeter.
4. OpenTelemetry-инструментация (`@opentelemetry/instrumentation-fetch`) автоматически инжектит `traceparent` в каждый запрос — при cross-origin без явного allow-list это блокирует **все** запросы. Same-origin исключает этот класс багов.
5. Refresh token в HttpOnly-cookie (Frontend ADR-FE-03 / §18 п.1) проще реализуется без `SameSite=None; Secure; Partitioned` сложностей.
6. SSE-аутентификация через `?token=` (см. ADR-FE-10 миграция на `sse_ticket`) не пересекается с CORS — same-origin EventSource не делает preflight.

**Конфигурация:**
- Production: `ORCH_CORS_ALLOWED_ORIGINS=` **пустой** (default same-origin only). CORS middleware не активируется.
- Dev: используется Vite proxy (`vite.config.ts → server.proxy: {'/api': 'http://localhost:8080'}`) — same-origin в dev-окружении тоже.
- Backend `Allowed Headers` включает `traceparent` и `tracestate` **превентивно** — на случай будущего разделения доменов или внешних кросс-доменных интеграций (см. security.md §5.1).

**Когда пересмотреть (триггеры перехода на B):**
- Появление публичного API для интеграций партнёров (ЭДО, CRM-системы) — `api.contractpro.ru` как официальный endpoint.
- Раздельное масштабирование frontend через CDN (CloudFront/CloudFlare).
- Multi-tenant white-label deployment с разными доменами для разных клиентов.

**Митигации текущих рисков:**
- Общий release-цикл frontend/orchestrator → координация через CI: контракт-тест на OpenAPI gate перед merge (см. Frontend §10.2 contract tests).
- При необходимости разделить деплой — backend-конфигурация уже готова (CORS middleware есть, env-переменные есть), нужно только заполнить `ORCH_CORS_ALLOWED_ORIGINS`.

**Frontend ссылки:** `Frontend/architecture/high-architecture.md` §7.2 (HTTP-клиент с относительным baseURL), §13.2 (nginx.conf), §14.3 (OTel `traceparent` без CORS-блока), §21 (ADR table).
