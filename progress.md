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

### TASK-033 — DM Outbound Adapter
**Статус:** done
**Дата:** 2026-03-22

**План реализации (согласован с code-architect):**
1. Создать `internal/egress/dm/sender.go`:
   - Consumer-side `BrokerPublisher` интерфейс (своя копия, не импорт из publisher)
   - `Sender` struct реализует ОБА порта: `DMArtifactSenderPort` и `DMTreeRequesterPort`
   - `topicMap` с 3 топиками: artifactsReady, semanticTreeReq, diffReady
   - `publishJSON` DRY-helper: json.Marshal → broker.Publish
   - 3 метода: SendArtifacts, SendDiffResult, RequestSemanticTree
   - Constructor panic на nil broker и пустые топики (детерминированный порядок)
2. Создать `internal/egress/dm/sender_test.go`:
   - 22 теста: interface compliance, topic routing, JSON format, round-trip, errors, marshal, constructor, context forwarding, omitempty
3. Удалить `.gitkeep`

**Ключевые решения:**
- Один struct для двух портов: общая зависимость (broker) + одинаковый паттерн (marshal+publish)
- Повторение BrokerPublisher interface в своём пакете (Go interface-at-consumer idiom)
- Marshal errors → non-retryable DomainError (ErrCodeBrokerFailed), consistent с publisher
- Детерминированный порядок валидации топиков (slice вместо map) — улучшение по ревью

**Review findings:**
- S-1: Map iteration order → deterministic slice (applied)
- S-2: ErrCodeBrokerFailed для marshal — consistent, не меняем
- N-1..N-3: нитпики, не требуют действий

**Summary:**
- 2 файла: sender.go (~95 LOC), sender_test.go (~500 LOC)
- 22 теста с -race
- Все 19 пакетов PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-033✅ разблокирует: участвует в deps TASK-035 (Processing Pipeline) и TASK-037 (Comparison Pipeline)
- Remaining blockers для TASK-035: TASK-026 (OCR rate limiter, blocked by TASK-011), TASK-031 (temp artifact storage)
- Remaining blockers для TASK-037: TASK-021 (pending response registry), TASK-034 (DM inbound, blocked by TASK-021)
- Eligible задачи (high, deps met): TASK-009 (KV-store), TASK-011 (OCR client), TASK-012 (observability), TASK-015 (command consumer), TASK-021 (pending response registry), TASK-031 (temp artifact storage), TASK-043 (security)
- При DI: `dm.NewSender(brokerClient, cfg.Broker)` — returns *Sender implementing DMArtifactSenderPort + DMTreeRequesterPort

### TASK-031 — Temporary Artifact Storage Adapter
**Статус:** done
**Дата:** 2026-03-22

**План реализации:**
1. Создать consumer-side интерфейс `StorageClient` (Upload, Download, Delete, DeleteByPrefix) в `internal/egress/storage/adapter.go`
2. Реализовать `Adapter` struct с delegation к `StorageClient` и key prefixing (`keyPrefix + callerKey`)
3. Валидация empty key/prefix → non-retryable DomainError (ErrCodeStorageFailed)
4. Client errors passthrough без дополнительного wrapping
5. 19 unit-тестов с mock storage

**Summary:**
- Adapter в `internal/egress/storage/adapter.go` — реализует `TempStoragePort` через thin delegation к `StorageClient` consumer-side interface
- `StorageClient` interface: 4 метода, совпадает с `TempStoragePort`, реализуется `objectstorage.Client`
- Key prefixing: `NewAdapter(client, keyPrefix)` — `fullKey(key) = keyPrefix + key`. Позволяет multi-tenant/env-scoped namespaces
- Empty key/prefix валидация: non-retryable `DomainError` (programming error). Empty prefix в `DeleteByPrefix` — safety guard
- Constructor panic на nil client (паттерн publisher/sender)
- Compile-time check `var _ TempStoragePort = (*Adapter)(nil)`
- 19 тестов: interface (1), constructor panic (1), upload 6, download 3, delete 3, deleteByPrefix 3, context forwarding (1), data passthrough (1)
- code-reviewer: Approve (N-1..N-4 nits, N-3 исправлен — concurrency doc wording)
- 20 пакетов PASS -race, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-035 (critical, Processing Pipeline Orchestrator) теперь заблокирована только TASK-026 (→TASK-011) — все остальные зависимости done
- При DI: `storage.NewAdapter(s3Client, "")` или `storage.NewAdapter(s3Client, "dp/")` если bucket shared
- Eligible задачи (high, deps met): TASK-009 (KV-store), TASK-011 (OCR client), TASK-012 (observability), TASK-015 (command consumer), TASK-021 (pending response registry), TASK-043 (security)

### TASK-011 — Инфраструктурный клиент Yandex Cloud Vision OCR
**Статус:** done
**Дата:** 2026-03-22

**План реализации:**
1. Создать consumer-side интерфейс `HTTPAPI` (Do method) в `internal/infra/ocr/client.go`
2. Реализовать `Client` struct с HTTPAPI, endpoint, apiKey, folderID
3. `NewClient(cfg OCRConfig)` — production http.Client с Transport (MaxIdleConns=10, TLSHandshakeTimeout=10s), Timeout=120s
4. `Recognize(ctx, pdfData)`: io.ReadAll → base64 → JSON marshal recognizeRequest → POST `/ocr/v1/recognizeText` → JSON decode recognizeResponse → fullText
5. Headers: Authorization: Api-Key, x-folder-id, Content-Type: application/json
6. Error response body limited to 1024 bytes + drain remainder for connection reuse
7. Error mapping в errors.go: mapError (context passthrough, network → retryable), mapHTTPStatus (429/5xx → retryable, 4xx → non-retryable)
8. 22 unit-теста с httptest.NewServer

**Ключевые решения:**
- Consumer-side HTTPAPI interface — паттерн как S3API в objectstorage
- PDF читается полностью в память (io.ReadAll) — ограничено InputValidator до 20 MB, пиковая нагрузка ~50 MB/job (raw + base64 + JSON)
- HTTP timeout 120s — safety net, context от оркестратора служит основным таймаутом
- Error body в error message — для диагностики, ограничен 1024 bytes
- Drain response body на error path (io.Copy(io.Discard)) — для переиспользования TCP connections
- t.Errorf вместо t.Fatalf в httptest handlers — корректное поведение из не-test goroutines
- Нет retry/rate limiting — это ответственность TASK-026 (engine/ocr adapter)

**Summary:**
- Созданы 3 файла: client.go, errors.go, client_test.go в `internal/infra/ocr/`
- Реализует `OCRServicePort` из domain/port/outbound.go
- Yandex Cloud Vision OCR API v2: POST /ocr/v1/recognizeText, PDF→base64→JSON→fullText
- 22 теста (включая 7 HTTP status subtests), все проходят с -race
- code-reviewer: Approve with notes (W-1 drain body, W-7 t.Fatalf→t.Errorf, N-10 reader error test — все исправлены)
- 21 пакет PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-011✅ разблокирует: TASK-026 (OCR Integration Adapter с rate limiter)
- TASK-026✅ → разблокирует TASK-035 (critical, Processing Pipeline Orchestrator) — последний блокер на critical path
- Eligible задачи (high, deps met): TASK-009 (KV-store), TASK-012 (observability), TASK-015 (command consumer), TASK-021 (pending response registry), TASK-026 (OCR rate limiter — TASK-011✅+TASK-025✅), TASK-043 (security)
- При DI: `ocr.NewClient(cfg.OCR)` — возвращает *Client реализующий OCRServicePort

### TASK-026 — OCR Integration Adapter: rate limiter, warnings, retry (FR-1.2.1, FR-1.2.2)
**Статус:** done
**Дата:** 2026-03-22

**План реализации:**
1. Расширить `Adapter` struct в `internal/engine/ocr/adapter.go`:
   - Добавить `*warning.Collector`, `rpsLimit`, `maxAttempts`, `backoffBase`
   - Встроить token bucket rate limiter (mu, tokens, capacity, lastRefill)
2. Обновить `NewAdapter` конструктор — принимает 6 параметров вместо 2
3. Обновить `Process` — добавить retry loop с:
   - `acquireToken(ctx)` — блокирующее ожидание токена rate limiter
   - Exponential backoff между попытками (backoffBase * 2^(attempt-1))
   - Re-download PDF из storage на каждый retry (reader consumed)
4. Добавить `checkWarnings(rawText)` — OCR_PARTIAL_RECOGNITION / OCR_LOW_QUALITY
5. Написать 26 тестов (обновить 7 существующих + 19 новых)
6. Code review: исправить nil warnings panic, backoff/re-download ordering, shift overflow

**Ключевые решения:**
- Token bucket rate limiter встроен в adapter (не отдельный компонент) — простота, единственный потребитель
- `*warning.Collector` инжектится через конструктор (shared между stages pipeline)
- Reader re-download на retry вместо буферизации в памяти — экономия до 100MB (5 jobs × 20MB)
- Backoff ПЕРЕД re-download — не занимаем connection pool во время ожидания
- Warnings mutually exclusive: empty → partial, short → low quality
- Shift cap=30 в backoff для защиты от overflow при больших maxAttempts

**Summary:**
- Обновлены 2 файла: `adapter.go` (232 строки), `adapter_test.go` (948 строк)
- 26 тестов с -race: all PASS
- code-reviewer: 2 critical fixes applied (nil panic, backoff ordering), 1 warning fix (shift overflow)
- 21 пакет PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-026✅ разблокирует TASK-035 (critical, Processing Pipeline Orchestrator) — это был последний блокер!
- TASK-035 deps: TASK-019✅, TASK-020✅, TASK-022✅, TASK-024✅, TASK-026✅, TASK-027✅, TASK-028✅, TASK-029✅, TASK-031✅, TASK-032✅, TASK-033✅ — ВСЕ DONE
- Следующий приоритет: TASK-035 (critical, Processing Pipeline Orchestrator happy path)
- При DI: `ocr.NewAdapter(ocrClient, storageAdapter, warningCollector, cfg.OCR.RPSLimit, cfg.Retry.MaxAttempts, cfg.Retry.BackoffBase)`

### TASK-035 — Processing Pipeline Orchestrator: happy path
**Статус:** done
**Дата:** 2026-03-22

**План реализации (согласован):**
1. Изучить все 11 зависимостей: ports (inbound, engine, outbound), lifecycle manager, warning collector, engine components (validator, fetcher, ocr adapter, text extractor, structure extractor, semantic tree builder), egress adapters (temp storage, event publisher, DM sender)
2. Спроектировать Orchestrator struct с 11 зависимостями, определить HandleProcessDocument pipeline flow
3. Добавить OCRProcessorPort в port/engine.go — для замены concrete *ocr.Adapter зависимости (hexagonal architecture)
4. Реализовать Orchestrator: 9 pipeline stages + status transitions + warning collection + artifact sending
5. Shared *warning.Collector между OCR adapter и orchestrator (Reset per job)
6. Cleanup best-effort (log and continue, не блокировать completion)
7. Warnings собираются один раз и используются для artifacts и completion event
8. Добавить nil checks в LifecycleManager конструктор
9. 18 unit-тестов: happy paths, error paths, edge cases

**Summary:**
- Orchestrator в `internal/application/processing/orchestrator.go` (210 строк)
- Реализует `port.ProcessingCommandHandler` interface
- 11 зависимостей: lifecycle, warnings (shared), validator, fetcher, ocrProcessor, textExtract, structExtract, treeBuilder, tempStorage, publisher, dmSender
- Pipeline: NewProcessingJob → Reset warnings → jobCtx timeout → QUEUED→IN_PROGRESS → Validate → Fetch → OCR/OCR_SKIPPED → Text Extract → Structure Extract → Semantic Tree → allWarnings once → SendArtifacts → Cleanup (best-effort) → COMPLETED/COMPLETED_WITH_WARNINGS → PublishProcessingCompleted
- **OCRProcessorPort** новый интерфейс в port/engine.go — Process(ctx, storageKey, isTextPDF) (*OCRRawArtifact, error)
  - Compile-time check `var _ port.OCRProcessorPort = (*Adapter)(nil)` в ocr/adapter.go
- **Shared warning.Collector**: OCR adapter и orchestrator используют один и тот же *warning.Collector, Reset() в начале каждого job
- **Best-effort cleanup**: после отправки артефактов в DM ошибка DeleteByPrefix логируется, но не останавливает pipeline
- **Nil checks** добавлены в LifecycleManager (publisher, idempotency)
- 18 тестов (1.0s с -race):
  - Happy paths: text PDF no warnings → COMPLETED, text PDF with warnings → COMPLETED_WITH_WARNINGS, scanned PDF OCR → applicable, scanned PDF OCR low quality → OCR_LOW_QUALITY warning captured
  - Error paths: validation, fetch, text extraction, structure extraction, tree build, DM send, OCR error, context cancellation
  - Edge: cleanup best-effort, artifacts content, command fields copied, nil panics (11 subtests), publish error, stage progression
- 21 пакет PASS, go vet clean, make build/test/lint OK
- code-reviewer applied: OCRProcessorPort interface, best-effort cleanup, single warning collection, LifecycleManager nil checks, OCR error test, context cancellation test

**Заметки для следующей итерации:**
- TASK-035✅ разблокирует TASK-036 (critical, Processing Pipeline Orchestrator — error handling, retry, timeouts)
- TASK-036 deps: только TASK-035✅ — можно начинать
- При реализации TASK-036: shared *warning.Collector НЕ safe для concurrent HandleProcessDocument calls (TODO в коде). Нужно либо per-job collectors, либо scope by job ID
- WAITING_DM_CONFIRMATION — no-op placeholder, будет реализован в TASK-034 (DM Inbound Adapter)
- Готовые задачи для следующей итерации: TASK-036 (critical), TASK-009 (high, KV-store), TASK-012 (high, Observability), TASK-015 (high, Command Consumer), TASK-021 (high, Pending Response Registry)

### TASK-036 — Processing Pipeline Orchestrator: обработка ошибок, retry и таймаутов
**Статус:** done
**Дата:** 2026-03-22

**План реализации (согласован):**
1. Добавить classifyError — классификация ошибок в терминальный статус:
   - context.DeadlineExceeded → TIMED_OUT (is_retryable=true)
   - Validation/format codes (VALIDATION_ERROR, FILE_TOO_LARGE, TOO_MANY_PAGES, INVALID_FORMAT, FILE_NOT_FOUND) → REJECTED (is_retryable=false)
   - Всё остальное → FAILED (is_retryable=false, retries уже исчерпаны)
2. Добавить retryStep — retry с exponential backoff:
   - maxRetries и backoffBase в Orchestrator struct
   - time.NewTimer + Stop (без timer leak)
   - Только для retryable ошибок (port.IsRetryable)
   - Respects context cancellation между retries
3. Извлечь runPipeline — happy path с retryStep на fetcher/OCR/DM sender
4. Добавить handlePipelineError — обработка ошибок:
   - context.Background() для side-effects (jobCtx может быть expired)
   - TransitionJob → PublishProcessingFailed → DeleteByPrefix (всё best-effort)
   - Возвращает оригинальный pipelineErr
5. Обновить HandleProcessDocument: runPipeline → handlePipelineError на ошибке
6. Обновить NewOrchestrator: +maxRetries, +backoffBase

**Ключевые решения:**
- retryStep применяется только к fetcher, OCR, DM sender — validator/textextract/structextract/treebuilder не дают retryable ошибок
- handlePipelineError использует context.Background() — при TIMED_OUT jobCtx уже expired
- is_retryable в ProcessingFailedEvent: false для FAILED (DP исчерпал retries), true только для TIMED_OUT (upstream может пере-отправить)
- Cleanup в handlePipelineError — явный вызов, документирован double-cleanup с LifecycleManager (idempotent)
- rejectedCodes map — все validation/format errors ведут к REJECTED статусу

**Summary:**
- classifyError, retryStep, runPipeline, handlePipelineError добавлены в orchestrator.go
- maxRetries int + backoffBase time.Duration в Orchestrator struct
- 34 теста с -race (18 existing updated + 16 new):
  - REJECTED: 5 variants (validation, file too large, invalid format, too many pages, file not found)
  - Retry: OCR success after retry, exhausted retries → FAILED
  - TIMED_OUT: DeadlineExceeded → ProcessingFailedEvent(is_retryable=true)
  - FAILED: non-retryable extraction, broker retries exhausted
  - Cleanup: на каждом терминальном пути (REJECTED, FAILED, TIMED_OUT)
  - ProcessingFailedEvent: все поля проверены
  - classifyError: 12-case table-driven
  - retryStep: ctx cancellation
- Code review: 0 critical, 5 warnings fixed (classifyError comment, timer leak, double cleanup doc, is_retryable semantics)
- 21 пакет PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-036✅ разблокирует TASK-039 (main entry point, но зависит ещё от TASK-017, TASK-037, TASK-038)
- Готовые задачи (pending, все deps done): TASK-009 (high, KV-store), TASK-012 (high, Observability), TASK-015 (high, Command Consumer), TASK-021 (high, Pending Response Registry), TASK-043 (high, Security)
- shared *warning.Collector по-прежнему не safe для concurrent calls — при параллельной обработке нужен per-job collector

### TASK-037 — Comparison Pipeline Orchestrator: happy path
**Статус:** done
**Дата:** 2026-03-24

**План реализации (согласован с code-architect):**
1. Создать `internal/application/comparison/orchestrator.go` — реализация `port.ComparisonCommandHandler`
2. Orchestrator struct: 7 зависимостей (lifecycle, warnings, treeReq, dmSender, registry, comparer, publisher) + maxRetries, backoffBase
3. `HandleCompareVersions` → `runPipeline` (6 стадий) → `handlePipelineError` (error handling)
4. 6-стадийный pipeline:
   - VALIDATING_INPUT — transition QUEUED→IN_PROGRESS + validateCompareCommand
   - REQUESTING_SEMANTIC_TREES — Register BEFORE send (race protection), 2x RequestSemanticTree с retryStep, Cancel при ошибке
   - WAITING_DM_RESPONSE — AwaitAll блокирует до получения обоих деревьев
   - EXECUTING_DIFF — comparer.Compare(baseTree, targetTree)
   - SAVING_COMPARISON_RESULT — SendDiffResult с retryStep
   - WAITING_DM_CONFIRMATION — второй Register/AwaitAll цикл для DocumentVersionDiffPersisted
5. Correlation ID формат: `{jobID}:base:{versionID}`, `{jobID}:target:{versionID}`, `{jobID}:diff-confirm`
6. classifyError: DeadlineExceeded/Canceled→TIMED_OUT, VALIDATION/DM_VERSION_NOT_FOUND→REJECTED, default→FAILED
7. handlePipelineError: terminal status guard + registry.Cancel + ComparisonFailedEvent
8. validateCompareCommand: пустые поля, base_version_id ≠ target_version_id

**Ключевые решения:**
- Два цикла Register/AwaitAll: первый для semantic trees, второй для diff persist confirmation. Между циклами первый cleanup автоматически (AwaitAll очищает entry). Второй использует `{jobID}:diff-confirm` как единственный correlationID
- Register BEFORE send — критически важно: если DM ответит до registry.Register, ответ будет потерян
- context.Canceled обработан (в отличие от processing orchestrator) — маппинг на TIMED_OUT/retryable
- Terminal status guard в handlePipelineError: если job уже COMPLETED (а потом PublishComparisonCompleted провалился), не пытаемся COMPLETED→FAILED
- Retry только на RequestSemanticTree и SendDiffResult — AwaitAll и Compare не имеют retryable ошибок
- Для diff confirm confirmation: registry.Receive вызывается с пустым SemanticTree{} — оркестратор проверяет только resp.Err == nil
- validateCompareCommand — базовая валидация полей команды, отсутствующая в processing orchestrator

**Code review (code-reviewer):**
- 2 critical fixed: context.Canceled→TIMED_OUT; handlePipelineError terminal status guard
- 5 warnings addressed: input validation added, unbounded bgCtx documented, unused variable removed, shared collector documented, mock improvements noted
- 5 suggestions noted for future: code deduplication across orchestrators, structured logging, per-method mock errors

**Summary:**
- orchestrator.go: Orchestrator struct, NewOrchestrator, HandleCompareVersions, runPipeline, handlePipelineError, classifyError, retryStep, validateCompareCommand
- orchestrator_test.go: 40 тестов с -race:
  - Constructor: 8 (7 nil panics + defaults)
  - classifyError: 8 table-driven (DeadlineExceeded, Canceled, WrappedCanceled, Validation, DMVersionNotFound, Broker, Generic, WrappedDeadlineExceeded)
  - Happy path: 8 (completed, warnings, correlation IDs, register order, tree routing, diff result, cmd fields, confirm cycle)
  - Error handling: 11 (base req, target req, await timeout, DM_VERSION_NOT_FOUND, comparer, diff send, confirmation, nil tree, register, transition, publish completed)
  - handlePipelineError: 2 (field completeness, registry cancel)
  - retryStep: 5 (success, non-retryable, context cancel, exhaust retries, retry then success)
  - Input validation: 6 (4 empty fields, same versions, valid)
  - Integration: 4 (stage progression, version IDs, no completion on error, warnings reset, terminal guard)
- 29 пакетов PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-037✅ разблокирует TASK-038 (comparison error handling/timeouts)
- TASK-038 разблокирует TASK-039 (critical — main entry point)
- Pending tasks с all deps done: TASK-038 (medium), TASK-043 (high, security), TASK-044 (high, audit), TASK-045 (low, PDF CMap)
- Для TASK-038: handlePipelineError уже реализован в TASK-037 — нужно добавить specific error scenarios и подробные тесты для каждого failure mode
- DM Receiver (receiver.go) маршрутизирует DiffPersisted/DiffPersistFailed через DMResponseHandler, НЕ через registry напрямую — wiring (TASK-039) должен обеспечить bridge

### TASK-038 — Comparison Pipeline Orchestrator: обработка ошибок и таймаутов
**Статус:** done
**Дата:** 2026-03-24

**План реализации (согласован с code-architect):**
1. Gap analysis: classifyError hardcodes `is_retryable=false` для FAILED, но AC#4 требует passthrough из DomainError
2. Fix classifyError: `return model.StatusFailed, port.IsRetryable(err)` вместо hardcoded false
3. Добавить ErrCodeDMDiffPersistFailed + NewDMDiffPersistFailedError в port/errors.go
4. Добавить 7 новых тестов + 3 table cases + fix существующего BrokerFailed case
5. Code review → fix warnings → финальная проверка

**Ключевые решения:**
- `classifyError` теперь пропускает `is_retryable` из DomainError.Retryable для FAILED status. Это корректно для всех случаев:
  - BrokerError (Retryable=true) → (FAILED, true) — upstream может ретраить позже
  - StorageError (Retryable=true) → (FAILED, true)
  - ExtractionError (Retryable=false) → (FAILED, false) — детерминистическая ошибка
  - DiffPersistFailed от DM → is_retryable берётся из DM события через конструктор NewDMDiffPersistFailedError
- Для plain `errors.New()` (non-DomainError), `port.IsRetryable()` возвращает false — безопасный default
- Cleanup через lifecycle TransitionJob (CleanupFunc runs on terminal) + registry.Cancel в handlePipelineError
- blockingRegistry для тестирования реального context timeout

**Code review (code-reviewer):**
- 0 critical
- 3 warnings fixed: W-1 added NewDMDiffPersistFailedError tests in errors_test.go, W-2 added callCount to mockTreeRequester for retry verification, W-3 contract change documented
- 4 suggestions noted: timeout aggressiveness (5ms), blockingRegistry delegation comment, field completeness duplication, non-DomainError wrap case

**Summary:**
- orchestrator.go: classifyError fix (1 line: `port.IsRetryable(err)` вместо hardcoded `false`), обновлён godoc comment
- port/errors.go: ErrCodeDMDiffPersistFailed + NewDMDiffPersistFailedError(msg, retryable, cause)
- orchestrator_test.go: 3 новых classifyError table cases + 7 новых тестов:
  - DiffPersistFailed retryable/non-retryable passthrough (2)
  - BrokerError retryable after retry exhaustion
  - Real job context timeout via blockingRegistry
  - Cleanup on failure / cleanup on timeout (2)
  - Full ComparisonFailedEvent field completeness for DiffPersistFailed
- errors_test.go: 2 новых constructor tests для NewDMDiffPersistFailedError
- mockTreeRequester: добавлен callCount для точной верификации retry count
- 29 пакетов PASS с -race, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-038✅ разблокирует TASK-039 (critical — main entry point, DI, graceful shutdown)
- Все зависимости TASK-039 теперь done: TASK-007, TASK-017, TASK-035, TASK-036, TASK-037, TASK-038
- Pending tasks с all deps done: TASK-039 (critical), TASK-043 (high, security), TASK-044 (high, audit), TASK-045 (low, PDF CMap)
- DM Receiver маршрутизирует DiffPersistFailed через DMResponseHandler — wiring в TASK-039 должен bridge к registry через NewDMDiffPersistFailedError

### TASK-009 — Инфраструктурный клиент KV-store для Idempotency Guard
**Статус:** done
**Дата:** 2026-03-23

**План реализации:**
1. Спроектировать архитектуру KV-store клиента (code-architect): Redis как backing store, consumer-side interface RedisAPI, разделение от IdempotencyStorePort
2. Добавить KVStoreConfig в config (Address required, Password, DB=0, PoolSize=10, Timeout=5s)
3. Реализовать client.go: Client struct, NewClient (redis.NewClient + Ping healthcheck), Set/Get/Exists/Close
4. Реализовать errors.go: ErrKeyNotFound sentinel, mapError (context passthrough, redis.Nil → ErrKeyNotFound, остальные → retryable STORAGE_FAILED)
5. Написать unit-тесты с mockRedis и in-memory store
6. Code review (code-reviewer): 0 critical, 2 warnings fixed (use-after-close guard, Close error mapping)

**Ключевые решения:**
- Redis (go-redis/v9) — connection pooling встроен, native TTL support
- KV-клиент НЕ реализует IdempotencyStorePort — это generic KV-клиент, port реализуется в ingress/idempotency (TASK-016)
- ErrKeyNotFound — plain error sentinel (не DomainError), чтобы "ключ не найден" не трактовалось как инфраструктурная ошибка
- Use-after-close guard на Set/Get/Exists — non-retryable StorageError
- mapError: context passthrough raw (consistent with broker, objectstorage, ocr)

**Реализация:**
- internal/infra/kvstore/client.go — Client struct, RedisAPI interface, NewClient, Set/Get/Exists/Close, isClosed
- internal/infra/kvstore/errors.go — ErrKeyNotFound, mapError, errClientClosed
- internal/infra/kvstore/client_test.go — 26 тестов с -race
- internal/config/sub_configs.go — KVStoreConfig struct + loadKVStoreConfig()
- internal/config/config.go — KVStore field, Load(), Validate()
- internal/config/config_test.go — обновлены для DP_KVSTORE_ADDRESS
- go.mod/go.sum — github.com/redis/go-redis/v9 v9.18.0

**Тесты (26):**
- Set: success, Redis error, context cancelled, zero TTL
- Get: success, key not found (sentinel, not DomainError), Redis error, deadline exceeded
- Exists: present, absent, Redis error
- Close: graceful shutdown, idempotent
- mapError: canceled, deadline, Nil, unknown
- In-memory: Set→Get→Exists sequence
- Concurrent: 50 goroutines
- Use-after-close: Set/Get/Exists
- Context forwarding: Set/Get/Exists
- Key forwarding: Exists

**Заметки для следующей итерации:**
- TASK-009✅ разблокирует TASK-016 (Idempotency Guard) ← реализован ниже

---

### TASK-016 — Idempotency Guard: проверка дедупликации задач по job_id через KV-store
**Статус:** done
**Дата:** 2026-03-23

**План реализации:**
1. Добавить SetNX в kvstore.RedisAPI interface и Client (атомарный set-if-not-exists)
2. Создать Store struct в ingress/idempotency/, реализующий IdempotencyStorePort
3. Consumer-side interface KVStoreAPI (Get/Set/SetNX) для dependency inversion
4. Check: Get → ErrKeyNotFound → StatusNew, иначе parse value → status
5. Register: SetNX → first-writer-wins, !acquired → DuplicateJobError
6. MarkCompleted: Set (unconditional, refreshes TTL)
7. Валидация пустого jobID на всех методах
8. Unit-тесты с mock KVStoreAPI
9. Code review и исправления

**Summary:**
Store в internal/ingress/idempotency/store.go реализует port.IdempotencyStorePort через consumer-side KVStoreAPI interface. Key naming: "idempotency:{jobID}". Register использует SetNX для атомарной дедупликации (защита от race conditions между инстансами). MarkCompleted использует unconditional Set (by design — только winner вызывает). Добавлен SetNX в kvstore.Client и RedisAPI. Валидация пустого jobID. Code review: 0 critical, 3 warnings fixed (empty jobID, SetNX в concurrent test, MarkCompleted doc).

**Изменённые файлы:**
- internal/ingress/idempotency/store.go — Store struct, NewStore, Check/Register/MarkCompleted, KVStoreAPI interface, keyFor, validateJobID
- internal/ingress/idempotency/store_test.go — 26 тестов с -race
- internal/infra/kvstore/client.go — SetNX добавлен в RedisAPI interface и Client
- internal/infra/kvstore/client_test.go — 7 новых тестов SetNX (итого 33)

**Тесты (26 idempotency + 7 new kvstore):**
- Constructor: nil kv, zero TTL, negative TTL
- Check: new (ErrKeyNotFound), in_progress, completed, storage error, context canceled, deadline, invalid value, key prefix
- Register: success, already exists (DuplicateJobError), storage error, context canceled
- MarkCompleted: success, storage error, context canceled
- Empty jobID: Check/Register/MarkCompleted
- Full lifecycle: Check→Register→Check→Register dup→MarkCompleted→Check
- Context forwarding: Check/Register/MarkCompleted
- Interface compliance
- kvstore SetNX: success, key exists, Redis error, context canceled, context forwarding, after close

**Заметки для следующей итерации:**
- TASK-016✅ разблокирует TASK-017 (Ingress Layer Integration: Consumer → Idempotency Guard → Concurrency Limiter → dispatch)
- TASK-017 зависит от TASK-013 (Concurrency Limiter), TASK-015 (Command Consumer), TASK-016 (Idempotency Guard ✅)
- TASK-013 зависит от TASK-012 (Observability SDK) — следующий блокер на критическом пути к TASK-039
- Готовые задачи (pending, все deps done): TASK-012 (high, Observability), TASK-015 (high, Command Consumer), TASK-021 (high, Pending Response Registry), TASK-043 (high, Security)

---

### TASK-012 — Observability SDK — структурированное логирование, метрики, трейсинг
**Статус:** done
**Дата:** 2026-03-23

**План реализации:**
1. Спроектировать архитектуру (code-architect): 5 файлов в `internal/infra/observability/`
2. Реализовать context.go — JobContext propagation через context.Context
3. Реализовать logger.go — slog wrapper с auto-extraction JobContext
4. Реализовать metrics.go — Prometheus metrics с dedicated Registry
5. Реализовать tracer.go — OpenTelemetry + OTLP/HTTP с no-op fallback
6. Реализовать observability.go — SDK composite (New/Shutdown)
7. Обновить config — TracingEnabled, TracingInsecure, envBool
8. Написать unit-тесты (39 observability + 5 config)
9. Code review + исправления

**Summary:**
SDK в `internal/infra/observability/`. **Logger**: slog (stdlib) wrapper, JSON output на stderr, auto-extraction JobContext из ctx (job_id, document_id, correlation_id, stage), With() для component-scoped логгеров. **Metrics**: prometheus/client_golang с dedicated Registry — dp_job_duration_seconds (HistogramVec), dp_job_status_total (CounterVec), dp_ocr_duration_seconds (HistogramVec), dp_concurrent_jobs_active (Gauge), dp_file_size_bytes (Histogram). **Tracer**: OTel + OTLP/HTTP exporter, no-op fallback, sync.Once для global provider/propagator, W3C TraceContext + Baggage, configurable insecure transport. **Context**: JobContext struct, WithJobContext/JobContextFrom/WithStage. **SDK composite**: New(ctx, cfg) → *SDK{Logger, Metrics, Tracer}. Code review: 2 critical fixed (configurable insecure transport, sync.Once for global state), 2 warnings fixed (warning alias, SetTracerProvider).

**Тесты (39 observability + 5 config = 44 всего):**
- Context: round-trip, empty ctx zero-value, WithStage preserves fields, WithStage on empty ctx, overwrite, multiple WithStage
- Logger: parseLevel (11 subtests), non-nil, JSON output, all levels, JobContext extraction, partial context, empty context, With child, With+JobContext, Slog, level filtering (2)
- Metrics: create without panic, Registry non-nil, all metrics observed (5 subtests), no registration conflicts, Gather metric names, Gather before observation, multiple label values
- Tracer: disabled=noop, empty endpoint=noop, both=noop, StartSpan no panic, SpanFromContext, SpanFromContext background, Shutdown no error, Shutdown multiple, Enabled table-driven (3 subtests)
- SDK: all components non-nil, tracing disabled, tracing+empty endpoint, Shutdown, Shutdown multiple, different log levels (5 subtests), metrics registry accessible
- Config: envBool valid values (8 subtests), empty falls back, invalid falls back, Load TracingEnabled, default false

**Заметки для следующей итерации:**
- TASK-012✅ разблокирует TASK-013 (Concurrency Limiter) → TASK-017 → TASK-039
- TASK-012✅ также разблокирует TASK-044 (Audit logging)
- Готовые задачи (pending, все deps done): TASK-013 (high, Concurrency Limiter — deps: TASK-001✅, TASK-007✅, TASK-012✅), TASK-015 (high, Command Consumer), TASK-021 (high, Pending Response Registry), TASK-043 (high, Security)
- TASK-013 на критическом пути к TASK-039 через TASK-017

### TASK-013 — Concurrency Limiter
**Статус:** done
**Дата:** 2026-03-23

**План реализации (согласован):**
1. Добавить метрику `dp_concurrent_jobs_waiting` (Gauge) в `observability/metrics.go`
2. Создать `internal/infra/concurrency/limiter.go`:
   - Semaphore struct с buffered channel (cap=maxJobs)
   - New(maxJobs, metrics, logger) конструктор с nil-safety
   - Acquire(ctx): fast path (non-blocking) + slow path (blocking + ctx cancellation)
   - Release(): channel receive с default guard для mismatched release
3. Создать `internal/infra/concurrency/limiter_test.go`:
   - Тесты конструктора, blocking, context cancellation, metrics, concurrent stress
4. Обновить тесты observability для новой метрики
5. Code review и исправления

**Ключевые решения:**
- Channel-based semaphore (идиоматичный Go) вместо sync-based
- Два этапа Acquire: fast path (не трогает waiting metric) + slow path (Inc/Dec waiting)
- Context errors passthrough raw (не DomainError) — паттерн из kvstore/broker
- Без Close() — нет ресурсов, shutdown через context cancellation
- Release с default guard: mismatched release → warn (не panic) для production safety

**Summary:**
Semaphore в `internal/infra/concurrency/limiter.go`. Channel-based (buffered `chan struct{}`). `New()` с nil-safety (panic на nil metrics/logger), maxJobs<1 → 1. `Acquire(ctx)`: fast path non-blocking → slow path с waiting gauge + ctx.Done. `Release()`: channel drain с default guard. Метрики: `dp_concurrent_jobs_active` (existing), `dp_concurrent_jobs_waiting` (new). Code review: 0 critical, 4 warnings (waiting gauge approximation, nil-safety fixed, double-release documented, time.Sleep in tests).

**Тесты (21 с -race):**
- Constructor: zero→1, negative→1, configured, nil metrics panic, nil logger panic
- Interface compliance
- Happy path: immediate, cycle, exactly max concurrent
- Blocking: blocks then unblocks on Release
- Context: canceled, deadline exceeded, already cancelled, fast-path cancelled ctx succeeds
- Metrics: active inc/dec, waiting inc blocking, waiting dec on cancel
- Stress: 50 goroutines × 100 iterations, max≤5 enforced, final gauges=0
- Edge: release without acquire (no panic, gauge unchanged)

**Заметки для следующей итерации:**
- TASK-013✅ разблокирует TASK-017 (Ingress Integration)
- Готовые задачи (pending, все deps done): TASK-015 (high, Command Consumer), TASK-021 (high, Pending Response Registry), TASK-043 (high, Security), TASK-044 (high, Audit logging)
- TASK-015 + TASK-013✅ + TASK-016✅ → разблокируют TASK-017 → TASK-039
- Критический путь к Main Entry Point: TASK-015 → TASK-017 → ... → TASK-039

### TASK-015 — Command Consumer
**Статус:** done
**Дата:** 2026-03-23

**План реализации (согласован):**
1. Создать `internal/ingress/consumer/consumer.go`:
   - Consumer-side интерфейс `BrokerSubscriber` (dependency inversion)
   - `Consumer` struct с BrokerSubscriber, ProcessingCommandHandler, ComparisonCommandHandler, Logger
   - `NewConsumer()` — panic на nil deps и empty topic names
   - `Start()` — идемпотентный (sync.Once), подписка на оба топика
   - `handleProcessDocument()` — unmarshal → validate → context enrichment → dispatch → always nil
   - `handleCompareVersions()` — аналогично
2. Создать `internal/ingress/consumer/validate.go`:
   - `validateProcessDocumentCommand()` — batch validation: job_id, document_id, file_url
   - `validateCompareVersionsCommand()` — batch validation: job_id, document_id, base_version_id, target_version_id
   - TrimSpace для защиты от whitespace-only значений
   - Aggregated error listing all missing fields
3. Тесты: 38 тестов с -race

**Ключевые решения:**
- Always return nil после dispatch (prevent poison-pill requeue loops) — handler управляет failure semantics
- Invalid/malformed messages → ack + log error (не возвращать в очередь)
- Handler errors → ack + log warn (handler уже опубликовал failure events)
- Batch validation (collect all missing fields) для удобства отладки
- sync.Once для идемпотентного Start() (защита от двойной подписки)
- Context enrichment через observability.WithJobContext перед dispatch

**Code review результаты:**
- 0 critical, 4 warnings fixed:
  - W-1: Empty topic names guard в конструкторе → FIXED: panic
  - W-2: Idempotent Start → FIXED: sync.Once
  - W-3: Partial subscription failure → FIXED: документирован в godoc
  - W-4: FileURL scheme validation → DEFERRED: покрывается TASK-043

**Summary:**
- 2 файла: consumer.go (Consumer struct, BrokerSubscriber interface, Start, handlers), validate.go (2 validation functions)
- 38 тестов: 6 constructor panics, 3 Start, 6 process handler, 5 compare handler, 1 integration, 2 context enrichment, 1 partial failure, 2 nil body, 12 validation
- 27 пакетов PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-015✅ разблокирует TASK-017 (Ingress Integration) — все deps done: TASK-013✅, TASK-015✅, TASK-016✅
- Готовые задачи (pending, все deps done): TASK-017 (high, Ingress Integration), TASK-021 (high, Pending Response Registry), TASK-043 (high, Security), TASK-044 (high, Audit logging)
- Критический путь: TASK-017 → TASK-039 (Main Entry Point, через TASK-037/038)
- TASK-021 также на критическом пути: TASK-021 → TASK-034 → TASK-037 → TASK-039

### TASK-017 — Интеграция Ingress-слоя: Consumer → Idempotency Guard → Concurrency Limiter → dispatch
**Статус:** done
**Дата:** 2026-03-23

**План реализации (согласован):**
1. Создать `internal/ingress/dispatcher/dispatcher.go` — координатор ingress pipeline (SRP)
2. Consumer-side interface `CommandDispatcher` в consumer.go (dependency inversion)
3. Dispatcher: `runPipeline(ctx, jobID, handler)` — общий поток:
   - Register (atomic SetNX) → DuplicateJobError → skip
   - Acquire(ctx) → error → MarkCompleted cleanup → return nil
   - defer Release() → handler dispatch
   - MarkCompleted best-effort
4. Модифицировать consumer.go: inject Dispatcher вместо прямых handler-ов
5. Модифицировать consumer_test.go: mockDispatcher
6. Написать dispatcher_test.go: полное покрытие

**Ключевые решения:**
- Отдельный пакет dispatcher (не inline в consumer) — SRP, тестируемость
- Register (atomic SetNX) вместо Check+Register — нет TOCTOU race condition
- MarkCompleted всегда после handler (retry управляется оркестратором внутренне)
- Always return nil (ack) — даже при Acquire timeout
- При Acquire failure — MarkCompleted cleanup чтобы не блокировать job на 24h TTL

**Code review:**
- 1 critical: C-1 Acquire failure → MarkCompleted cleanup (не блокировать job на 24h)
- 4 warnings: compile-time check, DRY runPipeline, Start docs, rawPreview(body)
- 3 suggestions: ctx propagation test, concurrent dispatch test, cleanup error test

**Summary:**
- 2 новых файла: dispatcher.go, dispatcher_test.go
- 2 изменённых файла: consumer.go, consumer_test.go
- 27 тестов dispatcher, 28 тестов consumer (обновлены)
- 28 пакетов PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-017✅ разблокирует TASK-039 (Main Entry Point) — но TASK-039 ещё ждёт TASK-037/038
- Готовые задачи (pending, все deps done): TASK-021 (high, Pending Response Registry), TASK-043 (high, Security), TASK-044 (high, Audit logging)
- Критический путь: TASK-021 → TASK-034 → TASK-037 → TASK-038 → TASK-039

### TASK-021 — Pending Response Registry
**Статус:** done
**Дата:** 2026-03-24

**План реализации (согласован с code-architect):**
1. Добавить `ErrCodeDMVersionNotFound` и `NewDMVersionNotFoundError` в `port/errors.go`
2. Добавить `PendingResponse` struct и `PendingResponseRegistryPort` interface в `port/outbound.go`
3. Создать `internal/application/pendingresponse/registry.go`:
   - `Registry` struct: `sync.Mutex` + `map[string]*entry` + reverse index `map[string]string`
   - `entry` struct: expected set, responses map, done channel, `sync.Once` для close
   - `Register`: batch-валидация (empty/dup/in-use), no-mutation-on-error
   - `AwaitAll`: select done/ctx.Done, sorted responses, cleanup on return
   - `Receive`: deep copy SemanticTree, idempotent, close done on last
   - `ReceiveError`: nil-error guard, idempotent
   - `Cancel`: idempotent, unblocks AwaitAll, cleanup
4. Создать `internal/application/pendingresponse/registry_test.go`: 40 тестов с -race
5. Прогнать все тесты, go vet, make targets

**Ключевые решения:**
- Пакет `internal/application/pendingresponse/` — application-layer логика координации (параллельно с lifecycle, warning)
- Таймаут через caller's context — не internal timeout (следуя паттерну всех port-методов)
- Single `sync.Mutex` + per-entry `done` channel + `sync.Once` — проще чем RWMutex + per-entry Mutex
- Deep copy SemanticTree в Receive (deepCopyTree/deepCopyNode) — защита от мутации shared Root/*SemanticNode
- `PendingResponse` в port пакете (рядом с interface), не в application пакете — избегаем циклических зависимостей
- Empty slice (не nil) для JSON-совместимости
- Sorted by CorrelationID для детерминизма

**Code review (code-reviewer):**
- 1 critical fixed: C-1 — shallow copy SemanticTree → deep copy (Root pointer + Children + Metadata maps shared)
- 2 warnings fixed: W-3 — nil error guard в ReceiveError, W-4 — empty slice вместо nil
- 1 warning documented: W-2 — Cancel-vs-AwaitAll ambiguity (nil error + empty responses при Cancel)

**Summary:**
- 2 новых файла: registry.go, registry_test.go
- 2 изменённых файла: port/outbound.go, port/errors.go
- 40 тестов: interface compliance (1), constructor (1), Register (8), Receive (4), ReceiveError (4), AwaitAll (7), Cancel (4), lifecycle (2), concurrent (5), edge (4)
- 28 пакетов PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-021✅ разблокирует TASK-034 (DM Inbound Adapter)
- Готовые задачи (pending, все deps done): TASK-034 (high, DM Inbound Adapter), TASK-043 (high, Security), TASK-044 (high, Audit logging)
- Критический путь: TASK-034 → TASK-037 → TASK-038 → TASK-039

### TASK-034 — DM Inbound Adapter: подписка и корреляция ответов от Document Management
**Статус:** done
**Дата:** 2026-03-24

**План реализации (согласован с code-architect):**
1. Изучить зависимости: модели событий (5 DM→DP events), порты (DMResponseHandler, PendingResponseRegistryPort), broker client (Subscribe), pending response registry (Receive/ReceiveError), существующие паттерны (consumer, sender)
2. Спроектировать Receiver struct в egress/dm/ рядом с sender.go:
   - BrokerSubscriber consumer-side interface
   - DMResponseHandler для 4 event types (artifacts persisted/failed, diff persisted/failed)
   - PendingResponseRegistryPort для SemanticTreeProvided (correlation-based dispatch)
   - Start() с sync.Once для 5 подписок
3. Реализовать receiver.go: Receiver struct, NewReceiver, Start(), 5 handlers
4. Реализовать validate.go: 5 валидаторов (по одной на тип события)
5. Написать тесты: receiver_test.go + validate_test.go
6. Code review, исправления, полная проверка

**Ключевые решения:**
- Файлы в `internal/egress/dm/` рядом с sender.go — симметричная пара (sender→DM, receiver←DM)
- SemanticTreeProvided → registry.Receive/ReceiveError напрямую, минуя DMResponseHandler — correlation-based async dispatch через PendingResponseRegistryPort
- Если SemanticTree.Root == nil → registry.ReceiveError (пустое дерево = ошибка версии)
- Остальные 4 события → handler.Handle* (DMResponseHandler port для orchestrators)
- BrokerSubscriber повторена локально (Go consumer-side interface idiom, как в consumer и sender)
- rawPreview дублирована (отдельные Go packages, нет shared utils)
- Все handlers return nil (no requeue) — poison-pill prevention, ошибки логируются
- PersistFailed-события содержат is_retryable — передаётся handler для управления retry-логикой
- correlation_id в JobContext для structured logging во всех 5 handlers
- dmTopicMap (отдельный от sender's topicMap, т.к. в одном package)

**Code review (code-reviewer):**
- 0 critical issues
- W1 (rawPreview duplication) — следуем установленному паттерну проекта
- W2 (BrokerSubscriber duplication) — Go idiom: define interface at consumer
- W3 (HandleSemanticTreeProvided в DMResponseHandler unused by Receiver) — не меняем port, не входит в скоуп
- W4 (correlation_id not validated for 4 events) — для них он информационный, не маршрутизационный
- S6 (boundary test) — добавлен TestRawPreview_ExactBoundary

**Summary:**
- 4 новых файла: receiver.go, validate.go, receiver_test.go, validate_test.go
- receiver.go: Receiver struct (BrokerSubscriber, DMResponseHandler, PendingResponseRegistryPort, Logger, dmTopicMap), NewReceiver (panic-on-nil), Start() (sync.Once, 5 subscriptions), 5 handlers, rawPreview
- validate.go: 5 validators (validateArtifactsPersisted, validateArtifactsPersistFailed, validateSemanticTreeProvided, validateDiffPersisted, validateDiffPersistFailed)
- 65 тестов в dm package (45 receiver + 20 validation):
  - Constructor: 4 nil panics + 5 empty topic panics (subtests)
  - Start: 5 subscriptions, failure, idempotent, partial failure
  - handleArtifactsPersisted: valid, invalid JSON, missing fields, handler error, empty body, nil body
  - handleArtifactsPersistFailed: valid, invalid JSON, missing error_message, handler error
  - handleSemanticTreeProvided: valid→registry.Receive, empty tree→registry.ReceiveError, invalid JSON, missing correlation_id, missing version_id, registry receive error, registry receive error error
  - handleDiffPersisted: valid, invalid JSON, missing fields, handler error
  - handleDiffPersistFailed: valid, invalid JSON, missing error_message, handler error
  - Integration: Start + dispatch all 5 events through broker handlers
  - Context enrichment: artifacts persisted, diff persist failed
  - rawPreview: short, truncated, empty, nil, exact boundary (200 bytes)
  - Validation: valid, missing fields, all missing, whitespace-only (per event type)
- 28 пакетов PASS, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-034✅ разблокирует TASK-037 (Comparison Pipeline Orchestrator — happy path)
- TASK-037 deps: TASK-019✅, TASK-021✅, TASK-030✅, TASK-032✅, TASK-033✅, TASK-034✅ — ВСЕ DONE
- Готовые задачи (pending, все deps done): TASK-037 (medium, Comparison Pipeline happy path), TASK-043 (high, Security), TASK-044 (high, Audit logging)
- Критический путь: TASK-037 → TASK-038 → TASK-039 → TASK-040/041/042
- При DI: `dm.NewReceiver(brokerClient, dmResponseHandler, pendingRegistry, logger, cfg.Broker)` → .Start()

---

### TASK-039 — Main entry point — DI, запуск, graceful shutdown, readiness/liveness probes
**Статус:** done
**Дата:** 2026-03-25
**Summary:** Реализован полный main entry point для dp-worker сервиса. Ручной DI wiring
всех компонентов, graceful shutdown, HTTP health/readiness probes.

**План реализации:**
1. Изучение существующей кодовой базы (Explore agent) — конструкторы, зависимости
2. Проектирование архитектуры (code-architect) — App struct в internal/app/, shutdown order
3. Реализация (golang-pro):
   - `internal/infra/health/handler.go` — /healthz (liveness, always 200), /readyz (readiness, atomic bool)
   - `internal/app/app.go` — App struct, New() (DI wiring 8 групп), Run(), Shutdown() с sync.Once
   - `internal/app/dmhandler.go` — composite DMResponseHandler (placeholder, логирует события)
   - `cmd/dp-worker/main.go` — thin shell: signal.NotifyContext → config.Load → app.New → app.Run
4. Тестирование: 5 тестов health handler + 7 тестов app shutdown
5. Code review (code-reviewer) — 4 warnings исправлены:
   - W1: sync.Once для идемпотентного Shutdown
   - W2: использование входящего ctx вместо context.Background() в dmhandler
   - W4: рефакторинг main() — os.Exit(run()) для корректного выполнения defers

**Ключевые решения:**
- App struct в internal/app/ (не cmd/) — тестируемо без main package tricks
- Consumer-side interfaces (brokerCloser, kvCloser, obsShutdowner) — mock injection в тестах
- brokerSubscribeAdapter — мост между broker.MessageHandler (named type) и func(ctx, []byte) error
- Shutdown order: not-ready → broker.Close (drains in-flight) → HTTP → KV → observability
- Ready flag (atomic.Bool) вместо dependency pinging — проще для worker-сервиса
- DI wiring в 8 группах по порядку зависимостей (bottom-up)

**Файлы:**
- `cmd/dp-worker/main.go` (изменён)
- `internal/app/app.go` (новый)
- `internal/app/app_test.go` (новый)
- `internal/app/dmhandler.go` (новый)
- `internal/infra/health/handler.go` (новый)
- `internal/infra/health/handler_test.go` (новый)

**Результаты тестирования:** 31 пакет PASS (включая -race), go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-039✅ разблокирует: TASK-040 (integration test processing), TASK-041 (integration test comparison), TASK-042 (Dockerfile)
- Pending задачи с выполненными deps: TASK-040 (high), TASK-041 (medium), TASK-042 (medium), TASK-043 (high, Security), TASK-044 (high, Audit logging)
- dmResponseHandler — placeholder, нужно заменить на реальную диспетчеризацию когда orchestrators реализуют DM response методы
- warningCollector shared между jobs — потенциальная проблема при concurrent processing (S4 из review)

### TASK-040 — Integration test — processing pipeline end-to-end с mock-инфраструктурой
**Статус:** done
**Дата:** 2026-03-25
**Summary:** E2E интеграционные тесты processing pipeline. Тестовая инфраструктура + 7 тестов
покрывающих все acceptance criteria: happy path, warnings, validation rejection, retry,
idempotency, malformed input, artifact format.

**План реализации:**
1. Изучение кодовой базы (Explore agent) — Orchestrator, Consumer, Dispatcher, все port interfaces
2. Проектирование архитектуры тестов (code-architect):
   - Архитектура: captureBroker → real Consumer → real Dispatcher → real Orchestrator
   - Mock только infrastructure: broker subscribe, idempotency KV, temp storage, event publisher, DM sender
   - Stubs для engine: fetcher, OCR, text extract, structure extract, tree builder, validator
   - Build tag: `//go:build integration`
3. Реализация (golang-pro):
   - `internal/integration/testinfra.go` — 13 типов инфраструктуры, testHarness, newTestHarness(t, ...harnessOption)
   - `internal/integration/processing_pipeline_test.go` — 7 test functions
4. Code review (code-reviewer) — 2 critical + 5 warnings найдены и исправлены:
   - C1 FIXED: sync.Mutex добавлен на все stubs (stubOCR, stubTextExtract, stubStructExtract, stubTreeBuilder, stubValidator)
   - C2 FIXED: warning.Collector экспонирован на testHarness
   - W3 FIXED: newTestHarness принимает *testing.T вместо panic
   - W4 FIXED: Test 4 использует withMaxRetries(2) option вместо дублирования wiring
   - W5 FIXED: maxRetries=1 документирован комментарием
   - W7 FIXED: deliverToTopic возвращает error при отсутствии handler
   - W11 FIXED: topic string вынесен в константу testTopicProcessDocument

**Файлы:**
- `internal/integration/testinfra.go` (новый) — тестовая инфраструктура
- `internal/integration/processing_pipeline_test.go` (новый) — 7 интеграционных тестов

**Тесты:**
1. TestProcessingPipeline_HappyPath_TextPDF — полный pipeline QUEUED→IN_PROGRESS→COMPLETED
2. TestProcessingPipeline_HappyPath_WithWarnings — COMPLETED_WITH_WARNINGS
3. TestProcessingPipeline_ValidationRejected — REJECTED + ProcessingFailedEvent
4. TestProcessingPipeline_FetchError_Failed — retry exhaustion maxRetries=2
5. TestProcessingPipeline_DuplicateJob_Skipped — idempotency guard
6. TestProcessingPipeline_InvalidJSON_Acknowledged — zero side effects
7. TestProcessingPipeline_ArtifactFormat_Complete — rich structure verification

**Результаты тестирования:**
- 7 integration тестов PASS с -race
- 31 пакет PASS (go test -count=1 -race ./...)
- go vet clean (с и без integration tag)
- make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-040✅ завершена. Pending задачи: TASK-041 (integration comparison, medium), TASK-042 (Dockerfile, medium), TASK-043 (Security, high), TASK-044 (Audit, high)
- Integration тесты запускаются: `go test -tags integration -v ./internal/integration/`
- Harness поддерживает harnessOption pattern для кастомизации (withMaxRetries и т.д.)
- Warning collector shared — не использовать harness для concurrent job submissions

### TASK-043 — Валидация безопасности: file_url, SSRF, sanitization
**Статус:** done
**Дата:** 2026-03-25

**План реализации (согласован с code-architect):**
1. Добавить ErrCodeSSRFBlocked + NewSSRFBlockedError в port/errors.go
2. Создать engine/validator/ssrf.go: ValidateURLSecurity (scheme check + DNS-resolved IP check)
   - Injectable Resolver interface для тестируемости DNS lookups
   - 9 blocked CIDR ranges: 0.0.0.0/8, 127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16, ::1/128, fc00::/7, fe80::/10
3. Интегрировать SSRF check в engine/validator/validator.go (правило 3 после file_url non-empty)
4. Создать infra/httpdownloader/ssrf.go: connection-time SSRF guard через net.Dialer Control
5. Обновить infra/httpdownloader/downloader.go: SSRF DialContext + Content-Type check + redirect scheme check
6. Добавить sanitization в ingress/consumer/validate.go: sanitizeString (null bytes, %00, path traversal loop, C0/C1 control chars)
7. Вызвать sanitization в consumer.go перед validation
8. Добавить ErrCodeSSRFBlocked в rejectedCodes processing orchestrator

**Ключевые решения:**
- 2-layer SSRF: pre-download ValidateURLSecurity (validator) + connection-time ssrfControl (downloader DialContext)
- DNS failure → block (not allow): предотвращение DNS rebinding (code review fix C1)
- 0.0.0.0/8 в blocked CIDRs: Linux routes 0.0.0.0 to loopback (code review fix C2)
- Path traversal: loop until stable (handles ....// nested patterns) (code review fix W2)
- Redirect scheme check in CheckRedirect (code review fix W3)
- Content-Type: accept application/pdf + application/octet-stream, reject text/* etc., empty allowed
- newDownloader(timeout, nil) for tests (httptest.Server binds 127.0.0.1, would be blocked by ssrfControl)
- Sanitization at trust boundary (consumer layer, before validation and dispatch)

**Summary:**
- 3 новых файла: engine/validator/ssrf.go, engine/validator/ssrf_test.go, infra/httpdownloader/ssrf.go
- 8 модифицированных файлов: port/errors.go, validator.go + test, downloader.go + test, consumer validate.go + consumer.go + validate_test.go, processing orchestrator, app.go
- Code review: 2 critical fixed (C1: DNS rebinding, C2: 0.0.0.0/8), 2 warnings fixed (W2: nested path traversal, W3: redirect scheme)
- 32 пакета PASS с -race (включая integration), go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-043✅ завершена. Pending задачи: TASK-041 (integration comparison, medium), TASK-042 (Dockerfile, medium), TASK-044 (Audit, high), TASK-045 (PDF CMap, low)
- CIDR list дублирован между engine/validator и infra/httpdownloader (hexagonal constraint: engine не импортирует infra и наоборот). При изменении менять оба файла!
- Validator теперь принимает 3й параметр Resolver (nil → net.DefaultResolver в production)
- Для тестов httpdownloader используйте newDownloader(timeout, nil) — без SSRF control

---

### TASK-044 — Аудит и журналирование действий (NFR-3.4, раздел 9 ТЗ)
**Статус:** done
**Дата:** 2026-03-26
**Приоритет:** high | **Категория:** security/audit
**Зависимости:** TASK-012 (done), TASK-035 (done)

**План реализации:**
1. Расширить `JobContext` полями `OrgID` и `UserID` для полного аудит-контекста
2. Обновить `withJobAttrs()` в Logger для эмиссии `org_id`/`user_id` в structured logs
3. Установить `CorrelationID = JobID` на входе в consumer (ingress boundary)
4. Внедрить `observability.Logger` как dependency injection во все application-layer компоненты
5. Заменить все `log.Printf` на structured JSON логи через `slog`
6. Добавить stage-by-stage логирование через `WithStage()` в оба пайплайна
7. Обновить все тесты под новые конструкторы

**Summary:**
- 10 модифицированных файлов:
  - `infra/observability/context.go`: +OrgID, +UserID в JobContext
  - `infra/observability/logger.go`: emit org_id, user_id в withJobAttrs
  - `ingress/consumer/consumer.go`: +CorrelationID, OrgID, UserID в WithJobContext
  - `application/lifecycle/manager.go`: inject logger, replace log.Printf, +structured transition/cleanup/terminal logs
  - `application/processing/orchestrator.go`: inject logger, replace 4x log.Printf, +8 stage logs, +entry/completion/error logs
  - `application/comparison/orchestrator.go`: inject logger, replace 2x log.Printf, +6 stage logs, +entry/completion/error logs
  - `app/app.go`: pass obs.Logger to 3 constructors
  - `consumer/consumer_test.go`: +CorrelationID/OrgID/UserID assertions in context enrichment tests
  - `lifecycle/manager_test.go`, `processing/orchestrator_test.go`, `comparison/orchestrator_test.go`, `integration/testinfra.go`: updated constructors
- Code review fixes: W2 (removed file_url from log to prevent pre-signed URL credential leak), W3 (added CorrelationID/OrgID/UserID assertions in consumer tests)
- 31 пакет PASS с -race, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-044✅ завершена. Pending задачи: TASK-041 (integration comparison, medium), TASK-042 (Dockerfile, medium), TASK-045 (PDF CMap, low)
- `observability.Logger` теперь обязательный параметр в конструкторах LifecycleManager, ProcessingOrchestrator, ComparisonOrchestrator
- Для тестов используйте `nopLogger()` → `observability.NewLogger("error")` для подавления шума
- `handlePipelineError` использует `context.Background()` с enriched JobContext, т.к. job context может быть expired
- Warning collector не thread-safe — concurrent job processing потребует отдельного решения

### TASK-041 — Integration test — comparison pipeline end-to-end
**Статус:** done
**Дата:** 2026-03-26
**Приоритет:** medium | **Категория:** integration
**Зависимости:** TASK-039 (done)

**План реализации:**
1. Изучить существующую тестовую инфраструктуру (testinfra.go, processing_pipeline_test.go)
2. Изучить comparison pipeline orchestrator, pendingresponse registry, version comparer
3. Спроектировать тестовую инфраструктуру для comparison pipeline (code-architect)
4. Реализовать mock-типы: treeRequesterMock (синхронная доставка деревьев), confirmingDMSender (async DM confirmation)
5. Реализовать comparisonHarness с real Registry и real Comparer
6. Написать 6 интеграционных тестов
7. Провести code review (code-reviewer)

**Ключевые решения:**
- Real `pendingresponse.Registry` вместо mock — тестирует реальную координацию Register/AwaitAll/Receive
- Real `comparison.Comparer` — валидирует формат diff output end-to-end
- Синхронная доставка деревьев в `treeRequesterMock` — Register вызывается ДО RequestSemanticTree
- Async confirmation в `confirmingDMSender` через goroutine + 10ms delay — SendDiffResult ДО Register для confirmation
- Отдельный `comparisonHarness` от `testHarness` — разные зависимости пайплайнов

**Summary:**
- 2 файла: testinfra.go (расширен), comparison_pipeline_test.go (новый)
- Добавлены в testinfra.go: noopProcessingHandler, treeRequesterMock, confirmingDMSender, comparisonHarness, newComparisonHarness, default helpers (defaultCompareCommand, defaultBaseTree, defaultTargetTree, baseCorrelationID, targetCorrelationID)
- 6 тестов:
  1. HappyPath: полный pipeline → COMPLETED, 2 tree requests, diff с TextDiffs+StructuralDiffs
  2. ValidationError_SameVersionIDs: base==target → REJECTED (ErrCodeValidation)
  3. DMTreeError_VersionNotFound: DM ошибка → REJECTED (ErrCodeDMVersionNotFound)
  4. DuplicateJob_Skipped: idempotency guard
  5. InvalidJSON_Acknowledged: no side effects
  6. DiffFormat_Complete: diff format validation, event meta checks
- Все 13 integration tests pass (6 новых + 7 существующих), go test -race clean
- 32 пакета unit tests pass, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-041✅ завершена. Pending задачи: TASK-042 (Dockerfile, medium), TASK-045 (PDF CMap, low)
- Для comparison integration tests используется отдельный comparisonHarness (не testHarness)
- Деревья по умолчанию: base (1 section, 1 clause), target (1 section с измененным clause + added section-2) → гарантированно TextDiffs + StructuralDiffs

### TASK-042 — Dockerfile и конфигурация для сборки контейнера
**Статус:** done
**Дата:** 2026-03-26

**План реализации:**
1. Спроектировать Dockerfile, .env.example, .dockerignore, make target (code-architect)
2. Создать multi-stage Dockerfile: golang:1.26.1-alpine (builder) → alpine:3.21 (production)
3. Создать .env.example с документацией всех переменных окружения (из sub_configs.go)
4. Создать .dockerignore для минимизации build context
5. Добавить docker-build target в Makefile
6. Создать .gitignore для исключения бинарника dp-worker
7. Code review и применение фиксов

**Ключевые решения:**
- Alpine вместо scratch/distroless — нужен wget для HEALTHCHECK
- CGO_ENABLED=0 + -s -w -trimpath → статический бинарник ~20MB, образ ~28-32MB
- Non-root user dpworker (без home, без пароля, /sbin/nologin)
- HEALTHCHECK по /readyz (не /healthz) — для Docker Compose/Swarm readiness
- start-period=30s — время на подключение к RabbitMQ/Redis
- EXPOSE 8080 (health) + 9090 (metrics)
- IMAGE_TAG из git describe --tags --always --dirty

**Summary:**
- 5 файлов создано/обновлено: Dockerfile, .env.example, .dockerignore, .gitignore, Makefile
- Dockerfile: 2-stage build, non-root user, HEALTHCHECK /readyz, EXPOSE 8080+9090
- .env.example: 9 required (DP_BROKER_ADDRESS, DP_STORAGE_*, DP_OCR_*, DP_KVSTORE_ADDRESS) + ~20 optional с дефолтами, 15 broker topic overrides (закомментированы)
- .dockerignore: dp-worker, .env, *_test.go, .git, IDE, *.md
- Makefile: docker-build target с contractpro/dp-worker:$(IMAGE_TAG):latest dual-tagging
- .gitignore: dp-worker, .env, coverage.out/html
- Code review fixes: /readyz вместо /healthz, start-period 30s, абсолютный ENTRYPOINT, placeholder credentials
- 32 пакета unit tests pass, go vet clean, make build/test/lint OK

**Заметки для следующей итерации:**
- TASK-042✅ завершена. Единственная pending задача: TASK-045 (PDF CMap/ToUnicode, low priority)
- Docker build не был запущен в CI sandbox — требуется ручная проверка: `make docker-build` + `docker images contractpro/dp-worker`
- При переезде на Kubernetes HEALTHCHECK можно убрать (k8s использует свои probes)
