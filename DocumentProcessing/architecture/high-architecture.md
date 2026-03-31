# Верхнеуровневая архитектура Document Processing v2

В рамках документа описана архитектура **доменной области Document Processing** сервиса **ContractPro** до уровня компонентов.

Документ является переработанной версией `high-architecture.md`.

---

# 1. Границы документа

## 1.1. Что входит в границы Document Processing

Document Processing (DP) — stateless-домен, отвечающий за:

* асинхронную обработку **документа**;
* прием команды на обработку через брокер сообщений (event-driven);
* валидацию входных параметров задачи обработки;
* извлечение текста из документа;
* выделение структуры документа (разделы, пункты, подпункты, приложения);
* распознавание блока реквизитов сторон (при наличии);
* построение **semantic tree**;
* формирование warnings о возможной неполноте анализа;
* сравнение версий документа (diff-логика внутри Document Processing):
  * **текстовый diff**,
  * **структурный diff по semantic tree**;
* передачу артефактов в **Document Management** для постоянного хранения;
* публикацию событий статуса и результата обработки/сравнения;
* очистку временных артефактов после завершения обработки.

---

# 2. Требования к системе

## 2.1. Модель предметной области

Модель предметной области **в границах домена Document Processing**.

### 2.1.1. Основные сущности

1. **ProcessingJob** — асинхронная задача на обработку входного PDF. Идентифицируется `job_id`. Содержит `InputDocumentReference`.

2. **ComparisonJob** — асинхронная задача сравнения двух версий документа. Идентифицируется `job_id`. Содержит ссылки на `base_version_id` и `target_version_id`.

3. **InputDocumentReference** — ссылка на исходный файл документа. Метаданные: `document_id`, имя, размер, mime-type, опционально checksum.

4. **ExtractedText** — нормализованный извлеченный текст документа.

5. **DocumentStructure** — логическая структура документа: разделы, пункты, подпункты, приложения, блок реквизитов сторон (при наличии).

6. **SemanticTree** — семантическое дерево документа, построенное на основе текста и структуры. Используется для анализа другими доменами и для сравнения версий.

7. **ProcessingWarning** — предупреждение о возможной неполноте анализа. В v1 — общий warning без координат проблемных фрагментов. Формируется компонентами pipeline (OCR, Text Extraction, Structure Extraction) и агрегируется WarningCollector.

8. **OCRRawArtifact** — сырой результат OCR (если применялся). Для text-PDF: `not_applicable`.

9. **VersionDiffResult** — результат сравнения версий: текстовый diff + структурный diff по semantic tree.

10. **TemporaryArtifacts** — промежуточные данные, живущие только в рамках выполнения задачи.

### 2.1.2. Связи сущностей

* `ProcessingJob` → использует `InputDocumentReference`.
* `ProcessingJob` → формирует `OCRRawArtifact`, `ExtractedText`, `DocumentStructure`, `SemanticTree`, `ProcessingWarning`.
* `ProcessingJob` → отправляет артефакты в **Document Management**.
* `ComparisonJob` → запрашивает из Document Management две версии `SemanticTree`.
* `ComparisonJob` → формирует `VersionDiffResult` и отправляет в Document Management.
* `TemporaryArtifacts` сопровождают обе задачи и удаляются после завершения.

### 2.1.3. Состояния задачи

#### Внешние статусы (для UI / API / оркестратора)

| Статус | Описание |
|--------|----------|
| `QUEUED` | Задача принята и поставлена в очередь |
| `IN_PROGRESS` | Задача обрабатывается |
| `COMPLETED` | Успешно завершена |
| `COMPLETED_WITH_WARNINGS` | Завершена успешно, но есть warnings |
| `FAILED` | Завершена с ошибкой (non-retryable или исчерпаны retry) |
| `TIMED_OUT` | Превышен таймаут задачи (120 сек) |
| `REJECTED` | Отклонена на валидации (формат/размер/страницы/метаданные) |

#### Внутренние стадии (для логов / метрик / технических событий)

**Processing pipeline:**
`VALIDATING_INPUT` → `FETCHING_SOURCE_FILE` → `VALIDATING_FILE` → `OCR` (или `OCR_SKIPPED`) → `TEXT_EXTRACTION` → `STRUCTURE_EXTRACTION` → `SEMANTIC_TREE_BUILDING` → `SAVING_ARTIFACTS` → `WAITING_DM_CONFIRMATION` → `CLEANUP_TEMP_ARTIFACTS`

**Comparison pipeline:**
`VALIDATING_INPUT` → `REQUESTING_SEMANTIC_TREES` → `WAITING_DM_RESPONSE` → `EXECUTING_DIFF` → `SAVING_COMPARISON_RESULT` → `WAITING_DM_CONFIRMATION`

Разделение на внешние статусы и внутренние стадии позволяет клиентам получать простой набор статусов, а эксплуатации — подробную телеметрию.

---

## 2.2. Глоссарий

* **ContractPro** — продукт для проверки/создания договоров с использованием AI в юрисдикции РФ.
* **Document Processing (DP)** — домен обработки документов.
* **Document Management (DM)** — домен постоянного хранения документов, версий, артефактов.
* **Legal Intelligence Core (LIC)** — домен юридического анализа.
* **OCR** — оптическое распознавание символов.
* **Semantic Tree** — структурно-семантическое представление документа.
* **Diff** — результат сравнения двух версий документа.
* **Message Broker** — брокер сообщений для обмена командами и событиями между доменами.
* **Correlation ID** — идентификатор корреляции для связывания событий одной бизнес-операции.
* **Idempotency** — свойство повторного выполнения команды без дублирования результата.
* **DLQ (Dead Letter Queue)** — очередь сообщений, которые не удалось обработать после исчерпания retry.
* **Retryable error** — временная ошибка, допускающая повторную попытку.
* **Non-retryable error** — ошибка, при которой повтор бессмысленен.

---

## 2.3. Контекст взаимодействия Document Processing

* **API/backend-оркестратор** публикует команду в брокер для DP.
* **DP** обрабатывает документ асинхронно.
* **DP** отправляет артефакты в **Document Management** через события.
* **Document Management** сохраняет артефакты и создает новую версию после успешной обработки.
* **DP** публикует статусные события для API/backend-оркестратора и других потребителей.
* **Legal Intelligence Core** читает данные из **Document Management**, не из DP напрямую.
* Для сравнения версий **DP** запрашивает semantic trees из DM через события и возвращает результат сравнения асинхронно.

### Контекстная диаграмма

```
                +----------------------------------+
                |       API / Backend Orchestrator  |
                |  (публикует команды в брокер)     |
                +-----------------+----------------+
                                  |
                                  | commands
                                  v
                      +------------------------+
                      |     Message Broker      |
                      +-----------+------------+
                                  |
                                  v
              +--------------------------------------+
              |         Document Processing          |
              |  (stateless + временные артефакты)    |
              +----------------+---------------------+
                               |
          +--------------------+--------------------+
          |                                         |
          | events (artifacts, status,              |
          | requests, responses)                    |
          v                                         v
+-------------------------+              +----------------------+
|   Document Management   |              |  External OCR Service |
|  (versions + artifacts) |              | (Yandex Cloud Vision) |
+-------------------------+              +----------------------+
          ^
          |
          | data read by other domains
          |
+-------------------------+
| Legal Intelligence Core |
+-------------------------+
```

---

## 2.4. Требования и ограничения

### 2.4.1. Пользовательские требования, релевантные для DP

* UR-1. Загрузка договора и получение статуса обработки — DP должен поддерживать асинхронный lifecycle статусов.
* UR-9. Повторная проверка версий — DP участвует в части сравнения версий (diff-логика).

### 2.4.2. Функциональные требования, релевантные для DP

**Загрузка и подготовка документа:**
* FR-1.1.1 — прием форматов (.doc/.docx/.pdf). В DP v1 — только PDF.
* FR-1.2.1 — OCR для скан-PDF через внешний сервис.
* FR-1.2.2 — warning при частично нечитаемом тексте. В v1 — общий warning без координат.
* FR-1.3.1 — выделение разделов/пунктов/подпунктов/приложений.
* FR-1.3.2 — распознавание блока реквизитов сторон.

**Результаты проверки и отчётность:**
* FR-5.3.1 — сравнение двух версий договора (diff-логика в DP).
* FR-5.3.2 — текстовый + структурный diff в зоне DP; изменения риск-профиля — другие домены.

### 2.4.3. Нефункциональные требования, влияющие на DP

* NFR-1.1 — до 60 сек для текстового договора 30–40 стр.
* NFR-1.2 — до 120 сек для сканированного договора с OCR.
* NFR-1.4 — горизонтальное масштабирование.
* NFR-2.4 / 2.5 — отказоустойчивость и работа при деградации.
* NFR-3.1 — TLS.
* NFR-3.4 / 9 — журналирование действий и ошибок.
* NFR-8.7 — мониторинг, метрики, нагрузка, очереди.

### 2.4.4. Ограничения DP v1

**Функциональные:**
* Входной формат — только PDF.
* Warning — общий, без координат проблемных фрагментов.
* Результаты сравнения — асинхронно через событие.
* Версия в DM создается после успешной обработки.

**Нефункциональные:**
* Максимальный размер файла — 20 МБ.
* Максимум страниц — 100.
* Таймаут задачи — 120 секунд.
* Нагрузка на старте — ~1000 договоров/сутки.
* OCR — Yandex Cloud Vision OCR.
* Временное хранилище — Yandex Object Storage.
* Взаимодействие — асинхронное, через брокер сообщений.
* Обязательные артефакты для DM: `ocr_raw`, `text`, `structure`, `semantic_tree`, `warnings`.

---

# 3. Архитектурные представления

## 3.1. Компоненты системы

Компоненты домена Document Processing разделены на слои по ответственности.

### Слой входа (Ingress)

| № | Компонент | Назначение |
|---|-----------|-----------|
| 1 | **Command Consumer** | Точка входа: получение команд из брокера (`ProcessDocumentRequested`, `CompareDocumentVersionsRequested`). Десериализация и первичная валидация контракта сообщения (наличие обязательных полей, формат). |
| 2 | **Idempotency Guard** | Проверка, не обрабатывалась ли уже команда с данным `job_id`. Предотвращает дублирование обработки при повторной доставке сообщений. Использует key-value store с TTL. |

### Слой оркестрации (Application)

| № | Компонент | Назначение |
|---|-----------|-----------|
| 3 | **Processing Pipeline Orchestrator** | Оркестрация шагов обработки документа: валидация → скачивание → OCR → извлечение текста → извлечение структуры → построение semantic tree → сохранение артефактов → cleanup. Использует Job Lifecycle Manager для статусов и таймаутов. |
| 4 | **Comparison Pipeline Orchestrator** | Оркестрация шагов сравнения версий: валидация → запрос semantic trees из DM → ожидание ответов → выполнение diff → сохранение результата. Использует Pending Response Registry для корреляции ответов от DM. |
| 5 | **Job Lifecycle Manager** (внутренний модуль) | Переиспользуемая логика управления жизненным циклом задачи: переходы между внешними статусами, контроль таймаута (120 сек), инициирование cleanup при завершении/ошибке. Используется обоими оркестраторами. |
| 6 | **Warning Collector** (внутренний модуль) | Агрегация warnings от компонентов pipeline (OCR, Text Extraction, Structure Extraction). Каждый компонент возвращает свои warnings, Collector формирует итоговый список. |

### Слой обработки (Domain / Processing Engines)

| № | Компонент | Назначение |
|---|-----------|-----------|
| 7 | **Input Validator** | Валидация бизнес-ограничений по метаданным команды до скачивания файла (заявленный размер > 20 МБ → REJECTED без скачивания, невалидный mime-type и т.д.). |
| 8 | **Source File Fetcher** | Скачивание PDF по `file_url`. Сохранение во временное хранилище. Проверка фактического размера (≤ 20 МБ), реального формата (PDF), количества страниц (≤ 100). |
| 9 | **OCR Integration Adapter** | Интеграция с Yandex Cloud Vision OCR. Включает rate limiter для защиты внешнего сервиса от перегрузки. Для text-PDF — пропуск с результатом `ocr_raw = not_applicable`. Формирует warnings при проблемах распознавания. |
| 10 | **Text Extraction & Normalization Engine** | Извлечение и нормализация текста из PDF (или из OCR-результата). Формирует warnings при обнаружении пустых страниц, мусорных символов и т.д. |
| 11 | **Structure Extraction Engine** | Выделение логической структуры: разделы, пункты, подпункты, приложения, блок реквизитов сторон. Формирует warnings при невозможности определить структуру. |
| 12 | **Semantic Tree Builder** | Построение semantic tree на основе ExtractedText и DocumentStructure. |
| 13 | **Version Comparison Engine** | Сравнение двух версий документа: текстовый diff + структурный diff по semantic tree. |

### Слой выхода (Egress / Adapters)

| № | Компонент | Назначение |
|---|-----------|-----------|
| 14 | **DM Outbound Adapter** | Публикация событий в Document Management: `DocumentProcessingArtifactsReady` (артефакты обработки), `GetSemanticTreeRequest` (запрос semantic tree для сравнения), `DocumentVersionDiffReady` (результат сравнения). |
| 15 | **DM Inbound Adapter** | Подписка на ответы от Document Management: `DocumentProcessingArtifactsPersisted`, `DocumentProcessingArtifactsPersistFailed`, `SemanticTreeProvided`, `DocumentVersionDiffPersisted`, `DocumentVersionDiffPersistFailed`. Корреляция ответов по `correlation_id` / `job_id`, уведомление соответствующего оркестратора. |
| 16 | **Event Publisher** | Публикация статусных событий (`StatusChangedEvent`), событий завершения (`ProcessingCompletedEvent`, `ComparisonCompletedEvent`) и событий ошибок (`ProcessingFailedEvent`, `ComparisonFailedEvent`) для внешних потребителей (API/backend-оркестратор, другие домены). События ошибок содержат `error_code`, `error_message`, `failed_at_stage`, `is_retryable`. |
| 17 | **Temporary Artifact Storage Adapter** | Работа с временными артефактами в Yandex Object Storage: сохранение, чтение, удаление при cleanup. |

### Слой инфраструктуры (Cross-cutting)

| № | Компонент | Назначение |
|---|-----------|-----------|
| 18 | **Pending Response Registry** (внутренний модуль) | Регистрация ожидаемых ответов от DM при сравнении версий. Отслеживание полученных ответов, уведомление оркестратора при сборе всех ответов, обработка таймаута отдельного ответа. |
| 19 | **Concurrency Limiter** | Ограничение числа одновременно обрабатываемых задач на экземпляре DP (worker pool / семафор). Настраиваемый параметр `max_concurrent_jobs`. |

**Observability (метрики, трейсинг, structured logging)** — не является отдельным доменным компонентом. Все компоненты используют общий observability SDK для:
* structured logging с `job_id`, `document_id`, `correlation_id`, внутренней стадией;
* метрик (время обработки по стадиям, счетчики статусов, размеры файлов, OCR latency);
* distributed tracing (propagation trace context через события).

---

## 3.2. Ключевые архитектурные решения

В этом разделе зафиксированы обоснования ключевых архитектурных решений, принятых при проектировании компонентов DP.

### ADR-1. Warning Collector вместо отдельного Warning Detector

**Решение:** Warnings формируются непосредственно компонентами pipeline (OCR Integration Adapter, Text Extraction Engine, Structure Extraction Engine) и агрегируются внутренним модулем Warning Collector.

**Обоснование:** Предупреждения о неполноте анализа — побочный продукт работы компонентов извлечения. Именно OCR знает о нечитаемых фрагментах, Text Extraction — о пустых страницах, Structure Extraction — о невозможности выделить структуру. Отдельный детектор, запускаемый после них, не имеет доступа к контексту ошибок и был бы вынужден либо дублировать логику анализа, либо быть простым агрегатором. Warning Collector честно выполняет роль агрегатора, не притворяясь детектором.

### ADR-2. Разделение DM-адаптера на Outbound и Inbound

**Решение:** Взаимодействие с Document Management разделено на DM Outbound Adapter (публикация событий) и DM Inbound Adapter (подписка на ответы и корреляция).

**Обоснование:** Outbound-логика (сериализация артефактов, формирование событий, retry отправки) и inbound-логика (подписка на топики, десериализация, корреляция ответов по `correlation_id`, обработка таймаутов ожидания) — разные задачи с разными паттернами ошибок и жизненными циклами. Разделение позволяет масштабировать и тестировать их независимо.

### ADR-3. Idempotency Guard на входе pipeline

**Решение:** Между Command Consumer и оркестратором размещен Idempotency Guard, проверяющий `job_id` через key-value store с TTL.

**Обоснование:** В event-driven системе с гарантией at-least-once delivery одна команда может прийти повторно. Без явного механизма идемпотентности повторная команда приведет к дублированию обработки, артефактов в DM и статусных событий. Guard на входе отсекает дубликаты до начала обработки.

### ADR-4. Два оркестратора + общий Job Lifecycle Manager

**Решение:** Вместо единого Processing Orchestrator введены Processing Pipeline Orchestrator и Comparison Pipeline Orchestrator, использующие общий внутренний модуль Job Lifecycle Manager.

**Обоснование:** Два workflow имеют разные шаги, входные данные и паттерны взаимодействия с DM (write-only при обработке vs request-response при сравнении). Объединение в одном компоненте приводит к сложному коду с ветвлениями по типу задачи. Общая логика (статусные переходы, таймауты, cleanup) вынесена в Job Lifecycle Manager, чтобы избежать дублирования.

### ADR-5. Observability как cross-cutting concern, а не компонент pipeline

**Решение:** Observability (метрики, трейсинг, structured logging) не выделен как отдельный доменный компонент. Все компоненты используют общий observability SDK.

**Обоснование:** Observability — инфраструктурный слой, а не шаг обработки. Размещение на одном уровне с бизнес-компонентами создает ложное впечатление, что это часть pipeline. На практике каждый компонент самостоятельно пишет логи и отправляет метрики через общий SDK.

### ADR-6. Трёхфазная валидация

**Решение:** Валидация разделена на три фазы: Command Consumer (контракт сообщения) → Input Validator (бизнес-ограничения по метаданным, до скачивания) → Source File Fetcher (фактические ограничения файла, после скачивания).

**Обоснование:** Если заявленный размер файла в метаданных > 20 МБ, нет смысла скачивать файл — можно сразу reject. Первая фаза (контракт) ловит невалидные сообщения, вторая (метаданные) — заведомо недопустимые файлы без затрат на сеть, третья (файл) — фактические нарушения ограничений.

### ADR-7. Pending Response Registry для корреляции асинхронных ответов

**Решение:** В сценарии сравнения версий введен Pending Response Registry — модуль, отслеживающий ожидаемые и полученные ответы от DM.

**Обоснование:** Асинхронный request-response через брокер требует: связи ответа с запросом по `correlation_id`, хранения состояния ожидания, обработки частичных ответов (пришел один semantic tree, второй — нет) и таймаута отдельного ответа. Без явного механизма это превращается в ad-hoc реализацию с высоким риском ошибок.

### ADR-8. Concurrency Limiter и OCR Rate Limiter

**Решение:** Введены Concurrency Limiter (ограничение одновременных задач на экземпляре) и rate limiter внутри OCR Integration Adapter.

**Обоснование:** Горизонтальное масштабирование (NFR-1.4) без backpressure при пиковой нагрузке приведет к одновременному обращению всех экземпляров к Yandex Cloud Vision OCR, rate limiting со стороны OCR, каскадным таймаутам и массовому переводу задач в FAILED/TIMED_OUT. Лимиты (`max_concurrent_jobs`, `ocr_rps_limit`) конфигурируемы.

---

## 3.3. Диаграмма компонентов (верхний уровень)

```
                              +-------------------------+
                              | API / Backend Orchestr.  |
                              +------------+------------+
                                           |
                                           | command events
                                           v
+==========================================================================+
|                         Document Processing                              |
|                                                                          |
|  INGRESS                                                                 |
|  ~~~~~~~~                                                                |
|  [Command Consumer] --> [Idempotency Guard]                              |
|                                |                                         |
|                    +-----------+-----------+                              |
|                    |                       |                              |
|  ORCHESTRATION     v                       v                             |
|  ~~~~~~~~~~~~~                                                           |
|  [Processing Pipeline    [Comparison Pipeline                            |
|   Orchestrator]           Orchestrator]                                  |
|       |                        |                                         |
|       | uses                   | uses                                    |
|       v                        v                                         |
|  [Job Lifecycle Manager]  [Pending Response Registry]                    |
|  [Warning Collector]                                                     |
|                                                                          |
|  PROCESSING ENGINES                                                      |
|  ~~~~~~~~~~~~~~~~~~                                                      |
|  Processing pipeline:          Comparison pipeline:                      |
|  [Input Validator]             [Version Comparison Engine]               |
|       |                                                                  |
|       v                                                                  |
|  [Source File Fetcher] -----> [Temp Artifact Storage Adapter]            |
|       |                        (Yandex Object Storage)                   |
|       v                                                                  |
|  [OCR Integration Adapter] ----------------------------------------> [External OCR]
|       |                                                                  |
|       v                                                                  |
|  [Text Extraction & Normalization]                                       |
|       |                                                                  |
|       v                                                                  |
|  [Structure Extraction Engine]                                           |
|       |                                                                  |
|       v                                                                  |
|  [Semantic Tree Builder]                                                 |
|                                                                          |
|  EGRESS                                                                  |
|  ~~~~~~                                                                  |
|  [DM Outbound Adapter] --events--> [Message Broker] --> [Doc Management] |
|  [DM Inbound Adapter]  <--events-- [Message Broker] <-- [Doc Management] |
|  [Event Publisher] --status events--> [Message Broker]                   |
|                                                                          |
|  CROSS-CUTTING: [Concurrency Limiter] · Observability SDK                |
+==========================================================================+
```

---

## 3.3.1. Реестр топиков брокера сообщений

Именование топиков следует иерархическому стандарту `{домен}.{тип}.{действие}`.

### DP — входящие команды (Command Consumer подписывается)

| Топик | Описание |
|-------|----------|
| `dp.commands.process-document` | Команда на обработку документа (ProcessDocumentRequested) |
| `dp.commands.compare-versions` | Команда на сравнение версий (CompareDocumentVersionsRequested) |

### DP → DM — артефакты и запросы (DM Outbound Adapter публикует)

| Топик | Описание |
|-------|----------|
| `dp.artifacts.processing-ready` | Артефакты обработки готовы (DocumentProcessingArtifactsReady) |
| `dp.requests.semantic-tree` | Запрос semantic tree версии (GetSemanticTreeRequest) |
| `dp.artifacts.diff-ready` | Результат сравнения версий готов (DocumentVersionDiffReady) |

### DM → DP — ответы (DM Inbound Adapter подписывается)

| Топик | Описание |
|-------|----------|
| `dm.responses.artifacts-persisted` | Артефакты успешно сохранены (DocumentProcessingArtifactsPersisted) |
| `dm.responses.artifacts-persist-failed` | Ошибка сохранения артефактов (DocumentProcessingArtifactsPersistFailed) |
| `dm.responses.semantic-tree-provided` | Semantic tree предоставлен (SemanticTreeProvided) |
| `dm.responses.diff-persisted` | Результат сравнения сохранён (DocumentVersionDiffPersisted) |
| `dm.responses.diff-persist-failed` | Ошибка сохранения результата сравнения (DocumentVersionDiffPersistFailed) |

### DP → внешние потребители (Event Publisher публикует)

| Топик | Описание |
|-------|----------|
| `dp.events.status-changed` | Изменение статуса задачи (StatusChangedEvent) |
| `dp.events.processing-completed` | Обработка завершена успешно (ProcessingCompletedEvent) |
| `dp.events.processing-failed` | Обработка завершена с ошибкой (ProcessingFailedEvent) |
| `dp.events.comparison-completed` | Сравнение завершено успешно (ComparisonCompletedEvent) |
| `dp.events.comparison-failed` | Сравнение завершено с ошибкой (ComparisonFailedEvent) |

Все имена топиков конфигурируются через переменные окружения (см. `DocumentProcessing/architecture/configuration.md`).

---

## 3.4. Описание поведения системы

### 3.4.1. Сценарий "Инициализация системы"

1. Запускаются экземпляры компонентов DP.
2. Загружается конфигурация:
   * настройки брокера сообщений и топиков/очередей;
   * лимиты обработки (`max_file_size`: 20 МБ, `max_pages`: 100, `job_timeout`: 120 сек);
   * `max_concurrent_jobs`, `ocr_rps_limit`;
   * настройки Yandex Object Storage;
   * настройки OCR-интеграции;
   * настройки retry/DLQ;
   * настройки Idempotency Guard (TTL, backend);
   * настройки HTTP-сервера (health/readiness probes).
3. Инициализация подключений:
   * к брокеру сообщений,
   * к Yandex Object Storage,
   * к Yandex Cloud Vision OCR (healthcheck),
   * к key-value store (Idempotency Guard),
   * к системе observability.
4. Command Consumer подписывается на команды: `dp.commands.process-document`, `dp.commands.compare-versions`.
5. DM Inbound Adapter подписывается на ответы: `dm.responses.artifacts-persisted`, `dm.responses.artifacts-persist-failed`, `dm.responses.semantic-tree-provided`, `dm.responses.diff-persisted`, `dm.responses.diff-persist-failed`.
6. Система переводится в состояние readiness.
7. Начинается потребление сообщений.

```
[Start]
   |
   v
[Load configuration]
   |
   v
[Init connections: Broker, YOS, OCR, KV-store, Observability]
   |
   v
[Command Consumer: subscribe to command topics]
   |
   v
[DM Inbound Adapter: subscribe to DM response topics]
   |
   v
[Readiness OK]
   |
   v
[Consume commands]
```

---

### 3.4.2. Сценарий "Обработка загруженного PDF"

#### Входные данные команды `ProcessDocumentRequested`

Обязательные поля:
* `job_id`
* `document_id`
* `version_id` (идентификатор версии документа в DM)
* `file_url` (ссылка на PDF)

Рекомендуемые поля:
* имя файла, размер, mime-type, checksum
* `organization_id`, `requested_by_user_id`

#### Шаги сценария

1. **Command Consumer** получает сообщение, валидирует контракт (наличие обязательных полей, формат).

2. **Idempotency Guard** проверяет `job_id`:
   * если задача уже завершена — ack сообщения без обработки;
   * если задача в процессе — ack без обработки;
   * если новая — регистрирует `job_id` и передает далее.

3. **Concurrency Limiter** проверяет доступность слота. Если слотов нет — сообщение остается в очереди (nack без retry penalty).

4. **Processing Pipeline Orchestrator** принимает задачу.

5. **Job Lifecycle Manager** публикует статус `IN_PROGRESS` через Event Publisher.

6. **Input Validator** проверяет бизнес-ограничения по метаданным:
   * заявленный размер > 20 МБ → `REJECTED`;
   * невалидный mime-type → `REJECTED`.

7. **Source File Fetcher** скачивает PDF по `file_url`:
   * проверяет доступность;
   * проверяет фактический размер (≤ 20 МБ);
   * проверяет, что файл — PDF;
   * проверяет количество страниц (≤ 100);
   * сохраняет во временное хранилище (Yandex Object Storage).
   * При нарушении ограничений → `REJECTED`, cleanup.

8. **OCR Integration Adapter**:
   * определяет, нужен ли OCR (text-PDF vs scan-PDF);
   * для text-PDF → `ocr_raw = not_applicable`, пропуск;
   * для scan-PDF → вызов Yandex Cloud Vision OCR (с rate limiting);
   * формирует warnings при проблемах распознавания → Warning Collector.

9. **Text Extraction & Normalization Engine** извлекает и нормализует текст.
   * Формирует warnings при обнаружении проблем → Warning Collector.

10. **Structure Extraction Engine** выделяет разделы, пункты, подпункты, приложения, блок реквизитов сторон.
    * Формирует warnings → Warning Collector.

11. **Semantic Tree Builder** строит `semantic_tree`.

12. **Warning Collector** формирует итоговый список warnings.

13. **DM Outbound Adapter** отправляет событие `DocumentProcessingArtifactsReady` с артефактами:
    * `ocr_raw`, `text`, `structure`, `semantic_tree`, `warnings`.

14. **DM Inbound Adapter** ожидает подтверждение:
    * `DocumentProcessingArtifactsPersisted` → успех;
    * `DocumentProcessingArtifactsPersistFailed` → ошибка.

15. При успехе:
    * Orchestrator запускает cleanup временных артефактов через Temporary Artifact Storage Adapter;
    * Job Lifecycle Manager публикует статус: `COMPLETED` (если warnings пуст) или `COMPLETED_WITH_WARNINGS`;
    * Event Publisher публикует `ProcessingCompletedEvent` (`job_id`, `document_id`, `status`, `has_warnings`, `warning_count`, `correlation_id`, `timestamp`);
    * Idempotency Guard обновляет статус `job_id` → завершен.

16. При ошибке:
    * retryable → retry-политика;
    * non-retryable или retry исчерпаны → `FAILED`, запись в DLQ, cleanup;
    * Event Publisher публикует `ProcessingFailedEvent` (`job_id`, `document_id`, `status`, `error_code`, `error_message`, `failed_at_stage`, `is_retryable=false`, `correlation_id`, `timestamp`).

17. При таймауте (> 120 сек):
    * Job Lifecycle Manager прерывает обработку → `TIMED_OUT`, cleanup;
    * Event Publisher публикует `ProcessingFailedEvent` (`job_id`, `document_id`, `status=TIMED_OUT`, `error_code`, `error_message`, `failed_at_stage`, `is_retryable=true`, `correlation_id`, `timestamp`).

#### Диаграмма сценария

```
[API/Backend Orchestrator]
         |
         | ProcessDocumentRequested
         v
    [Message Broker]
         |
         v
[Command Consumer] --validate contract--> OK
         |
         v
[Idempotency Guard] --check job_id--> new job
         |
         v
[Concurrency Limiter] --acquire slot--> OK
         |
         v
[Processing Pipeline Orchestrator]
         |
         +--> [Job Lifecycle Manager] --publish--> IN_PROGRESS
         |
         +--> [Input Validator] --validate metadata--> OK
         |
         +--> [Source File Fetcher] --download & validate file-->
         |         |                                     [Temp Storage]
         |         v
         +--> [OCR Adapter] (skip or call OCR) --warnings--> [Warning Collector]
         |
         +--> [Text Extraction] --warnings--> [Warning Collector]
         |
         +--> [Structure Extraction] --warnings--> [Warning Collector]
         |
         +--> [Semantic Tree Builder]
         |
         +--> [Warning Collector] --aggregate warnings-->
         |
         +--> [DM Outbound Adapter]
         |         --DocumentProcessingArtifactsReady-->
         |         [Broker] --> [Document Management]
         |
         +<-- [DM Inbound Adapter]
         |         <--DocumentProcessingArtifactsPersisted--
         |
         +--> [Temp Storage Adapter] --cleanup-->
         |
         +--> [Job Lifecycle Manager]
                   --publish--> COMPLETED / COMPLETED_WITH_WARNINGS
```

---

### 3.4.3. Сценарий "Сравнение версий документа"

#### Входные данные команды `CompareDocumentVersionsRequested`

Обязательные поля:
* `job_id`
* `document_id`
* `base_version_id`
* `target_version_id`

Рекомендуемые: `organization_id`, `requested_by_user_id`.

#### Шаги сценария

1. **Command Consumer** получает и валидирует сообщение.

2. **Idempotency Guard** проверяет `job_id`.

3. **Concurrency Limiter** проверяет доступность слота.

4. **Comparison Pipeline Orchestrator** принимает задачу.

5. **Job Lifecycle Manager** публикует статус `IN_PROGRESS`.

6. **DM Outbound Adapter** отправляет два запроса semantic tree:
   * `GetSemanticTreeRequest` для `base_version_id` (с `correlation_id_A`);
   * `GetSemanticTreeRequest` для `target_version_id` (с `correlation_id_B`).

7. **Pending Response Registry** регистрирует два ожидаемых ответа.

8. **DM Inbound Adapter** получает ответы `SemanticTreeProvided`, коррелирует по `correlation_id`, передает в Pending Response Registry.

9. **Pending Response Registry** уведомляет оркестратор, когда оба ответа получены.
   * Если один из ответов не получен в течение настраиваемого таймаута → `ComparisonFailedEvent` с `is_retryable=true`.
   * Если DM вернул ошибку (версия не найдена) → `FAILED`, `ComparisonFailedEvent` с `is_retryable=false`.

10. **Version Comparison Engine** выполняет текстовый diff и структурный diff по semantic tree.

11. **DM Outbound Adapter** отправляет `DocumentVersionDiffReady` с результатом сравнения.

12. **DM Inbound Adapter** ожидает подтверждение сохранения:
    * `DocumentVersionDiffPersisted` → успех;
    * `DocumentVersionDiffPersistFailed` → ошибка (содержит `is_retryable`).

13. При успехе:
    * `COMPLETED`;
    * Event Publisher публикует `ComparisonCompletedEvent` (`job_id`, `document_id`, `base_version_id`, `target_version_id`, `status`, `text_diff_count`, `structural_diff_count`, `correlation_id`, `timestamp`).

14. При ошибке:
    * retryable (`is_retryable=true` из `DocumentVersionDiffPersistFailed`) → retry-политика;
    * non-retryable или retry исчерпаны → `FAILED`;
    * Event Publisher публикует `ComparisonFailedEvent` (`job_id`, `document_id`, `status`, `error_code`, `error_message`, `failed_at_stage`, `is_retryable=false`, `correlation_id`, `timestamp`).

15. При таймауте (> 120 сек):
    * `TIMED_OUT`, cleanup;
    * Event Publisher публикует `ComparisonFailedEvent` (`job_id`, `document_id`, `status=TIMED_OUT`, `error_code`, `error_message`, `failed_at_stage`, `is_retryable=true`, `correlation_id`, `timestamp`).

#### Диаграмма сценария

```
[API/Backend Orchestrator]
         |
         | CompareDocumentVersionsRequested
         v
    [Message Broker]
         |
         v
[Command Consumer] --> [Idempotency Guard] --> [Concurrency Limiter]
         |
         v
[Comparison Pipeline Orchestrator]
         |
         +--> [Job Lifecycle Manager] --publish--> IN_PROGRESS
         |
         +--> [DM Outbound Adapter]
         |         --GetSemanticTree(base_version, correlation_A)-->  [Broker] --> [DM]
         |         --GetSemanticTree(target_version, correlation_B)--> [Broker] --> [DM]
         |
         +--> [Pending Response Registry] --register 2 expected responses-->
         |
         +<-- [DM Inbound Adapter]
         |         <--SemanticTreeProvided(A)-- [Broker] <-- [DM]
         |         <--SemanticTreeProvided(B)-- [Broker] <-- [DM]
         |
         +--> [Pending Response Registry] --all received, notify orchestrator-->
         |
         +--> [Version Comparison Engine] --text diff + structural diff-->
         |
         +--> [DM Outbound Adapter]
         |         --DocumentVersionDiffReady--> [Broker] --> [DM]
         |
         +<-- [DM Inbound Adapter] <--confirmation--
         |
         +--> [Job Lifecycle Manager]
                   --publish--> COMPLETED / FAILED / TIMED_OUT
```
