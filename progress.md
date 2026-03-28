# Document Processing — Progress Log

## TASK-053: ArtifactsPersistFailed/DiffPersistFailed — добавить поле error_code
**Дата:** 2026-03-28
**Статус:** ЗАВЕРШЁН

### План реализации
1. Изучение кода: event.go, receiver.go, validate.go, dmhandler.go, тесты
2. Проектирование (code-architect): omitempty, опциональность, порядок полей
3. Добавить ErrorCode string `json:"error_code,omitempty"` в обе структуры
4. Обновить логирование в receiver.go и dmhandler.go
5. Обновить тесты: event_test.go, receiver_test.go, validate_test.go
6. Code review + исправление warnings

### Подход
- ErrorCode — опциональное поле для обратной совместимости со старыми версиями DM
- omitempty в JSON-теге, чтобы не сериализовать пустую строку
- Поле перед ErrorMessage — по аналогии с ProcessingFailedEvent/ComparisonFailedEvent
- Валидация НЕ требует error_code (опциональное)
- ErrorCode — диагностическая метаинформация для логов, не для DomainError

### Изменённые файлы
- `internal/domain/model/event.go` — ErrorCode field в обоих PersistFailed structs
- `internal/egress/dm/receiver.go` — логирование error_code в 2 handlers
- `internal/app/dmhandler.go` — логирование error_code + is_retryable fix
- `internal/domain/model/event_test.go` — 6 новых тестов
- `internal/egress/dm/receiver_test.go` — ErrorCode в хелперах + assertion
- `internal/egress/dm/validate_test.go` — 2 новых теста на опциональность

### Результаты
- 32 пакета PASS с -race
- go vet clean
- make build/test/lint OK
- Code review: 0 critical, 2 warnings (1 fixed, 1 by-design)

---

## TASK-052: DocumentVersionDiffReady — добавить поля text_diff_count и structural_diff_count
**Дата:** 2026-03-28
**Статус:** ЗАВЕРШЁН

### План реализации
1. Изучение кода: event.go, orchestrator.go, sender.go, тесты
2. Добавить TextDiffCount int и StructuralDiffCount int в DocumentVersionDiffReady
3. Заполнить поля в Comparison Orchestrator через len()
4. Обновить тесты: event_test.go, sender_test.go, orchestrator_test.go
5. Code review + git commit

### Подход
- Аналогия с ComparisonCompletedEvent, которая уже содержит TextDiffCount и StructuralDiffCount
- Денормализация для удобства потребителей: count-поля позволяют проверить количество различий без парсинга массивов
- sender.go не требует изменений — json.Marshal автоматически сериализует новые поля

### Изменённые файлы
- `internal/domain/model/event.go` — добавлены поля TextDiffCount, StructuralDiffCount
- `internal/application/comparison/orchestrator.go` — заполнение count-полей
- `internal/domain/model/event_test.go` — обновлён round-trip тест
- `internal/egress/dm/sender_test.go` — обновлён хелпер и тесты формата/round-trip
- `internal/application/comparison/orchestrator_test.go` — проверки count-полей в diffEvent

### Результаты
- 32 пакета PASS с -race
- go vet clean
- make build/test/lint OK
- Code review: 0 critical, 0 warnings

---

## TASK-051: Processing Pipeline — выделить стадию VALIDATING_FILE после FETCHING_SOURCE_FILE
**Дата:** 2026-03-28
**Статус:** ЗАВЕРШЁН

### План реализации
1. Изучение кода: orchestrator.go, fetcher.go, stage.go, engine.go
2. Проектирование (code-architect): выбор между split port interface и error-code reclassification
3. Реализация error-code based stage reclassification в orchestrator
4. Тесты на корректность failed_at_stage
5. Code review + исправление warnings
6. Обновление tasks.json, git commit

### Подход
- **Error-code based stage reclassification** — ошибки файловой валидации (FILE_TOO_LARGE, INVALID_FORMAT, TOO_MANY_PAGES) реклассифицируются из FETCHING_SOURCE_FILE в VALIDATING_FILE в оркестраторе
- Без изменений порта SourceFileFetcherPort (остаётся single-method Fetch)
- Без изменений Fetcher (engine layer)
- Паттерн аналогичен существующему rejectedCodes

### Изменённые файлы
- `internal/application/processing/orchestrator.go` — fileValidationCodes map, isFileValidationError(), reclassification в runPipeline()
- `internal/application/processing/orchestrator_test.go` — 8 новых тестов

### Результаты
- 33 пакета PASS с -race
- go vet clean
- make build/test/lint OK
- Code review: 0 critical, 3 warnings адресованы
