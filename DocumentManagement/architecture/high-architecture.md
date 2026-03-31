# Верхнеуровневая архитектура Document Management

В рамках документа описана архитектура **доменной области Document Management (DM)** сервиса **ContractPro** до уровня компонентов.

---

# 1. Ключевые требования и ограничения для DM

## 1.1 Бизнес-контекст

ContractPro — AI-сервис проверки договоров в юрисдикции РФ. Пользователи загружают договоры, система анализирует риски, формирует рекомендации. Ключевые бизнес-процессы, затрагивающие DM:

1. **Загрузка договора** → создание документа и первой версии → запуск обработки в DP.
2. **Сохранение результатов обработки** → DP отправляет артефакты (OCR, text, structure, semantic tree, warnings) → DM сохраняет.
3. **Юридический анализ** → LIC читает артефакты из DM, формирует результаты → DM сохраняет результаты анализа.
4. **Формирование отчётов** → Reporting Engine читает из DM → формирует отчёты → DM хранит экспортные артефакты.
5. **Повторная проверка / применение рекомендаций** → создание новой версии → повторный цикл.
6. **Сравнение версий** → DP запрашивает semantic trees из DM → формирует diff → DM сохраняет diff.
7. **Выдача данных через API** → API/backend-оркестратор читает метаданные, артефакты, статусы из DM.

## 1.2 Функциональные требования, влияющие на DM

| Требование | Влияние на DM |
|------------|---------------|
| UR-1 (загрузка договора, статус обработки) | DM — точка создания документа и версии; хранит ссылку на исходный файл; отслеживает статус готовности артефактов |
| UR-9 (повторная проверка версий, изменения риск-профиля) | DM хранит lineage версий; связывает каждую версию с собственным набором артефактов; обеспечивает доступ к любой версии |
| UR-10 (выгрузка отчёта) | DM хранит ссылки на экспортные артефакты (PDF/DOCX), сформированные Reporting Engine |
| FR-4.2 (применение рекомендации → новая версия) | DM создаёт новую версию с `origin_type=RECOMMENDATION_APPLIED`, связывает с parent version |
| FR-5.3.1 (сравнение двух версий) | DM предоставляет semantic trees по запросу DP; хранит результат diff |
| FR-6.2 (разграничение по ролям и организациям) | DM обеспечивает tenant isolation по `organization_id` |
| FR-6.3 (журналирование) | DM ведёт audit trail по операциям с документами и версиями |

## 1.3 Нефункциональные требования, влияющие на DM

| NFR | Влияние на DM |
|-----|---------------|
| NFR-1.3 (UI ≤ 2 сек p95) | Синхронные read-операции DM (метаданные, списки) должны отвечать < 100 мс p95, чтобы вписаться в бюджет |
| NFR-2.2 (атомарность записи) | Запись метаданных + blob-ссылок в рамках одной бизнес-операции должна быть атомарной или компенсируемой |
| NFR-2.3 (RPO ≤ 15 мин, RTO ≤ 2 часов) | Metadata store и object storage должны поддерживать резервное копирование с соответствующим RPO/RTO |
| NFR-3.2 (шифрование данных) | Encryption at rest для metadata store и object storage |
| NFR-3.3 (доступ в рамках организации) | Каждый запрос к DM фильтруется по `organization_id` |
| NFR-3.4 (журнал действий) | Audit trail: кто, когда, что сделал с документом/версией |

## 1.4 Архитектурные ограничения

1. **EDA** — межсервисное взаимодействие через RabbitMQ.
2. **At-least-once delivery** — DM обязан быть идемпотентным при приёме событий.
3. **Единые внешние статусы** — `QUEUED`, `IN_PROGRESS`, `COMPLETED`, `COMPLETED_WITH_WARNINGS`, `FAILED`, `TIMED_OUT`, `REJECTED`.
4. **Correlation fields** — `correlation_id`, `job_id`, `document_id`, `version_id`, `organization_id`, `requested_by_user_id`.
5. **DLQ** для необработанных сообщений.
6. **Go** как язык реализации (единый стек с DP).
7. **RabbitMQ** как брокер (уже развёрнут для DP).
8. **Yandex Object Storage** для blob-артефактов (единая инфраструктура).

## 1.5 Междоменные зависимости

```
API/Backend Orchestrator
    │
    ├── (sync) ──► DM: CRUD документов, чтение метаданных/артефактов
    ├── (async) ──► DP: команды на обработку/сравнение
    │
DP (stateless)
    ├── (async) ──► DM: артефакты обработки, запросы semantic tree, diff
    │
LIC (stateless)
    ├── (async) ◄── DM / RabbitMQ: событие о готовности артефактов
    ├── (async) ──► DM: запрос артефактов через RabbitMQ (request-response)
    ├── (async) ◄── DM / RabbitMQ: артефакты предоставлены
    ├── (async) ──► DM: запись результатов анализа через RabbitMQ
    │
Reporting Engine (stateless)
    ├── (async) ◄── DM / RabbitMQ: событие о готовности результатов анализа
    ├── (async) ──► DM: запрос артефактов через RabbitMQ (request-response)
    ├── (async) ◄── DM / RabbitMQ: артефакты предоставлены
    ├── (async) ──► DM: запись экспортных артефактов через RabbitMQ
    │
OPM (stateful)
    ├── LIC запрашивает политики из OPM, не из DM
    │
UOM (stateful)
    ├── DM проверяет organization_id/user_id — но аутентификация в UOM
```

## 1.6 Архитектурные риски

| ID | Риск | Вероятность | Влияние | Митигация |
|----|------|-------------|---------|-----------|
| R-1 | Object Storage недоступен → cascade failure (DP артефакты не сохраняются, DP получает fail, пользователь видит ошибку) | Средняя | Высокое | Retry + DLQ + alert. DP поддерживает `is_retryable`. Оркестратор может перезапустить задачу. |
| R-2 | Orphan blobs при частичном сбое | Средняя | Низкое | Orphan cleanup job. Метрика orphan count. |
| R-3 | Outbox backlog при длительной недоступности RabbitMQ → задержка notifications для LIC/RE | Низкая | Среднее | Outbox backlog alert. RabbitMQ в HA-конфигурации. |
| R-4 | DB bloat от audit records при высокой нагрузке | Низкая | Среднее | Партиционирование `audit_records` по месяцам. Архивация в холодное хранилище. |
| R-5 | Рост размера Object Storage при отсутствии retention | Средняя | Среднее | S3 lifecycle rules. Retention policy enforcement. |
| R-6 | Конкурентное создание версий → deadlock на UPDATE documents | Низкая | Низкое | Optimistic locking (version_number check). Retry. |
| R-7 | Рассогласование schema_version артефактов при обновлении DP/LIC/RE | Средняя | Высокое | Schema registry. Backward-compatible changes only. |

---

# 2. Архитектурные допущения

| ID | Допущение |
|----|-----------|
| ASSUMPTION-1 | API/Backend-оркестратор — отдельный слой (не домен), координирующий вызовы к DM, DP, LIC. Его архитектура вне скоупа данного документа. |
| ASSUMPTION-2 | DM предоставляет синхронный REST/gRPC API для операций, требующих немедленного ответа (создание документа/версии, чтение метаданных, получение артефактов), и асинхронный EDA-интерфейс для межсервисного взаимодействия. |
| ASSUMPTION-3 | Аутентификация выполняется на уровне API Gateway / Backend-оркестратора. DM получает `organization_id` и `user_id` как доверенный контекст в каждом запросе. |
| ASSUMPTION-4 | Исходный файл договора загружается в Object Storage оркестратором до создания версии в DM. DM получает ссылку (`storage_key`), а не бинарный файл. |
| ASSUMPTION-5 | Один Document может принадлежать только одной организации. Передача между организациями — отдельный бизнес-процесс, вне скоупа v1. |
| ASSUMPTION-6 | DM не выполняет никакой бизнес-логики анализа, классификации или трансформации контента. Это обязанность DP, LIC, Reporting Engine. |
| ASSUMPTION-7 | Metadata store — PostgreSQL. Обоснование: реляционная модель с транзакциями, foreign keys, индексами; зрелый инструмент для Go; подходит для multi-tenant фильтрации. |
| ASSUMPTION-8 | Blob/Object storage — Yandex Object Storage (S3-compatible). Единая инфраструктура с DP. |
| ASSUMPTION-9 | Все домены (DP, LIC, Reporting Engine) работают с JSON-сериализацией событий через RabbitMQ. EventMeta (`correlation_id`, `timestamp`) — обязательный envelope. |
| ASSUMPTION-10 | Нагрузка на старте: ~1000 договоров/сутки. Средний размер артефактов на одну версию: ~5–15 МБ (исходный PDF + OCR + text + structure + semantic tree). |
| ASSUMPTION-11 | Версия документа — immutable после финализации. Артефакты привязаны к конкретной версии и не мутируют. |
| ASSUMPTION-12 | KV-store (Redis) используется для idempotency store и кэширования горячих метаданных. Единая инфраструктура с DP. |
| ASSUMPTION-13 | При отсутствии semantic tree для запрошенной версии DM возвращает ошибку. Формат ответа должен быть согласован с командой DP. |
| ASSUMPTION-14 | LIC и RE используют аналогичный DP механизм доставки артефактов: async через RabbitMQ с event envelope (`EventMeta`) и паттерном confirm/fail. |
| ASSUMPTION-15 | API/Backend-оркестратор — единственный потребитель sync API DM. DP, LIC, RE взаимодействуют с DM исключительно через RabbitMQ. Прямой доступ из frontend к DM отсутствует. |
| ASSUMPTION-16 | Ошибка при отсутствии semantic tree передаётся через поля `error_code`, `error_message`, `is_retryable` в существующем событии `SemanticTreeProvided` (не отдельный event type). Backward-compatible: поля с `omitempty`. Задача TASK-055 в DP. |
| ASSUMPTION-17 | `version_id` передаётся явно в `ProcessDocumentRequested` и `DocumentProcessingArtifactsReady`. DM привязывает артефакты к конкретной версии по `version_id` из события, а не через lookup `current_version_id`. Задача TASK-056 в DP. |
| ASSUMPTION-18 | `organization_id` передаётся во всех 8 исходящих событиях DP (`DocumentProcessingArtifactsReady`, `GetSemanticTreeRequest`, `DocumentVersionDiffReady`, `StatusChangedEvent`, `ProcessingCompletedEvent`, `ProcessingFailedEvent`, `ComparisonCompletedEvent`, `ComparisonFailedEvent`). DM использует для tenant validation. Поле `omitempty` — backward-compatible. Задача TASK-057 в DP. |
| ASSUMPTION-19 | Retention policy в v1 — глобальная (единая для всех организаций), задаётся в конфигурации DM через переменные окружения. В последующих версиях может быть расширена до per-organization (сущность `RetentionPolicy` с привязкой к `organization_id`, UI для R-3). |
| ASSUMPTION-20 | Event delivery guarantee реализуется через Transactional Outbox: событие записывается в таблицу `outbox_events` в той же DB-транзакции, что и основные данные; фоновый Outbox Poller публикует в RabbitMQ. CDC (Debezium) не используется в v1 — избыточная инфраструктура для текущего масштаба. |

---

# 3. Границы домена Document Management

## 3.1 Что входит в домен

1. **Управление жизненным циклом документа:** создание, архивация, soft delete.
2. **Версионность:** создание новых версий, хранение lineage, current version pointer.
3. **Метаданные:** document metadata, version metadata, технические атрибуты.
4. **Хранение артефактов:** приём артефактов от DP, LIC, Reporting Engine; хранение blob-ссылок + metadata descriptors.
5. **Выдача данных:** синхронный API для чтения метаданных и артефактов; асинхронный response для DP (semantic trees, confirmation).
6. **Публикация событий:** уведомление о готовности артефактов, создании версий, ошибках.
7. **Трассируемость:** audit trail, lineage версий, происхождение артефактов.
8. **Tenant isolation:** фильтрация всех данных по `organization_id`.
9. **Retention management:** политики хранения, архивация, очистка.
10. **Идемпотентная обработка входящих событий.**

## 3.2 Что не входит в домен

| Функция | Принадлежит |
|---------|-------------|
| OCR, text extraction, structure extraction | DP |
| Построение semantic tree | DP |
| Diff-логика (текстовый + структурный diff) | DP |
| Классификация, риск-анализ, рекомендации | LIC |
| Формирование пользовательских отчётов | Reporting Engine |
| Управление политиками, чек-листами | OPM |
| Аутентификация, авторизация, управление пользователями | UOM |
| Загрузка бинарного файла (upload endpoint) | API/Backend-оркестратор |
| Оркестрация сквозного процесса (загрузка → обработка → анализ → отчёт) | API/Backend-оркестратор |

## 3.3 Границы относительно DP

| Ответственность | DP | DM |
|----------------|----|----|
| Приём команды на обработку | ✓ | — |
| Выполнение OCR / text extraction / structure extraction | ✓ | — |
| Построение semantic tree | ✓ | — |
| Выполнение diff-логики | ✓ | — |
| Отправка артефактов | ✓ (публикует) | ✓ (принимает, сохраняет) |
| Хранение артефактов | — | ✓ |
| Выдача semantic tree для сравнения | — | ✓ (по запросу DP) |
| Хранение diff-результата | — | ✓ |
| Подтверждение сохранения | — | ✓ (публикует confirmation) |
| Cleanup временных артефактов | ✓ | — |

**Контракт взаимодействия DP → DM уже зафиксирован** в реализации DP:
- DP публикует: `DocumentProcessingArtifactsReady`, `GetSemanticTreeRequest`, `DocumentVersionDiffReady`.
- DM отвечает: `DocumentProcessingArtifactsPersisted`, `DocumentProcessingArtifactsPersistFailed`, `SemanticTreeProvided`, `DocumentVersionDiffPersisted`, `DocumentVersionDiffPersistFailed`.

DM **обязан реализовать** обработку этих событий с сохранением envelope (`EventMeta`).

## 3.4 Границы относительно LIC

| Ответственность | LIC | DM |
|----------------|-----|----|
| Классификация, анализ, рекомендации | ✓ | — |
| Чтение артефактов (semantic tree, text, structure) | ✓ (запрашивает через RabbitMQ) | ✓ (выдаёт через RabbitMQ) |
| Запись результатов анализа | ✓ (публикует через RabbitMQ) | ✓ (сохраняет) |
| Хранение результатов анализа | — | ✓ |

LIC **не обращается к DP напрямую** — только через DM. Все взаимодействия LIC ↔ DM — async через RabbitMQ.

## 3.5 Границы относительно Reporting Engine

| Ответственность | RE | DM |
|----------------|----|----|
| Формирование отчётов (PDF/DOCX) | ✓ | — |
| Чтение данных для отчёта | ✓ (запрашивает через RabbitMQ) | ✓ (выдаёт через RabbitMQ) |
| Запись экспортных артефактов | ✓ (публикует через RabbitMQ) | ✓ (сохраняет) |
| Хранение отчётов | — | ✓ |

## 3.6 Границы относительно OPM и UOM

- **OPM:** DM не взаимодействует с OPM напрямую. Политики запрашиваются LIC из OPM.
- **UOM:** DM не аутентифицирует пользователей. `organization_id` и `user_id` приходят как доверенный контекст от API Gateway / оркестратора. DM **enforcement** — фильтрация по `organization_id`.

---

# 4. Архитектурная концепция DM

## 4.1 Назначение домена

Document Management — **stateful-домен**, являющийся единым **source of truth** по документам, их версиям и артефактам в системе ContractPro. DM обеспечивает:

- надёжное хранение и версионирование документов;
- приём и персистенцию артефактов от вычислительных доменов (DP, LIC, RE);
- выдачу данных другим доменам и внешним потребителям;
- трассируемость изменений;
- tenant isolation.

## 4.2 Роль DM в общей системе

DM — **центральный data hub** между вычислительными доменами:

```
                    API / Backend Orchestrator
                    (sync: CRUD, read)
                              │
                              ▼
                   ┌─────────────────────┐
                   │  Document Management │
                   │    (stateful hub)    │
                   └──┬──────┬───────┬───┘
                      │      │       │
          artifacts   │      │       │  artifacts
          + confirm   │      │       │  + events
                      ▼      ▼       ▼
                    DP      LIC     RE
                (stateless compute domains)
```

**Поток данных:**
1. Оркестратор → DM: создание документа/версии (sync).
2. Оркестратор → DP: команда на обработку (async).
3. DP → DM: артефакты обработки (async).
4. DM → DP: confirmation (async).
5. DM → RabbitMQ: событие `version-artifacts-ready` (async).
6. LIC подписывается на `version-artifacts-ready`, запрашивает артефакты из DM через RabbitMQ `GetArtifactsRequest` (async request-response).
7. DM → LIC: `ArtifactsProvided` с запрошенными артефактами (async).
8. LIC выполняет анализ.
9. LIC → DM: результаты анализа через RabbitMQ `LegalAnalysisArtifactsReady` (async).
10. DM → LIC: confirmation (async).
11. DM → RabbitMQ: событие `version-analysis-ready` (async).
12. RE подписывается на `version-analysis-ready`, запрашивает артефакты из DM через RabbitMQ `GetArtifactsRequest` (async request-response).
13. DM → RE: `ArtifactsProvided` с запрошенными артефактами (async).
14. RE формирует отчёт.
15. RE → DM: экспортные артефакты через RabbitMQ `ReportsArtifactsReady` (async).
16. DM → RE: confirmation (async).
17. DM → RabbitMQ: событие `version-reports-ready` (async).
18. Оркестратор / API читает результаты из DM (sync).

## 4.3 Почему DM должен быть stateful

1. **Source of truth**: документы, версии и артефакты — долгоживущие данные, которые должны переживать перезапуск сервиса.
2. **ACID-транзакции**: создание версии + привязка артефактов должны быть атомарными.
3. **Индексация и поиск**: запросы по `organization_id`, `document_id`, фильтрация, пагинация.
4. **Lineage**: граф версий хранится в реляционной модели с foreign keys.
5. **Audit trail**: append-only лог требует persistent storage.

## 4.4 Принципы проектирования

| # | Принцип | Обоснование |
|---|---------|-------------|
| 1 | **Immutable versions** | Версия после финализации не изменяется. Новые данные — новая версия. Обеспечивает аудируемость и предсказуемость. |
| 2 | **Metadata + blob separation** | Метаданные — в PostgreSQL. Blob-артефакты — в Object Storage. Ссылка между ними — `storage_key`. Обеспечивает масштабируемость и дешёвое хранение. |
| 3 | **Idempotent ingestion** | Каждое входящее событие обрабатывается идемпотентно по ключу `(job_id, artifact_type)` или `(job_id, event_type)`. |
| 4 | **Event-first, sync-supplement** | Междоменное взаимодействие (DP, LIC, RE ↔ DM) — исключительно через RabbitMQ (события, request-response). Sync API — только для оркестратора и UI (CRUD, чтение метаданных/артефактов пользователем). |
| 5 | **Tenant-scoped access** | Каждый запрос (sync и async) содержит `organization_id`. Все queries фильтруются по нему. |
| 6 | **Confirm-after-persist** | DM публикует confirmation-event только после успешного commit в metadata store + успешной записи в object storage. |
| 7 | **Schema versioning** | Каждый артефакт несёт `schema_version`. Обеспечивает backward compatibility при эволюции контрактов. |

---

# 5. Модель предметной области DM

## 5.1 Document

**Назначение:** Корневой агрегат. Представляет один договор в системе.

**Ключевые поля:**

| Поле | Тип | Описание |
|------|-----|----------|
| `document_id` | UUID | Уникальный идентификатор документа |
| `organization_id` | UUID | Принадлежность организации (tenant key) |
| `title` | string | Название документа (задаётся пользователем или извлекается) |
| `current_version_id` | UUID (nullable) | Указатель на актуальную версию |
| `status` | enum | `ACTIVE`, `ARCHIVED`, `DELETED` |
| `created_by_user_id` | UUID | Кто создал документ |
| `created_at` | timestamp | Когда создан |
| `updated_at` | timestamp | Последнее обновление |
| `deleted_at` | timestamp (nullable) | Soft delete |

**Инварианты:**
- `document_id` глобально уникален.
- `organization_id` не может быть изменён после создания.
- `current_version_id` указывает только на версию, принадлежащую этому документу.
- `status=DELETED` — soft delete; данные сохраняются для аудита.

**Связи:**
- `Document` 1→N `DocumentVersion`.
- `Document` принадлежит одной организации.

## 5.2 DocumentVersion

**Назначение:** Immutable-снимок состояния документа в определённый момент времени.

**Ключевые поля:**

| Поле | Тип | Описание |
|------|-----|----------|
| `version_id` | UUID | Уникальный идентификатор версии |
| `document_id` | UUID | FK на Document |
| `organization_id` | UUID | Tenant key (денормализация для RLS и быстрых запросов) |
| `version_number` | int | Порядковый номер версии (auto-increment в пределах документа) |
| `parent_version_id` | UUID (nullable) | Предыдущая версия (lineage) |
| `origin_type` | enum | Источник создания версии |
| `origin_description` | string (nullable) | Дополнительный контекст (например, «рекомендация по п. 5.3») |
| `source_file_key` | string | Ключ исходного файла в Object Storage |
| `source_file_name` | string | Оригинальное имя файла |
| `source_file_size` | int64 | Размер в байтах |
| `source_file_checksum` | string | SHA-256 checksum |
| `artifact_status` | enum | Статус готовности артефактов |
| `created_by_user_id` | UUID | Кто инициировал создание версии |
| `created_at` | timestamp | Когда создана |

**Enum `origin_type`:**

| Значение | Описание |
|----------|----------|
| `UPLOAD` | Первичная загрузка пользователем |
| `RE_UPLOAD` | Повторная загрузка (новая редакция) |
| `RECOMMENDATION_APPLIED` | Применение рекомендации из LIC |
| `MANUAL_EDIT` | Ручное редактирование пользователем |
| `RE_CHECK` | Повторная проверка без изменения файла (перезапуск DP + LIC). Создаётся новая версия с тем же `source_file_key`, что и у parent version. Артефакты привязываются к новой версии, старая версия не затрагивается. |

**Enum `artifact_status`** (state machine, правила переходов и notifications — см. [state-machine.md](state-machine.md)):

| Значение | Описание |
|----------|----------|
| `PENDING` | Версия создана, артефакты ещё не получены |
| `PROCESSING_ARTIFACTS_RECEIVED` | Артефакты DP сохранены |
| `ANALYSIS_ARTIFACTS_RECEIVED` | Результаты LIC сохранены |
| `REPORTS_READY` | Отчёты RE сохранены |
| `FULLY_READY` | Все ожидаемые артефакты получены |
| `PARTIALLY_AVAILABLE` | Часть артефактов доступна, часть — с ошибкой |

**Инварианты:**
- Версия immutable после создания (метаданные и `source_file_key` не меняются).
- `artifact_status` — единственное мутабельное поле; переходы только вперёд.
- `version_number` монотонно возрастает в пределах документа.
- `parent_version_id` ссылается только на версию того же документа.

**Связи:**
- `DocumentVersion` N→1 `Document`.
- `DocumentVersion` 1→N `ArtifactDescriptor`.
- `DocumentVersion` имеет self-reference через `parent_version_id`.

## 5.3 ArtifactDescriptor

**Назначение:** Метаданные одного артефакта, привязанного к версии документа. Blob-содержимое хранится в Object Storage, ArtifactDescriptor содержит ссылку.

**Ключевые поля:**

| Поле | Тип | Описание |
|------|-----|----------|
| `artifact_id` | UUID | Уникальный идентификатор |
| `version_id` | UUID | FK на DocumentVersion |
| `document_id` | UUID | FK на Document (денормализация для быстрых запросов) |
| `organization_id` | UUID | Tenant key (денормализация) |
| `artifact_type` | enum | Тип артефакта |
| `producer_domain` | enum | Домен, создавший артефакт |
| `storage_key` | string | Ключ объекта в Object Storage |
| `size_bytes` | int64 | Размер blob |
| `content_hash` | string | SHA-256 содержимого (deduplication, integrity) |
| `schema_version` | string | Версия схемы артефакта (например, «1.0») |
| `job_id` | string | ID задачи, в рамках которой создан артефакт |
| `correlation_id` | string | Correlation ID бизнес-операции |
| `created_at` | timestamp | Когда сохранён |

**Enum `artifact_type`:**

| Значение | Producer | Описание |
|----------|----------|----------|
| `OCR_RAW` | DP | Сырой OCR-результат |
| `EXTRACTED_TEXT` | DP | Нормализованный текст |
| `DOCUMENT_STRUCTURE` | DP | Логическая структура |
| `SEMANTIC_TREE` | DP | Семантическое дерево |
| `PROCESSING_WARNINGS` | DP | Предупреждения обработки |
| `CLASSIFICATION_RESULT` | LIC | Тип договора + уверенность |
| `KEY_PARAMETERS` | LIC | Извлечённые параметры |
| `RISK_ANALYSIS` | LIC | Выявленные риски |
| `RISK_PROFILE` | LIC | Сводный риск-профиль |
| `RECOMMENDATIONS` | LIC | Рекомендации формулировок |
| `SUMMARY` | LIC | Краткое резюме |
| `DETAILED_REPORT` | LIC | Детальный отчёт |
| `AGGREGATE_SCORE` | LIC | Сводная оценка |
| `EXPORT_PDF` | RE | Экспорт PDF |
| `EXPORT_DOCX` | RE | Экспорт DOCX |

**Enum `producer_domain`:** `DP`, `LIC`, `RE`.

> Исходный файл (PDF) хранится через `DocumentVersion.source_file_key`, а не как отдельный `ArtifactDescriptor`. Это исключает необходимость `SOURCE_FILE` artifact type и `ORCHESTRATOR` producer (REV-011).

**Инварианты:**
- Уникальность по `(version_id, artifact_type)` — одна версия содержит максимум один артефакт каждого типа.
- `storage_key` ссылается на существующий объект в Object Storage.
- `content_hash` вычисляется при записи и проверяется при чтении (integrity).
- Артефакт immutable после создания.

**Связи:**
- `ArtifactDescriptor` N→1 `DocumentVersion`.

## 5.4 VersionDiffReference

**Назначение:** Хранит ссылку на результат сравнения двух версий.

**Ключевые поля:**

| Поле | Тип | Описание |
|------|-----|----------|
| `diff_id` | UUID | Уникальный идентификатор |
| `document_id` | UUID | FK на Document |
| `organization_id` | UUID | Tenant key |
| `base_version_id` | UUID | FK на DocumentVersion (базовая) |
| `target_version_id` | UUID | FK на DocumentVersion (целевая) |
| `storage_key` | string | Ключ blob в Object Storage |
| `text_diff_count` | int | Количество текстовых различий |
| `structural_diff_count` | int | Количество структурных различий |
| `job_id` | string | ID задачи сравнения |
| `correlation_id` | string | Correlation ID |
| `created_at` | timestamp | Когда создан |

**Инварианты:**
- Уникальность по `(base_version_id, target_version_id)`.
- Обе версии принадлежат одному документу.
- Immutable после создания.

**Связи:**
- `VersionDiffReference` N→1 `Document`.
- `VersionDiffReference` ссылается на две `DocumentVersion`.

## 5.5 AuditRecord

**Назначение:** Append-only запись о значимом действии над документом или версией.

**Ключевые поля:**

| Поле | Тип | Описание |
|------|-----|----------|
| `audit_id` | UUID | Уникальный идентификатор |
| `organization_id` | UUID | Tenant key |
| `document_id` | UUID (nullable) | Если действие связано с документом |
| `version_id` | UUID (nullable) | Если связано с конкретной версией |
| `action` | enum | Тип действия |
| `actor_type` | enum | `USER`, `SYSTEM`, `DOMAIN` |
| `actor_id` | string | `user_id` или имя домена |
| `job_id` | string (nullable) | Связанная задача |
| `correlation_id` | string (nullable) | Correlation ID |
| `details` | JSONB | Дополнительные данные (старый/новый статус, тип артефакта и т.д.) |
| `created_at` | timestamp | Когда зафиксировано |

**Enum `action`:**
`DOCUMENT_CREATED`, `VERSION_CREATED`, `ARTIFACT_SAVED`, `ARTIFACT_READ`, `DIFF_SAVED`, `DOCUMENT_ARCHIVED`, `DOCUMENT_DELETED`, `ARTIFACT_STATUS_CHANGED`, `VERSION_FINALIZED`.

**Инварианты:**
- Append-only: записи никогда не удаляются и не модифицируются.
- `created_at` — серверное время, не клиентское.

## 5.6 IdempotencyRecord

**Назначение:** Запись для дедупликации обработки входящих асинхронных событий.

**Ключевые поля:**

| Поле | Тип | Описание |
|------|-----|----------|
| `idempotency_key` | string | Составной ключ (например, `{job_id}:{event_type}`) |
| `status` | enum | `PROCESSING`, `COMPLETED`, `FAILED` |
| `result_snapshot` | JSONB (nullable) | Сохранённый результат для повторного ответа |
| `created_at` | timestamp | Когда создана запись |
| `expires_at` | timestamp | TTL для автоочистки |

**Хранение:** Redis (быстрый доступ, TTL) с fallback-проверкой в PostgreSQL по `(job_id, artifact_type)`.

---

# 6. Внутренние компоненты DM

## 6.1 Архитектура компонентов

```
+===========================================================================+
|                         Document Management                               |
|                                                                           |
|  INGRESS (async)                                                          |
|  ~~~~~~~~~~~~~~~~                                                         |
|  [Event Consumer] --> [Idempotency Guard] --> [Event Router]              |
|                                                                           |
|  INGRESS (sync)                                                           |
|  ~~~~~~~~~~~~~~~                                                          |
|  [API Handler] --> [Auth Context Extractor] --> [Request Router]          |
|                                                                           |
|  APPLICATION                                                              |
|  ~~~~~~~~~~~                                                              |
|  [Artifact Ingestion Service]                                             |
|  [Version Management Service]                                             |
|  [Artifact Query Service]                                                 |
|  [Document Lifecycle Service]                                             |
|  [Diff Storage Service]                                                   |
|                                                                           |
|  DOMAIN                                                                   |
|  ~~~~~~                                                                   |
|  [Document] [DocumentVersion] [ArtifactDescriptor]                        |
|  [VersionDiffReference] [AuditRecord]                                     |
|                                                                           |
|  EGRESS                                                                   |
|  ~~~~~~                                                                   |
|  [Event Publisher]          — публикация событий в брокер                  |
|  [Confirmation Publisher]   — подтверждения для DP                         |
|  [Notification Publisher]   — уведомления для LIC, RE                     |
|                                                                           |
|  INFRASTRUCTURE                                                           |
|  ~~~~~~~~~~~~~~                                                           |
|  [Metadata Store (PostgreSQL)]                                            |
|  [Object Storage Adapter (S3)]                                            |
|  [Idempotency Store (Redis)]                                              |
|  [Broker Client (RabbitMQ)]                                               |
|  [Observability SDK]                                                      |
|  [Health Check Handler]                                                   |
+===========================================================================+
```

## 6.2 Event Consumer

**Назначение:** Точка входа для асинхронных событий из RabbitMQ.

**Ответственность:**
- Подписка на топики входящих событий.
- Десериализация JSON → Go-структуры.
- Первичная валидация контракта сообщения (обязательные поля, формат).
- Передача в Idempotency Guard.

**Входы:** Raw messages из RabbitMQ.
**Выходы:** Десериализованные события → Idempotency Guard.
**Зависимости:** Broker Client, Observability SDK.

**Подписки на топики:**

| Топик | Событие | Источник |
|-------|---------|----------|
| `dp.artifacts.processing-ready` | `DocumentProcessingArtifactsReady` | DP |
| `dp.requests.semantic-tree` | `GetSemanticTreeRequest` | DP |
| `dp.artifacts.diff-ready` | `DocumentVersionDiffReady` | DP |
| `lic.requests.artifacts` | `GetArtifactsRequest` | LIC |
| `lic.artifacts.analysis-ready` | `LegalAnalysisArtifactsReady` | LIC |
| `re.requests.artifacts` | `GetArtifactsRequest` | RE |
| `re.artifacts.reports-ready` | `ReportsArtifactsReady` | RE |

## 6.3 Idempotency Guard

**Назначение:** Дедупликация повторно доставленных событий.

**Ответственность:**
- Проверка `idempotency_key` в Redis.
- Если ключ найден со статусом `COMPLETED` — ACK без повторной обработки (confirmation уже доставлен через outbox).
- Если ключ найден со статусом `PROCESSING` и возраст < 2× overall_timeout (120s) — ACK без обработки (in-flight дубликат).
- Если ключ найден со статусом `PROCESSING` и возраст ≥ 2× overall_timeout — удалить ключ, обработать заново (предыдущая обработка сбилась).
- Если ключ не найден — SET `PROCESSING` с коротким TTL (120s), передача дальше. После успешной обработки — перезаписать на `COMPLETED` с TTL 24h.

**Входы:** Десериализованное событие.
**Выходы:** Событие → Event Router (если новое).
**Зависимости:** Idempotency Store (Redis).

**Ключи идемпотентности:**

| Событие | Idempotency Key |
|---------|-----------------|
| `DocumentProcessingArtifactsReady` | `dp-artifacts:{job_id}` |
| `GetSemanticTreeRequest` | `dp-tree-req:{job_id}:{version_id}` |
| `DocumentVersionDiffReady` | `dp-diff:{job_id}` |
| `GetArtifactsRequest` (LIC) | `lic-get-artifacts:{job_id}:{version_id}` |
| `LegalAnalysisArtifactsReady` | `lic-artifacts:{job_id}` |
| `GetArtifactsRequest` (RE) | `re-get-artifacts:{job_id}:{version_id}` |
| `ReportsArtifactsReady` | `re-reports:{job_id}` |

**TTL:** 24 часа (конфигурируемо). Достаточно для покрытия retry-окна + DLQ-обработки.

## 6.4 Event Router

**Назначение:** Маршрутизация десериализованных событий к соответствующему Application Service.

**Ответственность:**
- По типу события определяет целевой сервис.
- Передаёт вызов.

| Событие | Целевой сервис |
|---------|----------------|
| `DocumentProcessingArtifactsReady` | Artifact Ingestion Service |
| `GetSemanticTreeRequest` | Artifact Query Service |
| `GetArtifactsRequest` | Artifact Query Service |
| `DocumentVersionDiffReady` | Diff Storage Service |
| `LegalAnalysisArtifactsReady` | Artifact Ingestion Service |
| `ReportsArtifactsReady` | Artifact Ingestion Service |

## 6.5 API Handler

**Назначение:** HTTP REST endpoint для синхронных операций.

**Ответственность:**
- Маршрутизация HTTP-запросов.
- Десериализация, валидация.
- Извлечение auth-контекста (`organization_id`, `user_id`).
- Вызов Application Services.
- Формирование HTTP-ответа.

**Endpoints (минимальный набор):**

| Method | Path | Описание | Service |
|--------|------|----------|---------|
| POST | `/documents` | Создать документ | Document Lifecycle Service |
| GET | `/documents` | Список документов организации | Document Lifecycle Service |
| GET | `/documents/{id}` | Метаданные документа + текущая версия | Document Lifecycle Service |
| POST | `/documents/{id}/versions` | Создать новую версию | Version Management Service |
| GET | `/documents/{id}/versions` | Список версий | Version Management Service |
| GET | `/documents/{id}/versions/{vid}` | Метаданные версии + список артефактов | Version Management Service |
| GET | `/documents/{id}/versions/{vid}/artifacts/{type}` | Получить артефакт | Artifact Query Service |
| GET | `/documents/{id}/diffs/{base_vid}/{target_vid}` | Получить diff | Artifact Query Service |

Все endpoints фильтруют по `organization_id` из auth-контекста.

## 6.6 Artifact Ingestion Service

**Назначение:** Приём и сохранение артефактов от вычислительных доменов.

**Ответственность:**
1. Валидация: документ и версия существуют, принадлежат указанной организации.
2. Сохранение blob-артефактов в Object Storage.
3. Создание `ArtifactDescriptor` в metadata store.
4. Обновление `artifact_status` версии.
5. Публикация confirmation-события для домена-отправителя.
6. Публикация notification-события для нижестоящих доменов.
7. Запись `AuditRecord`.
8. Обновление `idempotency_key` → `COMPLETED`.

**Входы:** Десериализованные события с артефактами.
**Выходы:** Confirmation events, notification events, audit records.
**Зависимости:** Metadata Store, Object Storage Adapter, Confirmation Publisher, Notification Publisher, Idempotency Store.

**Критическая последовательность записи:**
1. Сохранить blob в Object Storage → получить `storage_key`. При ошибке — compensation (удаление уже сохранённых). Circuit breaker для защиты от каскадного отказа. Per-event retry budget: 30–40s (не 5 × 3 × 30s).
2. В одной DB-транзакции: `SELECT document_versions FOR UPDATE` (блокировка строки для защиты от race condition при параллельном обновлении `artifact_status` — BRE-001), проверить допустимость перехода, создать `ArtifactDescriptor`, обновить `artifact_status`, записать `AuditRecord`, записать `outbox_events`.
3. Outbox Poller публикует confirmation и notification.

При недопустимом переходе `artifact_status` (параллельный запрос опередил) — NACK с requeue, дать предыдущему этапу завершиться.

Если шаг 1 успешен, а шаг 2 падает → orphan blob. Регистрируется в таблице `orphan_candidates` (не full S3 scan). Orphan cleanup job удаляет по таблице.

## 6.7 Version Management Service

**Назначение:** Создание и управление версиями документа.

**Ответственность:**
1. Создание новой версии (`DocumentVersion`) с lineage.
2. Назначение `version_number`.
3. Установка `origin_type`.
4. Обновление `current_version_id` в `Document`.
5. Публикация события `dm.events.version-created`.
6. Запись `AuditRecord`.

**Входы:** Sync API requests (от оркестратора).
**Выходы:** `DocumentVersion` + event.
**Зависимости:** Metadata Store, Event Publisher.

**Атомарность:** Создание версии + обновление `current_version_id` + audit — в одной DB-транзакции.

## 6.8 Artifact Query Service

**Назначение:** Чтение артефактов и данных для других доменов и API.

**Ответственность:**
1. Обработка `GetSemanticTreeRequest` от DP: поиск `ArtifactDescriptor` типа `SEMANTIC_TREE` для указанной версии → чтение blob из Object Storage → публикация `SemanticTreeProvided`.
2. Обработка `GetArtifactsRequest` от LIC/RE: поиск `ArtifactDescriptor` по запрошенным типам для указанной версии → чтение blob из Object Storage → публикация `ArtifactsProvided`. Запрос содержит `artifact_types[]` — список нужных типов. Ответ содержит все запрошенные артефакты в одном событии.
3. Обработка sync-запросов API (оркестратор): получение артефактов по типу, версии, документу.
4. Генерация signed URL для прямого скачивания blob (если применимо).
5. Запись `AuditRecord` для операций чтения.

**Входы:** Async events (`GetSemanticTreeRequest`, `GetArtifactsRequest`), sync API requests.
**Выходы:** Async responses (`SemanticTreeProvided`, `ArtifactsProvided`), HTTP responses.
**Зависимости:** Metadata Store, Object Storage Adapter, Confirmation Publisher.

## 6.9 Document Lifecycle Service

**Назначение:** CRUD-операции на уровне документа.

**Ответственность:**
1. Создание документа.
2. Получение метаданных документа (с текущей версией).
3. Список документов организации (с пагинацией, фильтрацией).
4. Архивация документа (`ACTIVE` → `ARCHIVED`).
5. Soft delete (`ACTIVE`/`ARCHIVED` → `DELETED`).
6. Запись `AuditRecord`.

**Входы:** Sync API requests.
**Выходы:** HTTP responses, audit records.
**Зависимости:** Metadata Store.

## 6.10 Diff Storage Service

**Назначение:** Приём и сохранение результатов сравнения версий.

**Ответственность:**
1. Валидация: обе версии существуют и принадлежат одному документу.
2. Сохранение diff blob в Object Storage.
3. Создание `VersionDiffReference` в metadata store.
4. Публикация `DocumentVersionDiffPersisted` для DP.
5. Запись `AuditRecord`.

**Входы:** `DocumentVersionDiffReady` от DP.
**Выходы:** Confirmation events, audit records.
**Зависимости:** Metadata Store, Object Storage Adapter, Confirmation Publisher.

## 6.11 Confirmation Publisher

**Назначение:** Публикация подтверждений сохранения для доменов-отправителей.

**Ответственность:**
- Формирование и публикация подтверждений/ответов: `DocumentProcessingArtifactsPersisted`, `DocumentProcessingArtifactsPersistFailed`, `DocumentVersionDiffPersisted`, `DocumentVersionDiffPersistFailed`, `SemanticTreeProvided`, `ArtifactsProvided`.
- Сохранение `EventMeta` (correlation_id, timestamp).

**Выходные топики:**

| Топик | Событие |
|-------|---------|
| `dm.responses.artifacts-persisted` | `DocumentProcessingArtifactsPersisted` |
| `dm.responses.artifacts-persist-failed` | `DocumentProcessingArtifactsPersistFailed` |
| `dm.responses.semantic-tree-provided` | `SemanticTreeProvided` |
| `dm.responses.artifacts-provided` | `ArtifactsProvided` |
| `dm.responses.diff-persisted` | `DocumentVersionDiffPersisted` |
| `dm.responses.diff-persist-failed` | `DocumentVersionDiffPersistFailed` |

## 6.12 Notification Publisher

**Назначение:** Публикация уведомлений о готовности данных для нижестоящих доменов.

**Ответственность:**
- Отслеживание `artifact_status` версии.
- При переходе `artifact_status` → `PROCESSING_ARTIFACTS_RECEIVED`: публикация `dm.events.version-artifacts-ready`.
- При переходе → `ANALYSIS_ARTIFACTS_RECEIVED`: публикация `dm.events.version-analysis-ready`.
- При переходе → `REPORTS_READY` или `FULLY_READY`: публикация `dm.events.version-reports-ready`.

**Выходные топики:**

| Топик | Описание | Потребитель |
|-------|----------|-------------|
| `dm.events.version-artifacts-ready` | Артефакты обработки DP сохранены | LIC |
| `dm.events.version-analysis-ready` | Результаты анализа LIC сохранены | RE |
| `dm.events.version-reports-ready` | Отчёты RE сохранены | Orchestrator / API |
| `dm.events.version-created` | Новая версия создана | Orchestrator |

## 6.13 Infrastructure Components

### Metadata Store (PostgreSQL)

- Хранит: Document, DocumentVersion, ArtifactDescriptor, VersionDiffReference, AuditRecord.
- Индексы: `(organization_id)`, `(document_id, version_id)`, `(version_id, artifact_type)`, `(created_at)` для audit.
- Connection pool: pgx.
- Миграции: golang-migrate.

### Object Storage Adapter (S3-compatible)

- Хранит blob-артефакты.
- Именование: `{organization_id}/{document_id}/{version_id}/{artifact_type}` (соответствует unique constraint `(version_id, artifact_type)`).
- Операции: PutObject, GetObject, DeleteObject, HeadObject, GeneratePresignedURL.
- Retry: exponential backoff (3 попытки), circuit breaker (gobreaker) для защиты от каскадного отказа.

### Idempotency Store (Redis)

- Хранит `idempotency_key` → `{status, result_snapshot}` с TTL.
- TTL: 24 часа (конфигурируемо).

### Broker Client (RabbitMQ)

- Publish с подтверждением (publisher confirms).
- Subscribe с manual ack, configurable prefetch count (`DM_CONSUMER_PREFETCH`, default 10).
- Concurrency limiter — семафор (`DM_CONSUMER_CONCURRENCY`, default 5).
- Auto-reconnect при обрыве.
- Конфигурируемые exchange/queue names.
- Queue policies: `durable: true`, `x-max-length`, `x-message-ttl`.

### Health Check Handler

- `/healthz` — liveness: процесс жив.
- `/readyz` — readiness: PostgreSQL + Redis + RabbitMQ + Object Storage доступны.

---

# 7. Архитектура сервиса

DM реализуется как один Go-сервис (Monolith DM Service), обрабатывающий и async-события через RabbitMQ, и sync-запросы через REST API. Внутри — hexagonal архитектура с чётким разделением по слоям.

> Анализ вариантов и обоснование выбора — см. [ADR-001: Monolith DM Service](adr-001-monolith-dm-service.md).

При росте нагрузки в 10–100× сервис можно разделить на API + Worker без изменения доменной модели и контрактов — hexagonal архитектура позволяет это сделать заменой инфраструктурного слоя.

### Диаграмма сервиса

```
                    ┌─────────────────────────────────────┐
                    │        API / Backend Orchestrator    │
                    │   (sync: REST API calls to DM)      │
                    └─────────┬──────────┬────────────────┘
                              │          │
                     HTTP REST│          │ commands via broker
                              │          │
┌─────────────────────────────┴──────────┴─────────────────────────────┐
│                       Document Management Service                    │
│                                                                      │
│  ┌──────────────────┐    ┌───────────────────────────────┐           │
│  │   API Handler    │    │      Event Consumer           │           │
│  │  (sync ingress)  │    │     (async ingress)           │           │
│  └────────┬─────────┘    └──────┬────────────────────────┘           │
│           │                     │                                     │
│           │                     ▼                                     │
│           │              ┌──────────────┐                            │
│           │              │ Idempotency  │                            │
│           │              │    Guard     │                            │
│           │              └──────┬───────┘                            │
│           │                     │                                     │
│           ▼                     ▼                                     │
│  ┌──────────────────────────────────────────────┐                    │
│  │            Application Services               │                    │
│  │                                               │                    │
│  │  • Artifact Ingestion Service                 │                    │
│  │  • Version Management Service                 │                    │
│  │  • Artifact Query Service                     │                    │
│  │  • Document Lifecycle Service                 │                    │
│  │  • Diff Storage Service                       │                    │
│  └──────────┬────────────────────┬───────────────┘                    │
│             │                    │                                     │
│             ▼                    ▼                                     │
│  ┌──────────────────┐  ┌──────────────────────┐                      │
│  │ Confirmation Pub │  │ Notification Pub     │                      │
│  │ (→ DP, LIC, RE)  │  │ (→ LIC, RE, Orch)   │                      │
│  └────────┬─────────┘  └──────────┬───────────┘                      │
│           │                       │                                   │
│           └───────────┬───────────┘                                   │
│                       ▼                                               │
│            ┌─────────────────┐                                        │
│            │  Outbox Poller  │                                        │
│            └────────┬────────┘                                        │
│                     │                                                 │
│  INFRASTRUCTURE     │                                                 │
│  ┌──────────────────┼────────────────────────────────┐               │
│  │ PostgreSQL │ Redis │ Object Storage │ RabbitMQ    │               │
│  │ (metadata) │(idemp)│ (blobs)        │ (events)    │               │
│  └────────────┴───────┴────────────────┴─────────────┘               │
│                                                                      │
│  CROSS-CUTTING: Observability SDK · Health Check Handler             │
└──────────────────────────────────────────────────────────────────────┘
```

---

# 8. Сценарии работы

Sequence diagrams для каждого сценария — см. [sequence-diagrams.md](sequence-diagrams.md).

## 8.1 Сохранение артефактов после обработки документа

**Trigger:** Событие `DocumentProcessingArtifactsReady` от DP.

### Happy path

1. Event Consumer получает сообщение из `dp.artifacts.processing-ready`.
2. Десериализация → `DocumentProcessingArtifactsReady`.
3. Idempotency Guard: ключ `dp-artifacts:{job_id}` не найден → регистрация `PROCESSING`.
4. Event Router → Artifact Ingestion Service.
5. Валидация: `document_id` + `version_id` (выводится из `job_id` или передаётся в событии) существуют.
6. Для каждого артефакта (ocr_raw, text, structure, semantic_tree, warnings):
   a. Сериализация blob → JSON.
   b. PutObject в Object Storage → `storage_key`.
   c. Вычисление `content_hash`.
7. DB-транзакция:
   a. INSERT ArtifactDescriptor × N.
   b. UPDATE DocumentVersion SET artifact_status = `PROCESSING_ARTIFACTS_RECEIVED`.
   c. INSERT AuditRecord.
8. Redis: idempotency_key → `COMPLETED`.
9. Publish `DocumentProcessingArtifactsPersisted` в `dm.responses.artifacts-persisted`.
10. Publish `dm.events.version-artifacts-ready` для LIC.
11. ACK сообщения.

### Альтернативные ветки

**Документ не найден:** → Publish `DocumentProcessingArtifactsPersistFailed` с `error_code=DOCUMENT_NOT_FOUND`, `is_retryable=false`. ACK. DLQ не нужен (валидная ситуация — race condition или ошибка оркестратора).

**Object Storage недоступен:** → NACK с requeue. Retry с backoff на уровне брокера. После исчерпания retry (конфигурируемо, default 3) → `FAILED`, publish `DocumentProcessingArtifactsPersistFailed` с `is_retryable=true`. DLQ.

**DB-транзакция упала после Object Storage:** → Blob сохранён, метаданные — нет. Orphan blob. При повторной доставке: idempotency_key = `PROCESSING` → повторная обработка (перезапись blob, повторная транзакция). Orphan cleanup job удалит мёртвые blob.

### Статусные переходы

`DocumentVersion.artifact_status`: `PENDING` → `PROCESSING_ARTIFACTS_RECEIVED`.

## 8.2 Создание новой версии документа

**Trigger:** Sync API POST `/documents/{id}/versions`.

### Happy path

1. API Handler: десериализация, извлечение auth context.
2. Auth Context Extractor: `organization_id`, `user_id`.
3. Version Management Service:
   a. Проверка: документ существует, принадлежит организации, статус = `ACTIVE`.
   b. Проверка: `source_file_key` указывает на существующий объект в Object Storage (HEAD request).
   c. DB-транзакция:
      - INSERT DocumentVersion (version_number = max + 1, origin_type, parent_version_id = current_version_id).
      - UPDATE Document SET current_version_id = new_version_id, updated_at = now().
      - INSERT AuditRecord.
   d. Publish `dm.events.version-created` в брокер.
4. HTTP 201 Created с метаданными новой версии.

### Альтернативные ветки

**Документ не найден / чужая организация:** → HTTP 404.

**Документ archived/deleted:** → HTTP 409 Conflict.

**Concurrent version creation (race condition):** → `SELECT documents FOR UPDATE` для сериализации создания версий. `version_number = max + 1` вычисляется внутри транзакции. Unique constraint `(document_id, version_number)` как safety net. При конфликте — retry (до 3 раз).

## 8.3 Выдача semantic tree для сравнения версий

**Trigger:** Событие `GetSemanticTreeRequest` от DP.

### Happy path

1. Event Consumer → Idempotency Guard (`dp-tree-req:{job_id}:{version_id}`).
2. Event Router → Artifact Query Service.
3. Поиск ArtifactDescriptor: `version_id` + `artifact_type=SEMANTIC_TREE`.
4. GetObject из Object Storage → blob.
5. Десериализация blob → `SemanticTree`.
6. Publish `SemanticTreeProvided` в `dm.responses.semantic-tree-provided` с тем же `correlation_id`.
7. ACK.

### Альтернативные ветки

**Версия или артефакт не найдены:** → DM возвращает `SemanticTreeProvided` с `semantic_tree.root = null` и заполненными полями `error_code`, `error_message`, `is_retryable=false`. DP v1 определяет ошибку по `root == nil`, после TASK-055 — по `error_message` (ASSUMPTION-16).

**Object Storage недоступен:** → NACK, retry. После исчерпания → DLQ. DP получит таймаут на уровне Pending Response Registry.

### Статусные переходы

Нет изменений в статусах версии — это read-операция.

## 8.4 Сохранение результата сравнения версий

**Trigger:** Событие `DocumentVersionDiffReady` от DP.

### Happy path

1. Event Consumer → Idempotency Guard (`dp-diff:{job_id}`).
2. Event Router → Diff Storage Service.
3. Валидация: `base_version_id` и `target_version_id` существуют, принадлежат одному документу.
4. Сериализация diff → JSON blob.
5. PutObject в Object Storage.
6. DB-транзакция:
   a. INSERT VersionDiffReference.
   b. INSERT AuditRecord.
7. Publish `DocumentVersionDiffPersisted` в `dm.responses.diff-persisted`.
8. ACK.

### Альтернативные ветки

**Diff уже существует (повторная доставка):** → Idempotency Guard: `COMPLETED` → повторная публикация `DocumentVersionDiffPersisted`. ACK.

**Версия не найдена:** → Publish `DocumentVersionDiffPersistFailed` с `error_code=VERSION_NOT_FOUND`, `is_retryable=false`.

## 8.5 Сохранение результатов LIC

**Trigger:** Событие `LegalAnalysisArtifactsReady` от LIC.

### Happy path

Аналогично сценарию 8.1, но:
- Idempotency key: `lic-artifacts:{job_id}`.
- Артефакты: `CLASSIFICATION_RESULT`, `KEY_PARAMETERS`, `RISK_ANALYSIS`, `RISK_PROFILE`, `RECOMMENDATIONS`, `SUMMARY`, `DETAILED_REPORT`, `AGGREGATE_SCORE`.
- artifact_status: `PROCESSING_ARTIFACTS_RECEIVED` → `ANALYSIS_ARTIFACTS_RECEIVED`.
- Notification: `dm.events.version-analysis-ready` для RE.
- Confirmation: `dm.responses.lic-artifacts-persisted` / `dm.responses.lic-artifacts-persist-failed` для LIC.

## 8.6 Сохранение результатов Reporting Engine

**Trigger:** Событие `ReportsArtifactsReady` от RE.

### Happy path

Аналогично сценарию 8.1, но:
- Idempotency key: `re-reports:{job_id}`.
- Артефакты: `EXPORT_PDF`, `EXPORT_DOCX`.
- artifact_status: `ANALYSIS_ARTIFACTS_RECEIVED` → `REPORTS_READY` (или `FULLY_READY`, если это последний этап).
- Notification: `dm.events.version-reports-ready` для оркестратора.
- Confirmation: `dm.responses.re-reports-persisted` / `dm.responses.re-reports-persist-failed` для RE.

**Как RE получает данные для формирования отчёта:**

RE подписывается на `dm.events.version-analysis-ready`. По получению события RE публикует `GetArtifactsRequest` в `re.requests.artifacts` с указанием нужных типов (`artifact_types: [RISK_ANALYSIS, RISK_PROFILE, SUMMARY, DETAILED_REPORT, KEY_PARAMETERS, AGGREGATE_SCORE]`). DM читает запрошенные артефакты из Object Storage и отвечает событием `ArtifactsProvided` в `dm.responses.artifacts-provided`. RE получает все артефакты в одном сообщении, формирует отчёт, затем публикует `ReportsArtifactsReady`.

> Паттерн единообразен для всех доменов: DP запрашивает semantic tree через `GetSemanticTreeRequest`, LIC и RE запрашивают артефакты через `GetArtifactsRequest`. Все взаимодействия — async через RabbitMQ.

## 8.7 Получение артефактов для API / UI

API/Backend-оркестратор читает метаданные и артефакты через sync API DM. Для blob-артефактов: DM генерирует signed URL (presigned S3 URL) с TTL.

## 8.8 Повторная доставка одного и того же события

**Trigger:** At-least-once delivery → дубликат `DocumentProcessingArtifactsReady`.

1. Event Consumer → десериализация.
2. Idempotency Guard: ключ `dp-artifacts:{job_id}` найден, статус = `COMPLETED`.
3. ACK. Обработка не выполняется повторно. Confirmation уже доставлен через Outbox Poller при первой обработке.

**Стоимость:** один lookup в Redis. Повторная публикация confirmation **не выполняется** — это предотвращает дублирование notifications (BRE-002).

## 8.9 Ошибка частичного сохранения

**Сценарий:** Из 5 артефактов DP 3 сохранены в Object Storage, на 4-м — ошибка.

### Поведение

1. Artifact Ingestion Service **не коммитит** DB-транзакцию до успешного сохранения **всех** blob.
2. При ошибке Object Storage на любом артефакте:
   a. Rollback DB-транзакции.
   b. Удаление уже сохранённых blob (compensation).
   c. NACK → retry.
3. При retry: полная повторная загрузка всех blob (идемпотентно — перезапись).
4. Если retry исчерпан:
   a. Publish `DocumentProcessingArtifactsPersistFailed` с `is_retryable=true`.
   b. DLQ.
   c. `artifact_status` остаётся `PENDING`.

**Альтернатива (partial save):** Сохранять то, что удалось, с `artifact_status=PARTIALLY_AVAILABLE`. Не рекомендуется для v1: усложняет логику downstream-доменов и не даёт гарантий целостности набора.

## 8.10 Конфликт версии

**Сценарий:** Два одновременных запроса на создание версии для одного документа.

1. Запрос A и запрос B параллельно выполняют INSERT с `version_number = max + 1`.
2. Unique constraint `(document_id, version_number)` → один из запросов получает constraint violation.
3. Проигравший запрос: retry с пересчётом `version_number`.
4. Optimistic locking: UPDATE Document SET current_version_id WHERE updated_at = expected → rows_affected = 0 → retry.
5. Максимум 3 retry. Если не удалось → HTTP 409 Conflict.

## 8.11 Таймаут или недоступность зависимого хранилища

### PostgreSQL недоступен

1. Sync API: HTTP 503 Service Unavailable. Readiness probe → not ready → load balancer перестаёт направлять трафик.
2. Async: NACK с requeue. Сообщения остаются в очереди до восстановления.

### Object Storage недоступен

1. Запись: NACK, retry. При исчерпании → DLQ + `PersistFailed` с `is_retryable=true`.
2. Чтение (semantic tree request): NACK, retry. При исчерпании → DLQ. DP получит таймаут.

### Redis недоступен

1. Idempotency Guard: fallback на проверку по DB (`ArtifactDescriptor` exists by `job_id` + `artifact_type`). Деградация производительности, но не блокировка.
2. Если DB тоже недоступна → NACK, requeue.

---

# 9. Интеграции и контракты

Входящие/исходящие события, sync API, топики RabbitMQ, envelope, корреляция, versioning сообщений — см. [integration-contracts.md](integration-contracts.md).

Полный каталог всех событий с JSON schema — см. [event-catalog.md](event-catalog.md).

OpenAPI 3.0 спецификация sync REST API — см. [api-specification.yaml](api-specification.yaml).

---

# 10. Хранение и консистентность

PostgreSQL схема, Object Storage, transaction boundaries, idempotency, transactional outbox, deduplication, retention — см. [storage.md](storage.md).

---

# 11. Статусы, ошибки и отказоустойчивость

Внешние/внутренние статусы, retryable/non-retryable errors, DLQ, timeout policy — см. [error-handling.md](error-handling.md).

---

# 12. Безопасность, доступ и аудит

Multi-tenancy, RBAC, audit trail, шифрование at rest / in transit, presigned URLs — см. [security.md](security.md).

---

# 13. Наблюдаемость и эксплуатация

Structured logging, Prometheus metrics, OpenTelemetry tracing, алерты и operational dashboards — см. [observability.md](observability.md).

---

