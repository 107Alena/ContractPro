# Progress Log — Document Processing

Журнал прогресса реализации доменной области Document Processing.

---

## Формат записи

```
### TASK-XXX — Краткое описание
**Статус:** done
**Дата:** YYYY-MM-DD
**Агент:** (идентификатор сессии)
**Summary:** Что было сделано, какие решения приняты, что стоит учесть.
**Коммиты:** hash1, hash2
```

---

## Записи

### TASK-001 — Инициализация Go-модуля и структуры проекта
**Статус:** done
**Дата:** 2026-03-12

**План реализации (согласован):**
1. Инициализировать Go-модуль `contractpro/document-processing` в `DocumentProcessing/development/`
2. Создать все 26 каталогов из acceptance criteria с `.gitkeep`
3. Создать минимальный `cmd/dp-worker/main.go` (package main, пустой main)
4. Создать `Makefile` с целями build, test, lint
5. Выполнить `go mod tidy` и прогнать все test_steps

**Summary:**
- Go module: `contractpro/document-processing` (Go 1.26.1)
- Все 26 каталогов созданы в hexagonal-архитектуре: domain (model, port), application (processing, comparison, lifecycle, warning), engine (validator, fetcher, ocr, textextract, structure, semantictree, comparison), ingress (consumer, idempotency), egress (dm, publisher, storage), infra (broker, kvstore, objectstorage, ocr, concurrency, observability), pdf
- Makefile: `make build` → `go build ./cmd/dp-worker/`, `make test` → `go test ./...`, `make lint` → `go vet ./...`
- Все test_steps пройдены: go mod tidy, make build, make test, make lint, ls -R

**Заметки для следующей итерации:**
- Следующие задачи для реализации: TASK-002..005 (доменные модели) и TASK-007 (конфигурация) — все зависят только от TASK-001

### TASK-002 — Доменные модели: задачи и статусы
**Статус:** in_progress
**Дата:** 2026-03-14

**План реализации (согласован):**
1. Создать `internal/domain/model/status.go`:
   - Тип `JobStatus string` с 7 константами (QUEUED, IN_PROGRESS, COMPLETED, COMPLETED_WITH_WARNINGS, FAILED, TIMED_OUT, REJECTED)
   - Карта валидных переходов `validTransitions`
   - Функция `ValidateTransition(from, to) error`
   - Метод `IsTerminal() bool`
2. Создать `internal/domain/model/stage.go`:
   - Тип `ProcessingStage string` с 11 константами
   - Тип `ComparisonStage string` с 6 константами
3. Создать `internal/domain/model/job.go`:
   - Embedded-структура `JobMeta` (job_id, status, created_at, updated_at)
   - `ProcessingJob` (JobMeta + document_id, file_url, stage, file_name, file_size, mime_type, checksum, org_id, user_id)
   - `ComparisonJob` (JobMeta + document_id, base_version_id, target_version_id, stage, org_id, user_id)
   - Конструкторы `NewProcessingJob()`, `NewComparisonJob()`
4. Создать `internal/domain/model/status_test.go`:
   - Table-driven тесты всех 49 комбинаций переходов (7×7)
   - Тесты `IsTerminal()` для всех 7 статусов
5. Создать `internal/domain/model/job_test.go`:
   - Тесты конструкторов
   - JSON serialization/deserialization round-trip

**Ключевые решения:**
- Enums через `type XxxStatus string` — прозрачная JSON-сериализация
- Transition map в model пакете (доменная логика)
- JobMeta embedded struct для общих полей ProcessingJob и ComparisonJob
- Раздельные типы ProcessingStage и ComparisonStage для type safety

**Статус:** done
**Summary:**
- Созданы 5 файлов в `internal/domain/model/`: status.go, stage.go, job.go, status_test.go, job_test.go
- 7 JobStatus, 11 ProcessingStage, 6 ComparisonStage — все string-константы
- ValidateTransition() + IsTerminal() с картой переходов
- 55 тестов, все проходят (включая 49 комбинаций переходов 7×7)
- go vet, make build — без ошибок

**Заметки для следующей итерации:**
- Следующие готовые задачи (critical, deps met): TASK-003, TASK-004, TASK-005, TASK-007
- TASK-006 (порты) зависит от TASK-002..005 — станет доступна после завершения 003-005
- .gitkeep в internal/domain/model/ можно удалить (появились реальные файлы)

### TASK-003 — Доменные модели: сущности документа
**Статус:** done
**Дата:** 2026-03-15

**План реализации (согласован):**
1. Создать `internal/domain/model/document.go`:
   - OCRStatus enum (applicable, not_applicable)
   - InputDocumentReference (document_id, file_name, file_size, mime_type, checksum)
   - PageText (page_number, text)
   - ExtractedText (document_id, pages []PageText) + метод FullText()
   - OCRRawArtifact (raw_text, status)
2. Создать `internal/domain/model/structure.go`:
   - SubClause (number, content)
   - Clause (number, content, sub_clauses)
   - Section (number, title, content, clauses)
   - Appendix (number, title, content)
   - PartyDetails (name, inn, ogrn, address, representative)
   - DocumentStructure (document_id, sections, appendices, party_details)
3. Создать `internal/domain/model/document_test.go`:
   - JSON round-trip для InputDocumentReference, ExtractedText, OCRRawArtifact
   - omitempty-проверки (checksum, raw_text)
   - Тест OCRStatus констант
   - Тест пустых pages
4. Создать `internal/domain/model/structure_test.go`:
   - JSON round-trip для DocumentStructure с полной иерархией (русскоязычный контент)
   - omitempty-проверки для Section, Clause, PartyDetails, DocumentStructure
   - Минимальный JSON-тест

**Ключевые решения:**
- OCRStatus как string enum (lowercase: "applicable", "not_applicable") — отличается от JobStatus (UPPER_SNAKE) т.к. это внутренний маркер, а не внешний статус
- PartyDetails как слайс в DocumentStructure ([]PartyDetails) — договор может иметь 2+ сторон
- Все вложенные слайсы (clauses, sub_clauses, appendices, party_details) с omitempty — минимальный JSON при неполной структуре

**Summary:**
- Созданы 2 файла моделей: document.go (4 типа + метод FullText), structure.go (6 типов)
- Созданы 2 файла тестов: document_test.go (8 тестов), structure_test.go (6 тестов)
- ExtractedText не дублирует текст: только Pages, полный текст через FullText()
- Итого 22 новых теста, все проходят
- go build, go vet, make build, make test — без ошибок
- Общее количество тестов в пакете model: 77

**Заметки для следующей итерации:**
- Следующие готовые задачи (critical, deps met): TASK-004, TASK-005, TASK-007, TASK-018
- TASK-006 (порты) зависит от TASK-003(done), TASK-004, TASK-005 — нужно завершить 004 и 005
- TASK-004 использует типы из этой задачи (DocumentStructure, ExtractedText) для SemanticTree

### TASK-004 — Доменные модели: SemanticTree, ProcessingWarning, VersionDiffResult, TemporaryArtifacts
**Статус:** done
**Дата:** 2026-03-15

**План реализации (согласован):**
1. Создать `internal/domain/model/semantic_tree.go`:
   - NodeType enum (ROOT, SECTION, CLAUSE, TEXT, APPENDIX, PARTY_DETAILS)
   - SemanticNode (ID, Type, Content, Metadata map[string]string, Children []*SemanticNode)
   - SemanticTree (DocumentID, Root *SemanticNode)
   - Walk(fn) — depth-first pre-order traversal с early stop
2. Создать `internal/domain/model/warning.go`:
   - ProcessingWarning (Code, Message, Stage ProcessingStage)
   - **Изменение от acceptance criteria:** вместо `source_component string` используется `Stage ProcessingStage` — привязка к существующему enum для type safety. Severity не добавляем (overengineering для v1).
3. Создать `internal/domain/model/diff.go`:
   - DiffType enum (added, removed, modified) — общий для текстового и структурного diff
   - TextDiffEntry (Type, Path, OldContent, NewContent)
   - StructuralDiffEntry (Type, NodeType, NodeID, Path, Description)
   - VersionDiffResult (DocumentID, BaseVersionID, TargetVersionID, TextDiffs[], StructuralDiffs[])
   - text_diffs и structural_diffs — независимые данные (два разных среза одного сравнения)
4. Создать `internal/domain/model/artifacts.go`:
   - TemporaryArtifacts (JobID, StorageKeys []string)
   - AddKey(), HasKeys() — вспомогательные методы
   - StorageKeys оставлен для гибкости (точечное удаление), хотя cleanup через DeleteByPrefix
5. Тесты для каждого файла: JSON round-trip, omitempty, Walk traversal, константы

**Ключевые решения:**
- SemanticNode.Children — указатели []*SemanticNode для мутабельности и эффективного обхода
- Metadata map[string]string — гибкость для хранения номера/заголовка без раздувания структуры
- DiffType общий для TextDiffEntry и StructuralDiffEntry
- ProcessingWarning.Stage вместо source_component — type safety через существующий ProcessingStage enum
- Severity не добавлен: v1 показывает warnings одним списком, градация не требуется

**Summary:**
- Созданы 4 файла моделей: semantic_tree.go, warning.go, diff.go, artifacts.go
- Созданы 4 файла тестов: semantic_tree_test.go, warning_test.go, diff_test.go, artifacts_test.go
- 22 новых теста, все проходят
- go build, go vet — без ошибок
- Общее количество тестов в пакете model: 99

**Заметки для следующей итерации:**
- Следующие готовые задачи (critical, deps met): TASK-005, TASK-007, TASK-018
- TASK-006 (порты) зависит от TASK-004(done), TASK-005 — нужно завершить TASK-005
- TASK-020 (Warning Collector) зависит от TASK-004(done) — будет доступна
- TASK-027, TASK-028, TASK-029, TASK-030 зависят от TASK-004(done) + TASK-006

### TASK-005 — Доменные модели: команды и события
**Статус:** done
**Дата:** 2026-03-15

**План реализации (согласован):**
1. Создать `internal/domain/model/command.go`:
   - ProcessDocumentCommand (job_id, document_id, file_url + опциональные: organization_id, user_id, file_name, file_size, mime_type, checksum)
   - CompareVersionsCommand (job_id, document_id, base_version_id, target_version_id + опциональные: organization_id, user_id)
2. Создать `internal/domain/model/event.go`:
   - EventMeta embedded struct (correlation_id, timestamp) — по аналогии с JobMeta
   - 9 событий из acceptance criteria: DocumentProcessingArtifactsReady, DocumentProcessingArtifactsPersisted, DocumentProcessingArtifactsPersistFailed, GetSemanticTreeRequest, SemanticTreeProvided, DocumentVersionDiffReady, StatusChangedEvent, ProcessingCompletedEvent, ComparisonCompletedEvent
   - 4 дополнительных события (согласованы с заказчиком):
     - ProcessingFailedEvent, ComparisonFailedEvent — error_code, error_message, failed_at_stage, is_retryable (для UX: кнопка "Повторить" и мониторинг причин отказов)
     - DocumentVersionDiffPersisted, DocumentVersionDiffPersistFailed — подтверждение сохранения diff-результата от DM (аналог ArtifactsPersisted для comparison pipeline)
   - is_retryable во всех 4 error-событиях: PersistFailed (управляет retry-логикой DP), FailedEvent (UX: показать/скрыть "Повторить")
3. Создать `internal/domain/model/command_test.go`:
   - JSON round-trip для обеих команд
   - omitempty-проверки для опциональных полей
4. Создать `internal/domain/model/event_test.go`:
   - JSON round-trip для всех 13 событий
   - omitempty-проверки (warnings в ArtifactsReady, stage в StatusChangedEvent)
   - Проверка embedding EventMeta (correlation_id/timestamp на верхнем уровне JSON)

**Ключевые решения:**
- EventMeta embedded struct — аналог JobMeta для событий
- StatusChangedEvent.Stage — тип string (не ProcessingStage/ComparisonStage), т.к. событие может относиться к обоим pipeline, а для DTO type safety не критична
- ProcessingFailedEvent/ComparisonFailedEvent добавлены сверх acceptance criteria — архитектура упоминает "событие ошибки", но явно не определяет его; без этих событий потребители не знают причину FAILED
- DocumentVersionDiffPersisted/PersistFailed добавлены — comparison pipeline тоже ожидает подтверждение от DM (шаг 12 архитектуры), а события для этого не были определены
- is_retryable в error-событиях: PersistFailed → управляет retry в DP; FailedEvent → позволяет API показать "Повторить"
- omitempty для опциональных полей команд и событий (warnings, stage, org_id, user_id и т.д.)

**Summary:**
- Созданы 2 файла моделей: command.go (2 типа команд), event.go (EventMeta + 13 событий)
- Созданы 2 файла тестов: command_test.go (4 теста), event_test.go (18 тестов)
- 22 новых теста, все проходят
- go build, go vet — без ошибок
- Общее количество тестов в пакете model: 121

**Заметки для следующей итерации:**
- TASK-006 (порты) — ВСЕ зависимости выполнены (TASK-002✅, TASK-003✅, TASK-004✅, TASK-005✅). Это самая приоритетная следующая задача, т.к. блокирует большинство engine- и application-задач
- TASK-007 (конфигурация) и TASK-018 (state machine) — также доступны (critical, deps met)
- Дополнительные события (4 шт.) нужно учесть при реализации TASK-006 (port interfaces) и TASK-032/034 (event publisher, DM adapters)

### TASK-006 — Определение всех port-интерфейсов (inbound и outbound)
**Статус:** done
**Дата:** 2026-03-15

**План реализации (согласован):**
1. Создать `internal/domain/port/errors.go`:
   - `DomainError` struct (Code, Message, Retryable, Cause) с методами Error(), Unwrap()
   - 13 констант кодов ошибок (ErrCodeValidation, ErrCodeFileTooLarge, ErrCodeTooManyPages, ErrCodeInvalidFormat, ErrCodeFileNotFound, ErrCodeOCRFailed, ErrCodeExtractionFailed, ErrCodeStorageFailed, ErrCodeBrokerFailed, ErrCodeTimeout, ErrCodeServiceUnavailable, ErrCodeDuplicateJob, ErrCodeConcurrencyLimit)
   - 13 конструкторов (по одному на код), Retryable зашит в конструктор (кроме OCR — параметр)
   - 3 хелпера: IsDomainError(), IsRetryable(), ErrorCode() — принимают `error`, извлекают через errors.As
2. Создать `internal/domain/port/inbound.go`:
   - ProcessingCommandHandler — HandleProcessDocument(ctx, ProcessDocumentCommand) error
   - ComparisonCommandHandler — HandleCompareVersions(ctx, CompareVersionsCommand) error
   - DMResponseHandler — 5 методов (ArtifactsPersisted, ArtifactsPersistFailed, SemanticTreeProvided, DiffPersisted, DiffPersistFailed)
3. Создать `internal/domain/port/outbound.go`:
   - TempStoragePort (Upload, Download, Delete, DeleteByPrefix)
   - SourceFileDownloaderPort (Download → io.ReadCloser + int64)
   - OCRServicePort (Recognize → string)
   - EventPublisherPort (5 методов: StatusChanged, ProcessingCompleted/Failed, ComparisonCompleted/Failed)
   - DMArtifactSenderPort (SendArtifacts, SendDiffResult)
   - DMTreeRequesterPort (RequestSemanticTree)
   - IdempotencyStorePort (Check → IdempotencyStatus, Register, MarkCompleted) + IdempotencyStatus enum
   - ConcurrencyLimiterPort (Acquire, Release)
4. Создать `internal/domain/port/engine.go`:
   - InputValidatorPort (Validate)
   - SourceFileFetcherPort (Fetch → *FetchResult) + FetchResult struct (StorageKey, PageCount, IsTextPDF, FileSize)
   - TextExtractionPort (Extract с опциональным *OCRRawArtifact → *ExtractedText + warnings)
   - StructureExtractionPort (Extract → *DocumentStructure + warnings)
   - SemanticTreeBuilderPort (Build → *SemanticTree)
   - VersionComparisonPort (Compare → *VersionDiffResult)
5. Создать `internal/domain/port/errors_test.go`:
   - Table-driven тесты всех 14 конструкторов (Code, Retryable, Cause)
   - Тесты Error() с и без Cause
   - Тесты Unwrap() с и без Cause
   - Тесты errors.As прямой и через wrapping
   - Тесты IsRetryable (17 кейсов включая wrapped)
   - Тесты ErrorCode (15 кейсов включая wrapped и не-DomainError)
   - Тесты IsDomainError (4 кейса)

**Ключевые решения:**
- Два command handler вместо одного — разные оркестраторы реализуют (ISP, ADR-4)
- Один DMResponseHandler — DM Inbound Adapter единственный вызывающий, ему нужны все 5 методов
- OCRServicePort как outbound (инфраструктурный вызов Yandex Vision), не engine — оркестратор сам решает вызывать ли OCR по FetchResult.IsTextPDF
- 13 конструкторов вместо 3 generic — по одному на код, нельзя перепутать (решение по замечанию заказчика)
- Хелперы ErrorCode/IsRetryable/IsDomainError принимают `error` (не *DomainError) — вызывающий код получает `error` от интерфейса, не знает конкретный тип
- Engine-порты — оркестратор зависит от абстракций, не от реализаций (DIP), позволяет тестировать с mock-ами
- FetchResult и IdempotencyStatus определены в port-пакете — часть контракта порта
- Release() без error — освобождение семафора не может провалиться

**Summary:**
- Созданы 5 файлов: errors.go, inbound.go, outbound.go, engine.go, errors_test.go
- 3 inbound-порта (17 интерфейсных методов), 8 outbound-портов, 6 engine-портов
- DomainError: 13 кодов, 13 конструкторов, 3 хелпера
- 55 unit-тестов, все проходят
- go build, go vet — без ошибок
- Общее количество тестов в проекте: 176 (model: 121 + port: 55)

**Заметки для следующей итерации:**
- Разблокированные задачи (critical, deps met): TASK-007 (config), TASK-018 (state machine)
- Разблокированные задачи (high, deps met): TASK-020 (warning collector — зависит от TASK-004✅), TASK-021 (pending response registry — TASK-005✅ + TASK-006✅), TASK-022 (input validator — TASK-002✅ + TASK-005✅ + TASK-006✅)
- Многие engine/egress задачи теперь зависят только от TASK-006✅ + инфраструктурных задач (TASK-008..014)
- При реализации TASK-032 (Event Publisher) учесть 5 методов EventPublisherPort
- При реализации TASK-034 (DM Inbound Adapter) учесть DMResponseHandler с 5 методами

### TASK-007 — Модуль конфигурации
**Статус:** done
**Дата:** 2026-03-15

**План реализации (согласован):**
1. Создать `internal/config/config.go`:
   - Корневая структура `Config` с 9 вложенными конфигурациями (именованные поля, без embedding)
   - `Load() (*Config, error)` — чтение env, применение дефолтов, валидация
   - `Validate() error` — агрегированная ошибка со списком всех отсутствующих обязательных полей
   - Приватные хелперы: `envString`, `envInt`, `envInt64`, `envDuration`
2. Создать `internal/config/sub_configs.go`:
   - 9 структур: BrokerConfig (16 полей: Address + 15 топиков), StorageConfig (5 полей), OCRConfig (4 поля), LimitsConfig (3 поля), ConcurrencyConfig (1 поле), IdempotencyConfig (1 поле), ObservabilityConfig (3 поля), HTTPConfig (1 поле), RetryConfig (2 поля)
   - 9 функций `loadXxxConfig()` — по одной на структуру
3. Создать `internal/config/config_test.go`:
   - Хелпер `setRequiredEnv(t)` для установки 8 обязательных переменных
   - 11 тестов: валидация (all present, single missing, multiple missing, partial, full, empty), дефолты, топики, override значений, override топиков, невалидные значения (fallback to default)
4. Обновить `architecture/high-architecture.md`:
   - Новый раздел 3.3.1 — реестр топиков с иерархическим именованием
   - Обновление сценария инициализации: имена топиков вместо имён событий
5. Создать `architecture/configuration.md`:
   - Полная инструкция по конфигурации: обязательные/необязательные переменные, топики, пример .env

**Ключевые решения (согласованы с заказчиком):**
- Иерархическое именование топиков `{домен}.{тип}.{действие}` — стандарт для брокеров
- Префикс `dp.*` для всего, что DP публикует или слушает как команды; `dm.responses.*` для ответов от DM
- HTTPConfig добавлен сейчас (порт для health/readiness probes)
- RetryConfig добавлен сейчас (`MaxAttempts=3`, `BackoffBase=1s`)
- Только stdlib (`os.Getenv`) — без внешних зависимостей
- Невалидные значения int/duration тихо откатываются на дефолт (валидация ловит обязательные поля отдельно)

**Summary:**
- Созданы 3 файла кода: config.go, sub_configs.go, config_test.go
- Созданы 2 файла документации: architecture/configuration.md, обновлён architecture/high-architecture.md
- 9 вложенных конфигураций, 8 обязательных полей, 15 топиков с дефолтами
- 11 тестов (55 подтестов), все проходят
- go test, go vet, make build — без ошибок
- Общее количество тестов в проекте: 187 (model: 121 + port: 55 + config: 11)

**Заметки для следующей итерации:**
- TASK-007✅ разблокирует 5 инфраструктурных задач: TASK-008 (broker), TASK-009 (KV-store), TASK-010 (object storage), TASK-011 (OCR client), TASK-012 (observability)
- Разблокированные задачи (critical, deps met): TASK-018 (state machine), TASK-022 (input validator), TASK-028 (structure extraction), TASK-029 (semantic tree builder)
- Разблокированные задачи (high, deps met): TASK-008..012 (инфраструктура), TASK-014 (PDF), TASK-020 (warning collector), TASK-021 (pending response registry)
- При реализации инфраструктурных клиентов (TASK-008..012) — принимать соответствующую sub-config по значению (BrokerConfig, StorageConfig и т.д.)
- Топики определены в BrokerConfig — при реализации TASK-015 (consumer), TASK-032 (publisher), TASK-033/034 (DM adapters) брать имена топиков из конфига

### TASK-018 — Job Status State Machine
**Статус:** done
**Дата:** 2026-03-15

**План реализации (согласован):**
- Вариант Б (согласован с заказчиком): вместо отдельного StateMachine struct в application/lifecycle, добавить метод `TransitionTo(newStatus)` на `JobMeta` (embedded в ProcessingJob и ComparisonJob)
- Причина: отдельный StateMachine избыточен — ValidateTransition() и IsTerminal() уже реализованы в domain model (TASK-002), обёртка не добавляет ценности
- TransitionTo атомарно валидирует переход + обновляет Status и UpdatedAt

**Изменённые файлы:**
1. `internal/domain/model/job.go` — добавлен метод `(*JobMeta).TransitionTo(newStatus JobStatus) error`
2. `internal/domain/model/job_test.go` — добавлены 3 теста (17 подтестов): ValidTransitions (7), InvalidTransitions (7), ComparisonJob (3)

**Ключевые решения:**
- TransitionTo на JobMeta, а не на ProcessingJob/ComparisonJob — оба типа получают метод через embedding, без дублирования
- Ошибки — plain error (не DomainError): model не может импортировать port (циклическая зависимость). TASK-019 (Lifecycle Manager) обернёт в DomainError при необходимости
- Пакет application/lifecycle не создан — TASK-019 будет использовать job.TransitionTo() напрямую

**Summary:**
- 1 метод добавлен: TransitionTo на JobMeta
- 17 новых подтестов, все проходят
- go test, go vet — без ошибок
- Общее количество тестов в проекте: 190 (model: 124 + port: 55 + config: 11)

**Заметки для следующей итерации:**
- TASK-019 (Lifecycle Manager) разблокирован — использует job.TransitionTo() вместо отдельного StateMachine
- Критические задачи с выполненными зависимостями: TASK-019 (deps: TASK-006✅, TASK-018✅), TASK-022, TASK-028, TASK-029
- Высокоприоритетные: TASK-008..012 (инфраструктура), TASK-014 (PDF), TASK-020 (warning collector), TASK-021 (pending response registry)

### TASK-019 — Job Lifecycle Manager
**Статус:** done
**Дата:** 2026-03-15

**План реализации (согласован):**
1. Определить интерфейс `ManagedJob` в `internal/application/lifecycle/manager.go`:
   - `GetJobMeta() *JobMeta`, `GetDocumentID() string`, `GetStage() string`
   - Позволяет LifecycleManager работать с ProcessingJob и ComparisonJob без знания конкретного типа
2. Добавить методы `GetJobMeta()`, `GetDocumentID()`, `GetStage()` на `ProcessingJob` и `ComparisonJob` в `internal/domain/model/job.go`
3. Определить `CleanupFunc func(ctx context.Context, jobID string) error` — передаётся в конструктор один раз
4. Создать `LifecycleManager` struct (publisher, idempotency, jobTimeout, cleanup)
5. Реализовать `TransitionJob(ctx, job ManagedJob, newStatus JobStatus) error`:
   - Сохраняет oldStatus → вызывает job.GetJobMeta().TransitionTo(newStatus) → публикует StatusChangedEvent
   - На terminal: cleanup (best-effort, ошибка логируется) → idempotency.MarkCompleted
6. Реализовать `NewJobContext(parent) (ctx, cancel)` — context.WithTimeout(jobTimeout)
7. Написать 13 unit-тестов с mock EventPublisherPort и IdempotencyStorePort

**Ключевые решения (согласованы с заказчиком):**
- ManagedJob интерфейс вместо передачи documentID/stage в каждый вызов TransitionJob — данные берутся из job
- CleanupFunc в конструкторе, не в TransitionJob — cleanup один для всех задач, принимает jobID как параметр
- cleanup best-effort: ошибка логируется, но не откатывает transition — артефакты могут быть удалены позже через TTL/GC
- EventPublish failure возвращает error: transition уже произошёл на объекте, но вызывающий код знает об ошибке
- idempotency.MarkCompleted ошибка возвращает error: важная операция, без mark возможна повторная обработка
- CorrelationID = JobID в StatusChangedEvent — простая корреляция, достаточная для v1

**Изменённые/созданные файлы:**
1. `internal/domain/model/job.go` — добавлены 6 методов (GetJobMeta, GetDocumentID, GetStage × 2 типа)
2. `internal/application/lifecycle/manager.go` — LifecycleManager struct + ManagedJob интерфейс + CleanupFunc
3. `internal/application/lifecycle/manager_test.go` — 13 тестов с mock-ами

**Summary:**
- LifecycleManager: TransitionJob + NewJobContext
- ManagedJob: интерфейс для обобщённой работы с ProcessingJob и ComparisonJob
- CleanupFunc: передаётся в конструктор, вызывается best-effort на terminal переходах
- 13 тестов: все 5 terminal-переходов, non-terminal, invalid transition, cleanup error, nil cleanup, publish error, idempotency error, timeout context, event fields, ComparisonJob
- go test, go vet — без ошибок
- Общее количество тестов в проекте: 203 (model: 124 + port: 55 + config: 11 + lifecycle: 13)

**Заметки для следующей итерации:**
- TASK-019✅ разблокирует: TASK-035 (processing orchestrator), TASK-037 (comparison orchestrator)
- Критические задачи с выполненными зависимостями: TASK-022 (input validator: TASK-002✅, TASK-005✅, TASK-006✅), TASK-028 (structure extraction: TASK-003✅, TASK-004✅, TASK-006✅), TASK-029 (semantic tree builder: TASK-003✅, TASK-004✅, TASK-006✅)
- Высокоприоритетные с выполненными зависимостями: TASK-008..012 (инфраструктура), TASK-014 (PDF), TASK-020 (warning collector: TASK-004✅), TASK-021 (pending response registry: TASK-005✅, TASK-006✅)
- При реализации TASK-035/037 — оркестраторы создают LifecycleManager с конкретным CleanupFunc (TempStoragePort.DeleteByPrefix + ConcurrencyLimiterPort.Release)

### TASK-022 — Input Validator
**Статус:** done
**Дата:** 2026-03-15

**План реализации (согласован):**
1. Создать `internal/engine/validator/validator.go`:
   - Struct `Validator` с полями `maxFileSize int64` и `allowedMimeType string`
   - Конструктор `NewValidator(maxFileSize, allowedMimeType)` — engine-слой не зависит от config
   - Метод `Validate(ctx, cmd)` — 4 правила по порядку: document_id, file_url, file_size, mime_type
   - Compile-time проверка `var _ port.InputValidatorPort = (*Validator)(nil)`
2. Создать `internal/engine/validator/validator_test.go`:
   - 10 table-driven тестов: valid, empty document_id, empty file_url, size exceeds, size at limit, size zero, invalid mime, empty mime, correct mime, priority check

**Ключевые решения:**
- allowedMimeType как поле struct (не хардкод) — конфигурируемость
- Конструктор NewValidator (не New) — явное именование конструктора
- file_size и mime_type валидируются только если заявлены (>0 / non-empty) — опциональные поля команды
- Возврат первой найденной ошибки, не агрегация — для REJECTED одной причины достаточно
- Engine-слой не зависит от config-пакета — принимает примитивы через конструктор

**Summary:**
- Созданы 2 файла: validator.go, validator_test.go
- Реализует InputValidatorPort из domain/port/engine.go
- 10 тестов, все проходят
- go test, go vet — без ошибок
- Общее количество тестов в проекте: 213 (model: 124 + port: 55 + config: 11 + lifecycle: 13 + validator: 10)

**Заметки для следующей итерации:**
- TASK-022✅ разблокирует: TASK-043 (security validation: TASK-022✅, TASK-023)
- Критические задачи с выполненными зависимостями: TASK-028 (structure extraction: TASK-003✅, TASK-004✅, TASK-006✅), TASK-029 (semantic tree builder: TASK-003✅, TASK-004✅, TASK-006✅)
- Высокоприоритетные с выполненными зависимостями: TASK-008..012 (инфраструктура), TASK-014 (PDF), TASK-020 (warning collector: TASK-004✅), TASK-021 (pending response registry: TASK-005✅, TASK-006✅)
- При DI в main.go: `validator.NewValidator(cfg.Limits.MaxFileSize, "application/pdf")`

### TASK-020 — Warning Collector
**Статус:** done
**Дата:** 2026-03-16

**План реализации (согласован):**
1. Создать `internal/application/warning/collector.go`:
   - Struct `Collector` с `sync.Mutex` + `[]model.ProcessingWarning`
   - `NewCollector()` — конструктор
   - `Add(w)` — потокобезопасный append
   - `Collect()` — возврат копии слайса (nil при пустом)
   - `Reset()` — очистка
   - `HasWarnings()` — проверка наличия
2. Создать `internal/application/warning/collector_test.go`:
   - Тесты Add/Collect, HasWarnings, Reset, copy safety, concurrent access
   - Все тесты с `-race`

**Ключевые решения:**
- `sync.Mutex` (не RWMutex) — простота, методы не на горячем пути
- `Collect()` возвращает копию — защита от мутации снаружи
- `Collect()` на пустом коллекторе → `nil` (не пустой слайс)
- Нет зависимости от port-пакета — чистая утилита application-слоя

**Summary:**
- Созданы 2 файла: collector.go, collector_test.go
- 7 тестов (2 подтеста в AddAndCollect): all pass with -race
- go test, go vet — без ошибок
- Общее количество тестов в проекте: 220 (model: 124 + port: 55 + config: 11 + lifecycle: 13 + validator: 10 + warning: 7)

**Заметки для следующей итерации:**
- TASK-020✅ разблокирует: TASK-035 (processing orchestrator — один из 11 deps)
- Критические задачи с выполненными зависимостями: TASK-028 (structure extraction: TASK-003✅, TASK-004✅, TASK-006✅), TASK-029 (semantic tree builder: TASK-003✅, TASK-004✅, TASK-006✅)
- Высокоприоритетные с выполненными зависимостями: TASK-008..012 (инфраструктура), TASK-014 (PDF), TASK-021 (pending response registry: TASK-005✅, TASK-006✅)
- Warning Collector используется оркестратором: собирает warnings от engine-шагов, HasWarnings() определяет COMPLETED vs COMPLETED_WITH_WARNINGS

### TASK-028 — Structure Extraction Engine
**Статус:** done
**Дата:** 2026-03-16

**План реализации (согласован):**
1. Создать `internal/engine/structure/extractor.go`:
   - Struct `Extractor` без параметров (regex-based, stateless)
   - Конструктор `NewExtractor()`
   - Метод `Extract(ctx, text *ExtractedText) → (*DocumentStructure, []ProcessingWarning, error)`
   - Compile-time проверка `var _ StructureExtractionPort = (*Extractor)(nil)`
2. Логика парсинга (в порядке обработки):
   - Склейка текста через `FullText()`
   - Выделение блока реквизитов сторон (FR-1.3.2) — маркер "реквизиты сторон" case-insensitive, парсинг ИНН/ОГРН/адрес/представитель
   - Выделение приложений — паттерн `Приложение N`
   - Выделение разделов/пунктов/подпунктов — иерархический парсинг `N.` → `N.N.` → `N.N.N.`
3. Создать `internal/engine/structure/extractor_test.go`:
   - 14 тестов на образцах русских договоров

**Ключевые решения:**
- Regex-based подход: детерминированный, быстрый, достаточный для v1
- Поддержка role-label строк (Заказчик:/Исполнитель:) перед именем компании
- isKnownHeader() для 20+ типовых заголовков русских договоров (Предмет договора, Ответственность сторон и т.д.)
- startsWithUppercaseRussian() для распознавания заголовков разделов
- Warnings вместо errors для отсутствия структуры/реквизитов
- Partial party details warning: если есть имя, но нет ИНН и адреса

**Summary:**
- Созданы 2 файла: extractor.go (460 строк), extractor_test.go (670 строк)
- Реализует StructureExtractionPort из domain/port/engine.go
- 14 тестов (16 с подтестами), все проходят
- go test, go vet, -race — без ошибок
- Общее количество тестов в проекте: 234 (model: 124 + port: 55 + config: 11 + lifecycle: 13 + validator: 10 + warning: 7 + structure: 14)

**Заметки для следующей итерации:**
- TASK-028✅ разблокирует: TASK-035 (processing orchestrator — один из 11 deps)
- Критические задачи с выполненными зависимостями: TASK-029 (semantic tree builder: TASK-003✅, TASK-004✅, TASK-006✅)
- Высокоприоритетные с выполненными зависимостями: TASK-008..012 (инфраструктура), TASK-014 (PDF), TASK-021 (pending response registry), TASK-030 (version comparison: TASK-004✅, TASK-006✅)
- При DI в main.go: `structure.NewExtractor()` — без параметров
- Extractor использует FullText() для склейки страниц — зависит от корректной работы TextExtractionPort (TASK-027)

### TASK-029 — Semantic Tree Builder
**Статус:** done
**Дата:** 2026-03-17

**План реализации (согласован):**
1. Создать `internal/engine/semantictree/builder.go`:
   - Struct `Builder` без полей (stateless)
   - Конструктор `NewBuilder()`
   - Метод `Build(ctx, text, structure)` — построение дерева из DocumentStructure
   - Compile-time проверка `var _ SemanticTreeBuilderPort = (*Builder)(nil)`
2. Логика построения дерева:
   - Корневой узел ROOT
   - Section → SectionNode (metadata: number, title), Content → TextNode ребёнок
   - Clause → ClauseNode (metadata: number, content), SubClause → ClauseNode ребёнок
   - Appendix → AppendixNode (metadata: number, title), Content → TextNode ребёнок
   - PartyDetails → PartyDetailsNode (content: name, metadata: только непустые поля)
3. ID-схема: root, section-N, clause-N.N, subclause-N.N.N, appendix-N, party-N, text-N (глобальный счётчик)
4. Создать `internal/engine/semantictree/builder_test.go`:
   - 17 тестов покрывающих все аспекты: full contract, edge cases, JSON round-trip, Walk traversal, context cancellation, unique IDs

**Ключевые решения (согласованы с заказчиком):**
- NOTE(v1): параметр `ExtractedText` принимается по контракту `SemanticTreeBuilderPort`, но НЕ используется в текущей реализации. Дерево строится целиком из `DocumentStructure`, которая уже содержит весь текстовый контент. В будущих версиях `ExtractedText` может использоваться для захвата текста-преамбулы, не покрытого structure extractor-ом. Решение помечено комментарием в коде.
- SubClause использует тип NodeTypeClause (не отдельный тип) — соответствует модели из TASK-004
- PartyDetailsNode.Content = Name (для отображения), остальные поля в Metadata только если непустые
- DocumentID берётся из DocumentStructure (не из ExtractedText)

**Summary:**
- Созданы 2 файла: builder.go, builder_test.go (файл .gitkeep заменён)
- Реализует SemanticTreeBuilderPort из domain/port/engine.go
- 17 тестов, все проходят с -race
- go build, go vet — без ошибок
- Общее количество тестов в проекте: 251 (model: 124 + port: 55 + config: 11 + lifecycle: 13 + validator: 10 + warning: 7 + structure: 14 + semantictree: 17)

**Заметки для следующей итерации:**
- TASK-029✅ разблокирует: TASK-035 (processing orchestrator — один из 11 deps)
- Критические задачи с выполненными зависимостями: нет новых critical (все critical engine-задачи done)
- Высокоприоритетные с выполненными зависимостями: TASK-008..012 (инфраструктура), TASK-014 (PDF), TASK-021 (pending response registry), TASK-030 (version comparison: TASK-004✅, TASK-006✅)
- При DI в main.go: `semantictree.NewBuilder()` — без параметров

### TASK-014 — PDF-утилита
**Статус:** done
**Дата:** 2026-03-17

**План реализации (согласован):**
1. Создать `internal/pdf/pdf.go`:
   - Struct `Util` (stateless, без полей)
   - Конструктор `NewUtil()`
   - `IsValidPDF(io.Reader) bool` — проверка magic bytes %PDF (первые 4 байта)
   - `Analyze(io.ReadSeeker) (*Info, error)` — подсчёт страниц + определение text/scan
   - `ExtractText(io.ReadSeeker) ([]PageText, error)` — постраничное извлечение текста
   - Sentinel errors: `ErrInvalidPDF`, `ErrEmptyReader`
2. Библиотека `pdfcpu` (pure Go, Apache 2.0):
   - `api.ReadAndValidate` для парсинга PDF
   - `pdfcpu.ExtractPageContent` для извлечения content stream
   - Парсинг BT/ET блоков и операторов Tj/TJ/'/\" для извлечения текста
3. Создать `internal/pdf/pdf_test.go`:
   - 3 генератора тестовых PDF: `generateTextPDF`, `generateEmptyPagePDF`, `generateMixedPDF`
   - PDF генерируются программно из raw bytes (не коммитим бинарные файлы)
   - 22 теста с -race

**Ключевые решения (согласованы с заказчиком):**
- IsTextPDF = true ТОЛЬКО если ВСЕ страницы содержат текст (не ratio-based)
- Util полностью stateless (без minTextRatio поля) — раз критерий "все страницы", конфигурация не нужна
- Свои типы PageText/Info — пакет не импортирует domain/model (engine-слой конвертирует)
- Возвращает plain error — engine-слой оборачивает в DomainError
- IsValidPDF отдельно от Analyze — дешёвая проверка 4 байт до тяжёлого парсинга
- Analyze объединяет CountPages + IsTextPDF — чтобы не парсить PDF дважды
- io.ReadSeeker для Analyze/ExtractText — pdfcpu нужен random access

**Summary:**
- Созданы 2 файла: pdf.go (295 строк), pdf_test.go (645 строк)
- Добавлена зависимость github.com/pdfcpu/pdfcpu v0.11.1
- go mod tidy: прямые зависимости вынесены в отдельный require-блок
- 25 тестов, все проходят с -race: IsValidPDF (5 table-driven + nil), Analyze (single/multi text, scan, mixed, corrupted, nil), ExtractText (single/multi, scan, corrupted, ordering, nil), internal helpers (decodePDFString 5, extractBTETBlocks 5, parseTextFromContentStream 5), constructor
- real_pdf_test.go (build tag `manual`) — тесты на реальных PDF из internal/pdf/data/ (first.pdf, second.pdf)
- go build, go vet — без ошибок
- Общее количество тестов в проекте: 276 (model: 124 + port: 55 + config: 11 + lifecycle: 13 + validator: 10 + warning: 7 + structure: 14 + semantictree: 17 + pdf: 25)

**Доработки после первичной реализации:**
- Багфикс: `extractBTETBlocks` — "ET" внутри слов (PREDMET, RASCHETOV) ошибочно закрывала BT-блок. Исправлено: `findStandaloneOperator()` проверяет, что BT/ET — самостоятельные операторы (не часть слова). Добавлены 3 regression-теста.
- Тестирование на реальных PDF: first.pdf (текстовый, 1 стр, 39 KB) корректно определяется как IsTextPDF=true, second.pdf (скан, 2 стр, 70 KB) — как IsTextPDF=false.
- Обнаружено ограничение v1: кириллица в embedded-шрифтах с CMap/ToUnicode отображается как raw glyph-индексы. Создана TASK-045 на доработку.

**Заметки для следующей итерации:**
- TASK-014✅ разблокирует: TASK-024 (fetcher validation: TASK-014✅, TASK-023), TASK-025 (OCR adapter: TASK-006✅, TASK-014✅), TASK-027 (text extraction: TASK-004✅, TASK-006✅, TASK-014✅)
- TASK-045 (low priority) — декодирование CMap/ToUnicode для кириллицы в embedded-шрифтах, зависит от TASK-014✅
- Высокоприоритетные с выполненными зависимостями: TASK-008..012 (инфраструктура), TASK-021 (pending response registry), TASK-025 (OCR adapter), TASK-027 (text extraction), TASK-030 (version comparison)
- При DI: `pdf.NewUtil()` — без параметров, stateless, thread-safe
- Потребители: engine/fetcher (IsValidPDF + Analyze), engine/textextract (ExtractText), engine/ocr (использует FetchResult.IsTextPDF)

### TASK-025 — OCR Integration Adapter: определение необходимости OCR и маршрутизация
**Статус:** done
**Дата:** 2026-03-18

**План реализации (согласован):**
1. Создать `internal/engine/ocr/adapter.go`:
   - Struct `Adapter` с полями `ocrService port.OCRServicePort` и `storage port.TempStoragePort`
   - Конструктор `NewAdapter(ocrService, storage)`
   - Метод `Process(ctx, storageKey, isTextPDF)`:
     - `isTextPDF=true` → `OCRRawArtifact{Status: not_applicable}`, OCR не вызывается
     - `isTextPDF=false` → `storage.Download` → `ocrService.Recognize` → `OCRRawArtifact{Status: applicable, RawText: text}`
   - Обработка ошибок: StorageError (retryable), OCRError (retryability из underlying error)
   - Defer `reader.Close()`, проверка `ctx.Err()` между Download и Recognize
2. Создать `internal/engine/ocr/adapter_test.go`:
   - Mock-и для OCRServicePort и TempStoragePort
   - 7 тестов: text-PDF skip, scan-PDF success, storage error, OCR error, OCR retryable error, context cancelled, reader closed

**Ключевые решения:**
- Adapter не реализует port-интерфейс — нет отдельного OCR adapter порта, вызывается напрямую оркестратором
- `isTextPDF` приходит из `FetchResult.IsTextPDF` (определяется pdf-утилитой в TASK-014)
- Retryability из underlying error сохраняется через `port.IsRetryable(err)` → передаётся в `port.NewOCRError`
- StorageError создаётся с cause для unwrap-chain
- Проверка `ctx.Err()` между Download и Recognize — early exit при отмене контекста
- TASK-026 добавит rate limiter, warnings, retry — здесь базовая маршрутизация

**Summary:**
- Созданы 2 файла: adapter.go (60 строк), adapter_test.go (262 строки)
- 7 тестов, все проходят с -race
- go test, go vet — без ошибок
- Общее количество тестов в проекте: 283 (model: 124 + port: 55 + config: 11 + lifecycle: 13 + validator: 10 + warning: 7 + structure: 14 + semantictree: 17 + pdf: 25 + ocr: 7)

**Заметки для следующей итерации:**
- TASK-025✅ разблокирует: TASK-026 (OCR с rate limiter: TASK-011, TASK-025✅)
- Высокоприоритетные с выполненными зависимостями: TASK-008..012 (инфраструктура), TASK-021 (pending response registry: TASK-005✅, TASK-006✅), TASK-027 (text extraction: TASK-004✅, TASK-006✅, TASK-014✅), TASK-030 (version comparison: TASK-004✅, TASK-006✅)
- При DI в main.go: `ocr.NewAdapter(ocrClient, tempStorage)` — принимает оба инфраструктурных порта
- Оркестратор вызывает: `adapter.Process(ctx, fetchResult.StorageKey, fetchResult.IsTextPDF)`

### TASK-027 — Text Extraction & Normalization Engine
**Статус:** done
**Дата:** 2026-03-21

**План реализации (согласован):**
1. Создать `internal/engine/textextract/extractor.go`:
   - Интерфейс `PDFTextExtractor` (удовлетворяется `*pdf.Util`) для DI и тестирования
   - Struct `Extractor` с `pdfExtractor PDFTextExtractor` и `storage port.TempStoragePort`
   - Конструктор `NewExtractor(pdfExtractor, storage)`
   - Compile-time проверка `var _ port.TextExtractionPort = (*Extractor)(nil)`
2. Метод `Extract(ctx, storageKey, ocrResult)`:
   - PDF-путь (ocrResult nil/not_applicable): `storage.Download` → `io.ReadAll` → `pdfExtractor.ExtractText`
   - OCR-путь (ocrResult.Status == applicable): split `RawText` по `\f` на страницы
   - Нормализация: NFC → cleanText (garbage removal) → TrimSpace
   - Warnings: `TEXT_EXTRACTION_EMPTY_PAGE` per page, `TEXT_EXTRACTION_ALL_PAGES_EMPTY` aggregate
3. Создать `internal/engine/textextract/extractor_test.go`:
   - 18 тестов: оба пути, ошибки, нормализация, warnings, reader cleanup, context cancellation

**Ключевые решения:**
- `PDFTextExtractor` интерфейс — consumer-side interface для `*pdf.Util`, позволяет мокать без генерации реальных PDF в тестах
- `io.ReadAll` для конвертации `ReadCloser → ReadSeeker` — безопасно при лимите 20 МБ
- OCR текст делится по `\f` (form-feed) — OCR-сервисы часто вставляют его между страницами; если `\f` нет — одна страница
- Пустые страницы — warnings, не errors (паттерн structure extractor)
- `storageKey` как `DocumentID` — единственный идентификатор на этом этапе pipeline
- `strconv.Itoa` для номера страницы в warning message (стандартная библиотека)
- Garbage filter: C0/C1 control chars (кроме \t\n\r), DEL, zero-width (U+200B..U+200F), BOM (U+FEFF), replacement (U+FFFD), directional markers (U+202A..U+202E, U+2066..U+2069), word joiner (U+2060)
- Зависимость `golang.org/x/text` промотирована из indirect в direct (уже была в go.sum через pdfcpu)

**Summary:**
- Созданы 2 файла: extractor.go (195 строк), extractor_test.go (370 строк)
- Реализует TextExtractionPort из domain/port/engine.go
- 18 тестов, все проходят с -race
- go test ./... — все 301 тестов проекта проходят
- go mod tidy — зависимости обновлены

**Заметки для следующей итерации:**
- TASK-027✅ разблокирует: TASK-035 (processing orchestrator — один из 11 deps)
- Eligible задачи (high, deps met): TASK-008..012 (инфраструктура), TASK-021 (pending response registry), TASK-030 (version comparison: TASK-004✅, TASK-006✅)
- При DI в main.go: `textextract.NewExtractor(pdfUtil, tempStorage)` — pdfUtil = `pdf.NewUtil()`
- Оркестратор вызывает: `textExtractor.Extract(ctx, fetchResult.StorageKey, &ocrResult)` — результат передаётся в structExtractor.Extract() и treeBuilder.Build()

### TASK-030 — Version Comparison Engine
**Статус:** done
**Дата:** 2026-03-21

**План реализации (согласован):**
1. Создать `internal/engine/comparison/comparer.go`:
   - Struct `Comparer` без полей (stateless, zero-dependency)
   - Конструктор `NewComparer()`
   - Метод `Compare(ctx, baseTree, targetTree)` → `(*VersionDiffResult, error)`
   - Compile-time проверка `var _ port.VersionComparisonPort = (*Comparer)(nil)`
2. Внутренняя структура `nodeInfo` (node, path, parentID, childIdx)
3. Алгоритм:
   - `buildIndex(tree)` — DFS-обход, построение map[string]*nodeInfo с путями
   - `computeStructuralDiffs(baseIndex, targetIndex)` — 3 прохода: removed, added, moved
   - `computeTextDiffs(baseIndex, targetIndex)` — 3 прохода: removed, added, modified content
   - `nodeLabel(node)` — сегмент пути на русском языке
4. Создать `internal/engine/comparison/comparer_test.go`:
   - 27 тестов: все сценарии, edge cases, paths, deep structures

**Ключевые решения:**
- Сопоставление узлов по ID — семантические ID (section-1, clause-1.1) стабильны между версиями, не нужен fuzzy matching
- "Moved" моделируется как DiffTypeModified с Description "узел перемещён" — три DiffType достаточно для v1
- Пустые слайсы `[]TextDiffEntry{}` вместо nil — JSON сериализуется как `[]`, не `null`
- Сортировка diff-записей по Type+NodeID/Path для детерминированного вывода в тестах
- Без внешних diff-библиотек — дерево маленькое (сотни узлов), O(n) достаточно
- Пути на русском: "Раздел N / Пункт N.N / Приложение N / Реквизиты: Имя / Текст"
- nodeLabel fallbacks: number → title → generic label (для section без метаданных)

**Summary:**
- Созданы 2 файла: comparer.go (273 строки), comparer_test.go
- Удалён .gitkeep из internal/engine/comparison/
- Реализует VersionComparisonPort из domain/port/engine.go
- 27 тестов, все проходят с -race
- go test ./... — все тесты проекта проходят (13 пакетов)
- Общее количество тестов в проекте: 328 (model: 124 + port: 55 + config: 11 + lifecycle: 13 + validator: 10 + warning: 7 + structure: 14 + semantictree: 17 + pdf: 25 + ocr: 7 + textextract: 18 + comparison: 27)

**Заметки для следующей итерации:**
- TASK-030✅ разблокирует: TASK-037 (Comparison Pipeline Orchestrator — deps: TASK-019✅, TASK-021, TASK-030✅, TASK-032, TASK-033, TASK-034)
- Все 6 engine-компонентов завершены: validator✅, fetcher (ожидает TASK-023), ocr✅, textextract✅, structure✅, semantictree✅, comparison✅
- Eligible задачи (high, deps met): TASK-008..012 (инфраструктура), TASK-021 (pending response registry)
- При DI в main.go: `comparison.NewComparer()` — без параметров, stateless
- Comparison Pipeline Orchestrator вызывает: `comparer.Compare(ctx, baseSemanticTree, targetSemanticTree)` — оба дерева получены через DMTreeRequesterPort

### TASK-010 — Инфраструктурный клиент Yandex Object Storage
**Статус:** done
**Дата:** 2026-03-22

**План реализации:**
1. Создать `internal/infra/objectstorage/client.go`:
   - S3API consumer-side interface (5 методов: PutObject, GetObject, DeleteObject, ListObjectsV2, DeleteObjects)
   - Client struct с s3 (S3API) + bucket (string)
   - NewClient(cfg StorageConfig) — aws-sdk-go-v2 S3 client, static credentials, custom endpoint, path-style
   - Upload → PutObject, Download → GetObject, Delete → DeleteObject, DeleteByPrefix → paginated List + batch Delete
2. Создать `internal/infra/objectstorage/errors.go`:
   - mapError(err, operation) — context errors pass through, S3 errors → DomainError с правильным retryable
   - nonRetryableCodes: NoSuchKey, NoSuchBucket, AccessDenied, InvalidBucketName, NotFound
3. Создать `internal/infra/objectstorage/client_test.go`:
   - mockS3 struct с function fields (паттерн из textextract)
   - 28 тестов покрывающих все методы и ошибки

**Ключевые решения:**
- Библиотека: aws-sdk-go-v2 (модулярный, Yandex Object Storage полностью S3-совместим)
- Consumer-side S3API interface — инверсия зависимостей для тестирования (паттерн PDFTextExtractor)
- Retryable vs non-retryable ошибки: NoSuchKey, AccessDenied, NoSuchBucket → non-retryable; InternalError, ServiceUnavailable → retryable; network errors → retryable
- DeleteByPrefix: пагинация (1000 ключей/страница), batch delete с проверкой per-object errors, пустой prefix → ошибка (защита от удаления всего бакета)
- Download: nil Body guard (защита от panic на мisbehaving S3 endpoint)
- Context errors (Canceled, DeadlineExceeded) pass through raw — паттерн из textextract/ocr

**По результатам code-review исправлено:**
- Critical: проверка per-object errors в DeleteObjects response (silent data loss)
- Critical: nil Body guard в Download
- Warning: валидация пустого prefix в DeleteByPrefix
- Warning: дифференциация retryable/non-retryable ошибок (NoSuchKey, AccessDenied)
- Warning: убрано дублирование дефолтного региона (config уже устанавливает)

**Summary:**
- Созданы 3 файла: client.go, errors.go, client_test.go
- Удалён .gitkeep из internal/infra/objectstorage/
- Реализует TempStoragePort из domain/port/outbound.go
- 28 тестов, все проходят с -race
- Новые зависимости: aws-sdk-go-v2, aws-sdk-go-v2/credentials, aws-sdk-go-v2/service/s3, smithy-go
- go test ./... — все тесты проекта проходят (14 пакетов)

**Заметки для следующей итерации:**
- TASK-010✅ разблокирует: TASK-023 (critical — Source File Fetcher), TASK-031 (high — Temp Artifact Storage Adapter)
- Eligible задачи (high, deps met): TASK-008 (broker), TASK-009 (KV-store), TASK-011 (OCR client), TASK-012 (observability), TASK-021 (pending response registry)
- При DI в main.go: `objectstorage.NewClient(cfg.Storage)` — возвращает *Client, реализует TempStoragePort

### TASK-023 — Source File Fetcher — скачивание PDF по URL
**Статус:** done
**Дата:** 2026-03-22

**План реализации:**
1. Создать `internal/engine/fetcher/fetcher.go` (engine layer):
   - Fetcher struct с downloader (SourceFileDownloaderPort), storage (TempStoragePort), maxFileSize (int64)
   - NewFetcher(downloader, storage, maxFileSize) конструктор с DI
   - Fetch(ctx, cmd): ctx check → download → Content-Length early reject → limitedReader → Upload → cleanup on exceeded
   - limitedReader: cap read buffer для точного enforcement (max 1 byte overshoot)
   - classifyDownloadError: errors.Is context passthrough, DomainError passthrough, unknown → SERVICE_UNAVAILABLE
2. Создать `internal/infra/httpdownloader/downloader.go` (infra layer):
   - Downloader struct с http.Client (custom Transport, timeout, max 3 redirects)
   - Download(ctx, fileURL): HTTP GET → status classification → body + ContentLength
   - User-Agent: ContractPro-DP/1.0
3. Создать тесты: fetcher_test.go (17 тестов), downloader_test.go (13 тестов)

**Ключевые решения:**
- Два слоя: engine (Fetcher = orchestration + size enforcement) и infra (HTTPDownloader = transport + HTTP error classification)
- limitedReader с capped buffer (limit - bytesRead + 1) — max 1 byte overshoot вместо целого buffer (32KB)
- Content-Length early reject: если сервер объявил размер > лимита, не читаем body
- Cleanup при exceeded: Delete с context.WithTimeout(5s) для защиты от зависания
- HTTP Transport: MaxIdleConns=10, MaxIdleConnsPerHost=5, IdleConnTimeout=90s, TLSHandshakeTimeout=10s
- Storage key format: {job_id}/source.pdf
- PageCount и IsTextPDF = zero values (scope TASK-024)

**По результатам code-review исправлено:**
- Critical: limitedReader пропускал до buffer-size байт сверх лимита → cap read buffer
- Warning: classifyDownloadError == → errors.Is для wrapped context errors
- Warning: cleanup Delete без timeout → context.WithTimeout(5s)
- Warning: нет User-Agent → "ContractPro-DP/1.0"
- Warning: shared DefaultTransport → dedicated http.Transport с pool settings
- Warning: добавлены тесты boundary (exactly at limit) и Content-Length lie

**Summary:**
- Созданы 4 файла: fetcher.go, fetcher_test.go, downloader.go, downloader_test.go
- 17 тестов fetcher + 13 тестов downloader = 30 новых тестов
- Все 16 пакетов PASS с -race, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-023✅ разблокирует: TASK-024 (critical — Source File Fetcher validation), TASK-043 (high — Security validation)
- Eligible задачи (critical, deps met): TASK-024 (deps: TASK-014✅, TASK-023✅)
- Eligible задачи (high, deps met): TASK-008 (broker), TASK-009 (KV-store), TASK-011 (OCR client), TASK-012 (observability), TASK-021 (pending response registry), TASK-031 (temp artifact storage)
- При DI в main.go: `httpdownloader.NewDownloader(cfg.Limits.JobTimeout)`, `fetcher.NewFetcher(httpDl, storageClient, cfg.Limits.MaxFileSize)`

### TASK-024 — Source File Fetcher: валидация скачанного файла (PDF, страниц ≤ 100)
**Статус:** done
**Дата:** 2026-03-22

**План реализации (согласован с code-architect):**
1. Подход: буферизация в памяти (max 20MB, bounded limitedReader) → валидация → Upload
2. Consumer-side интерфейс PDFAnalyzer в fetcher.go (IsValidPDF + Analyze), реализуется *pdf.Util
3. Новые поля Fetcher: pdfAnalyzer PDFAnalyzer, maxPages int
4. Поток: download → Content-Length early reject → limitedReader → buffer → IsValidPDF → Analyze → pageCount check → Upload
5. На ошибке валидации файл НЕ загружается в storage → cleanup не нужен
6. Ошибки: INVALID_FORMAT (не PDF / corrupted), TOO_MANY_PAGES (> limit) — уже определены в port/errors.go

**Изменённые файлы:**
1. `internal/engine/fetcher/fetcher.go` — PDFAnalyzer interface, обновлён Fetcher struct и NewFetcher, переписан Fetch body (buffer → validate → upload)
2. `internal/engine/fetcher/fetcher_test.go` — mockPDFAnalyzer, validPDFAnalyzer helper, обновлены все существующие тесты, добавлены 12 новых тестов

**По результатам code-review исправлено:**
- W-2: io.Copy context errors (Canceled/DeadlineExceeded) passthrough raw вместо SERVICE_UNAVAILABLE
- W-3: Seek failure перед upload → SERVICE_UNAVAILABLE вместо INVALID_FORMAT
- S-1: Pre-allocate buffer по Content-Length hint
- S-5: Добавлен тест read error during streaming → SERVICE_UNAVAILABLE
- S-6: Добавлен тест context canceled during streaming → raw context.Canceled passthrough

**Summary:**
- Fetcher теперь буферизирует → валидирует PDF формат и число страниц → загружает в storage
- FetchResult заполняет PageCount и IsTextPDF из pdf.Analyze
- 32 теста fetcher: 26 Fetch subtests + 4 classifyDownloadError + 3 limitedReader
- Все 16 пакетов PASS с -race, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-024✅ разблокирует: TASK-035 (Processing Pipeline Orchestrator — но нужны ещё TASK-026, TASK-031, TASK-032, TASK-033)
- При DI в main.go: `fetcher.NewFetcher(httpDl, storageClient, pdf.NewUtil(), cfg.Limits.MaxFileSize, cfg.Limits.MaxPages)`
- Eligible задачи (high, deps met): TASK-008 (broker), TASK-009 (KV-store), TASK-011 (OCR client), TASK-012 (observability), TASK-021 (pending response registry), TASK-031 (temp artifact storage), TASK-043 (security validation)
- Нет больше критических задач с выполненными зависимостями

### TASK-008 — Инфраструктурный клиент брокера сообщений
**Статус:** done
**Дата:** 2026-03-22

**План реализации (согласован):**
1. Создать `internal/infra/broker/client.go`:
   - Consumer-side интерфейсы: AMQPAPI (Connection-level), AMQPChannelAPI (Channel-level)
   - Wrapper-адаптеры: amqpConnWrapper, amqpChanWrapper
   - Client struct: conn, pubCh, mu (RWMutex), subs, done, wg, dialFn, cancelCtx/cancelFn
   - Publish(ctx, topic, payload) — RLock на весь publish call
   - Subscribe(topic, handler) — QueueDeclare + Consume + consumer goroutine
   - Close() — idempotent graceful shutdown
2. Создать `internal/infra/broker/errors.go`:
   - mapError: context passthrough, AMQP codes → DomainError, unknown → retryable
3. Создать `internal/infra/broker/reconnect.go`:
   - reconnectLoop: NotifyClose watcher + IsClosed() check
   - reconnectWithBackoff: exponential backoff 1s→30s, 25% jitter, close old resources, re-subscribe
4. Создать `internal/infra/broker/client_test.go`:
   - Mock AMQP (function-field mocks, no external libs)
   - Publish/Subscribe/Close/MapError/Backoff/Reconnect тесты

**Ключевые решения:**
- 2 consumer-side интерфейса (AMQPAPI + AMQPChannelAPI) — матчит двухуровневую модель RabbitMQ
- MessageHandler func(ctx, body) error — callback с auto ack/nack
- cancelCtx для graceful handler cancellation при Close()
- RLock held for entire Publish — предотвращает TOCTOU race с reconnect
- IsClosed() check после NotifyClose — ловит edge case потерянного notification
- Close old conn/pubCh на reconnect — предотвращает fd leak
- backoffDelay с bit-shift вместо math.Pow, capped at attempt=5

**Review findings fixed:**
- W-1: TOCTOU race в Publish — hold RLock for entire call
- W-2: Missed close notification — IsClosed() check
- W-3: FD leak on reconnect — close old resources
- S-2: context.Background() → cancelCtx для graceful handler cancellation
- S-3: Re-subscribe errors captured
- S-5: backoffDelay overflow — cap + bit-shift
- S-6: Added 6 reconnect/backoff tests
- N-1: Sanitized broker address from error
- N-3: Comments for non-retryable AMQP codes

**Summary:**
- 4 файла: client.go, errors.go, reconnect.go, client_test.go
- 23 теста с -race: Publish (4), Subscribe (4+1 cancel-ctx), Close (2), mapError (6), interface (1), backoffDelay (3), reconnect (2)
- Все 17 пакетов PASS, go vet clean, make build/test/lint OK
- Зависимость: github.com/rabbitmq/amqp091-go v1.10.0

**Заметки для следующей итерации:**
- TASK-008✅ разблокирует: TASK-015 (Command Consumer), TASK-032 (Event Publisher), TASK-033 (DM Outbound), TASK-034 (DM Inbound)
- При DI в main.go: `brokerClient, err := broker.NewClient(cfg.Broker)` → inject в publisher/consumer adapters
- Eligible задачи (high, deps met): TASK-009 (KV-store), TASK-011 (OCR client), TASK-012 (observability), TASK-021 (pending response registry), TASK-031 (temp artifact storage), TASK-043 (security validation)
- Новые eligible задачи благодаря TASK-008: TASK-015 (deps: TASK-005✅, TASK-006✅, TASK-008✅), TASK-032 (deps: TASK-005✅, TASK-006✅, TASK-008✅), TASK-033 (deps: TASK-005✅, TASK-006✅, TASK-008✅)

### TASK-032 — Event Publisher: публикация статусных событий и событий завершения
**Статус:** done
**Дата:** 2026-03-22

**План реализации (согласован):**
1. Создать `internal/egress/publisher/publisher.go`:
   - Consumer-side интерфейс BrokerPublisher (Publish method) — dependency inversion
   - Publisher struct с BrokerPublisher и topicMap (5 топиков)
   - NewPublisher(broker, cfg) с nil-guard и empty-topic validation (panic)
   - publishJSON DRY-helper: json.Marshal → broker.Publish
   - 5 методов: PublishStatusChanged, PublishProcessingCompleted, PublishProcessingFailed, PublishComparisonCompleted, PublishComparisonFailed
2. Создать `internal/egress/publisher/publisher_test.go`:
   - mockBroker + ctxCapturingBroker
   - Topic routing, JSON format, round-trip, error handling, constructor validation

**Ключевые решения:**
- BrokerPublisher — 1-method consumer-side interface (Go idiom: define interfaces at consumer)
- topicMap вместо хранения полного BrokerConfig — минимальная зависимость
- Marshal errors → non-retryable DomainError (deterministic programming error, не retry)
- Broker errors passthrough (уже DomainError из broker.Client)
- Context errors passthrough raw (errors.Is semantics для orchestrator)
- Panic в конструкторе при nil broker или пустом топике (startup-time config error)
- Concurrency safety doc comment на BrokerPublisher

**Review findings fixed:**
- W-1: Marshal errors non-retryable (не NewBrokerError, а DomainError{Retryable: false})
- S-1: nil broker panic в конструкторе
- S-2: empty topic validation panic
- S-3: Context forwarding test
- N-3: Concurrency safety doc comment

**Summary:**
- 2 файла: publisher.go, publisher_test.go
- 24 теста с -race: interface compliance (1), correct topic routing (5), JSON format (5), round-trip (5), broker error (1), context.Canceled (1), context.DeadlineExceeded (1), marshal error (1), nil broker panic (1), empty topic panic (1), context forwarding (1), omitempty stage (1)
- Все 18 пакетов PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-032✅ разблокирует: участвует в deps TASK-035 (Processing Pipeline) и TASK-037 (Comparison Pipeline)
- Remaining blockers для TASK-035: TASK-026 (OCR rate limiter, blocked by TASK-011), TASK-031 (temp artifact storage), TASK-033 (DM outbound)
- Eligible задачи (high, deps met): TASK-009 (KV-store), TASK-011 (OCR client), TASK-012 (observability), TASK-015 (command consumer), TASK-021 (pending response registry), TASK-031 (temp artifact storage), TASK-033 (DM outbound), TASK-043 (security)
- При DI: `publisher.NewPublisher(brokerClient, cfg.Broker)` — returns *Publisher implementing EventPublisherPort
