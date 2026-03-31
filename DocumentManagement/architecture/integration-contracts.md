# Интеграции и контракты Document Management

## 1. Входящие события/команды

| Топик | Событие | Источник | Обработчик в DM |
|-------|---------|----------|-----------------|
| `dp.artifacts.processing-ready` | `DocumentProcessingArtifactsReady` | DP | Artifact Ingestion Service |
| `dp.requests.semantic-tree` | `GetSemanticTreeRequest` | DP | Artifact Query Service |
| `dp.artifacts.diff-ready` | `DocumentVersionDiffReady` | DP | Diff Storage Service |
| `lic.requests.artifacts` | `GetArtifactsRequest` | LIC | Artifact Query Service |
| `lic.artifacts.analysis-ready` | `LegalAnalysisArtifactsReady` | LIC | Artifact Ingestion Service |
| `re.requests.artifacts` | `GetArtifactsRequest` | RE | Artifact Query Service |
| `re.artifacts.reports-ready` | `ReportsArtifactsReady` | RE | Artifact Ingestion Service |

## 2. Исходящие события/подтверждения

| Топик | Событие | Потребитель | Описание |
|-------|---------|-------------|----------|
| `dm.responses.artifacts-persisted` | `DocumentProcessingArtifactsPersisted` | DP | Подтверждение сохранения артефактов DP |
| `dm.responses.artifacts-persist-failed` | `DocumentProcessingArtifactsPersistFailed` | DP | Ошибка сохранения артефактов DP |
| `dm.responses.semantic-tree-provided` | `SemanticTreeProvided` | DP | Ответ на запрос semantic tree |
| `dm.responses.artifacts-provided` | `ArtifactsProvided` | LIC, RE | Ответ на запрос артефактов |
| `dm.responses.diff-persisted` | `DocumentVersionDiffPersisted` | DP | Подтверждение сохранения diff |
| `dm.responses.diff-persist-failed` | `DocumentVersionDiffPersistFailed` | DP | Ошибка сохранения diff |
| `dm.responses.lic-artifacts-persisted` | `LegalAnalysisArtifactsPersisted` | LIC | Подтверждение сохранения результатов LIC |
| `dm.responses.lic-artifacts-persist-failed` | `LegalAnalysisArtifactsPersistFailed` | LIC | Ошибка сохранения результатов LIC |
| `dm.responses.re-reports-persisted` | `ReportsArtifactsPersisted` | RE | Подтверждение сохранения отчётов |
| `dm.responses.re-reports-persist-failed` | `ReportsArtifactsPersistFailed` | RE | Ошибка сохранения отчётов |
| `dm.events.version-artifacts-ready` | `VersionProcessingArtifactsReady` | LIC | Артефакты DP готовы |
| `dm.events.version-analysis-ready` | `VersionAnalysisArtifactsReady` | RE | Результаты LIC готовы |
| `dm.events.version-reports-ready` | `VersionReportsReady` | Orchestrator | Отчёты готовы |
| `dm.events.version-created` | `VersionCreated` | Orchestrator | Новая версия создана |

## 3. Запросы/ответы (sync API)

| Method | Endpoint | Описание |
|--------|----------|----------|
| POST | `/api/v1/documents` | Создать документ |
| GET | `/api/v1/documents?page=&size=&status=` | Список документов (org_id из JWT) |
| GET | `/api/v1/documents/{document_id}` | Метаданные документа |
| POST | `/api/v1/documents/{document_id}/archive` | Архивация документа |
| DELETE | `/api/v1/documents/{document_id}` | Soft delete |
| POST | `/api/v1/documents/{document_id}/versions` | Создать версию |
| GET | `/api/v1/documents/{document_id}/versions` | Список версий |
| GET | `/api/v1/documents/{document_id}/versions/{version_id}` | Метаданные версии |
| GET | `/api/v1/documents/{document_id}/versions/{version_id}/artifacts` | Список артефактов версии |
| GET | `/api/v1/documents/{document_id}/versions/{version_id}/artifacts/{type}` | Получить артефакт (JSON / signed URL) |
| GET | `/api/v1/documents/{document_id}/diffs/{base_vid}/{target_vid}` | Получить diff |

**Обоснование sync API:** Sync REST API используется только оркестратором и UI. Для CRUD-операций и отображения данных пользователю async неприемлем — пользователь ожидает немедленный ответ (NFR-1.3 ≤ 2 сек). Междоменное взаимодействие (DP, LIC, RE) — исключительно async через RabbitMQ.

## 4. Именование топиков RabbitMQ

Стандарт: `{домен}.{тип}.{действие}`.

| Prefix | Тип | Примеры |
|--------|-----|---------|
| `dm.responses.*` | Подтверждения для отправителей | `dm.responses.artifacts-persisted` |
| `dm.events.*` | Уведомления для нижестоящих | `dm.events.version-artifacts-ready` |
| `dp.artifacts.*` | Артефакты от DP | `dp.artifacts.processing-ready` |
| `dp.requests.*` | Запросы от DP | `dp.requests.semantic-tree` |
| `lic.requests.*` | Запросы от LIC | `lic.requests.artifacts` |
| `lic.artifacts.*` | Артефакты от LIC | `lic.artifacts.analysis-ready` |
| `re.requests.*` | Запросы от RE | `re.requests.artifacts` |
| `re.artifacts.*` | Артефакты от RE | `re.artifacts.reports-ready` |
| `dm.dlq.*` | Dead letter queue | `dm.dlq.ingestion-failed` |

## 5. Минимальный envelope сообщений

Совместимость с DP — все события содержат `EventMeta`:

```json
{
  "correlation_id": "uuid-v4",
  "timestamp": "2026-03-29T12:00:00Z",
  "job_id": "uuid-v4",
  "document_id": "uuid-v4",
  ...event-specific fields
}
```

`correlation_id` и `timestamp` — из `EventMeta` (embedded). `job_id` и `document_id` — event-specific, но присутствуют в каждом событии.

## 6. Поля корреляции и трассировки

| Поле | Описание | Где используется |
|------|----------|-----------------|
| `correlation_id` | Сквозной ID бизнес-операции | Все события, логи, audit, tracing |
| `job_id` | ID задачи обработки/сравнения | Привязка артефактов к задаче, idempotency |
| `document_id` | ID документа | Все операции |
| `version_id` | ID версии | Привязка артефактов, запросы semantic tree |
| `organization_id` | ID организации (tenant) | Фильтрация, tenant isolation |
| `requested_by_user_id` | Кто инициировал | Audit trail |

Все поля **пробрасываются** из входящего события в исходящие подтверждения/уведомления.

## 7. Versioning сообщений и backward compatibility

- Каждый тип артефакта в `ArtifactDescriptor` несёт `schema_version`.
- Новые поля добавляются как optional с дефолтами → backward compatible.
- При breaking change — новый `schema_version`, DM поддерживает чтение обеих версий в transition period.
- Имена топиков стабильны; при необходимости — новый топик с суффиксом версии.
