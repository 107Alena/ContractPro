# Интеграции и контракты API/Backend Orchestrator

Описание интеграционных контрактов **API/Backend Orchestrator** — единственной точки входа для frontend-приложений и внешних интеграций платформы **ContractPro**.

Документ описывает:
- синхронные вызовы оркестратора к доменным сервисам (DM, OPM, UOM, Object Storage);
- сводку асинхронных интеграций (команды и события) со ссылкой на [event-catalog.md](event-catalog.md) для полных JSON-схем;
- корреляционные поля и схему пробрасывания;
- именование очередей RabbitMQ для оркестратора.

> **Ключевое ограничение:** Оркестратор — единственный потребитель sync API Document Management (ASSUMPTION-15 из DM). Frontend и внешние системы не обращаются к DM, DP, OPM, UOM напрямую.

---

# 1. Синхронные вызовы (Orchestrator → доменные сервисы)

## 1.1 Document Management (DM) — REST API

Оркестратор является **единственным потребителем** DM sync REST API (ASSUMPTION-15). Все вызовы проксируются или агрегируются для frontend.

### Заголовки запросов

Каждый запрос к DM содержит набор заголовков для аутентификации, tenant isolation и трассировки:

| Заголовок | Формат | Описание |
|-----------|--------|----------|
| `Authorization` | `Bearer <JWT>` | JWT-токен. С точки зрения DM — доверенный контекст от оркестратора |
| `X-Organization-ID` | UUID | ID организации (tenant isolation). Извлекается из JWT claim `org` |
| `X-User-ID` | UUID | ID пользователя. Извлекается из JWT claim `sub` |
| `X-Correlation-ID` | UUID | Сквозной ID операции для трассировки. Генерируется оркестратором при приёме запроса от frontend |

> DM валидирует `X-Organization-ID` и `X-User-ID` regex-паттерном `^[a-zA-Z0-9._-]{1,128}$` и фильтрует все данные по `organization_id` (tenant isolation).

### 1.1.1 Документы (CRUD)

#### POST /api/v1/documents — создание документа

**Когда вызывается:** Contract Upload Coordinator при загрузке нового договора.

**Request:**

```json
{
  "title": "string (обязательное)"
}
```

**Response (201 Created):**

```json
{
  "document_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "title": "string",
  "status": "ACTIVE",
  "created_at": "string (ISO 8601)",
  "updated_at": "string (ISO 8601)",
  "created_by_user_id": "string (UUID)"
}
```

**Ошибки:**

| HTTP | error_code | Описание |
|------|------------|----------|
| 400 | `INVALID_PAYLOAD` | Невалидный JSON |
| 400 | `VALIDATION_ERROR` | `title` не указан |

---

#### GET /api/v1/documents — список документов

**Когда вызывается:** Results Aggregator при отображении списка договоров.

**Query parameters:**

| Параметр | Тип | Описание |
|----------|-----|----------|
| `page` | int | Номер страницы (default: 1) |
| `size` | int | Размер страницы (default: 20) |
| `status` | string | Фильтр по статусу: `ACTIVE`, `ARCHIVED`, `DELETED` |

> Фильтрация по `organization_id` применяется автоматически по заголовку `X-Organization-ID`.

**Response (200 OK):**

```json
{
  "items": [
    {
      "document_id": "string (UUID)",
      "organization_id": "string (UUID)",
      "title": "string",
      "status": "string (ACTIVE | ARCHIVED | DELETED)",
      "created_at": "string (ISO 8601)",
      "updated_at": "string (ISO 8601)",
      "created_by_user_id": "string (UUID)"
    }
  ],
  "total": 42,
  "page": 1,
  "size": 20
}
```

---

#### GET /api/v1/documents/{document_id} — получение документа

**Когда вызывается:** Results Aggregator при отображении договора с текущей версией.

**Response (200 OK):** Документ с текущей (последней) версией. Поля документа аналогичны ответу `POST /documents`, дополнительно включают `current_version` (если есть).

**Ошибки:**

| HTTP | error_code | Описание |
|------|------------|----------|
| 404 | `NOT_FOUND` | Документ не найден или принадлежит другой организации |

---

#### DELETE /api/v1/documents/{document_id} — soft delete

**Когда вызывается:** При удалении договора пользователем.

**Response (200 OK):** Обновлённый документ со статусом `DELETED`.

**Ошибки:**

| HTTP | error_code | Описание |
|------|------------|----------|
| 404 | `NOT_FOUND` | Документ не найден |
| 409 | `CONFLICT` | Документ уже удалён или архивирован |

---

#### POST /api/v1/documents/{document_id}/archive — архивация

**Когда вызывается:** При архивации договора пользователем.

**Response (200 OK):** Обновлённый документ со статусом `ARCHIVED`.

**Ошибки:**

| HTTP | error_code | Описание |
|------|------------|----------|
| 404 | `NOT_FOUND` | Документ не найден |
| 409 | `CONFLICT` | Документ уже архивирован или удалён |

---

### 1.1.2 Версии документов

#### POST /api/v1/documents/{document_id}/versions — создание версии

**Когда вызывается:**
- Contract Upload Coordinator — при загрузке нового файла (`origin_type=UPLOAD` или `RE_UPLOAD`).
- Re-check Coordinator — при повторной проверке (`origin_type=RE_CHECK`).

**Request:**

```json
{
  "source_file_key": "string (обязательное, ключ файла в Object Storage)",
  "source_file_name": "string (обязательное, имя файла)",
  "source_file_size": 1048576,
  "source_file_checksum": "string (SHA-256 hex)",
  "origin_type": "string (UPLOAD | RE_UPLOAD | RECOMMENDATION_APPLIED | MANUAL_EDIT | RE_CHECK)",
  "origin_description": "string (optional, описание причины создания версии)",
  "parent_version_id": "string (UUID, optional, ID родительской версии)"
}
```

> **ASSUMPTION-4 (DM):** Файл ДОЛЖЕН быть загружен в Object Storage ДО создания версии. DM валидирует наличие файла через HEAD-запрос к S3 по `source_file_key`.

**Response (201 Created):**

```json
{
  "version_id": "string (UUID)",
  "document_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "version_number": 1,
  "artifact_status": "PENDING",
  "origin_type": "string (UPLOAD | RE_UPLOAD | RE_CHECK | ...)",
  "origin_description": "string (optional)",
  "parent_version_id": "string (UUID, optional)",
  "source_file_key": "string",
  "source_file_name": "string",
  "source_file_size": 1048576,
  "source_file_checksum": "string",
  "created_at": "string (ISO 8601)",
  "created_by_user_id": "string (UUID)"
}
```

**Ошибки:**

| HTTP | error_code | Описание |
|------|------------|----------|
| 400 | `INVALID_PAYLOAD` | Невалидный JSON |
| 400 | `VALIDATION_ERROR` | Невалидный `origin_type` или отсутствуют обязательные поля |
| 404 | `NOT_FOUND` | Документ не найден |
| 409 | `CONFLICT` | Документ архивирован/удалён |

---

#### GET /api/v1/documents/{document_id}/versions — список версий

**Когда вызывается:** Results Aggregator при отображении истории версий.

**Response (200 OK):** Массив версий документа, отсортированных по `version_number`.

---

#### GET /api/v1/documents/{document_id}/versions/{version_id} — получение версии

**Когда вызывается:**
- Results Aggregator — метаданные версии, включая `artifact_status`.
- Re-check Coordinator — получение `source_file_key` для создания RE_CHECK-версии.
- Comparison Coordinator — валидация существования версий перед сравнением.

**Response (200 OK):**

```json
{
  "version_id": "string (UUID)",
  "document_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "version_number": 1,
  "artifact_status": "string (PENDING | PROCESSING_ARTIFACTS_RECEIVED | ANALYSIS_ARTIFACTS_RECEIVED | REPORTS_READY | FULLY_READY | PARTIALLY_AVAILABLE)",
  "origin_type": "string",
  "parent_version_id": "string (UUID, optional)",
  "source_file_key": "string",
  "source_file_name": "string",
  "source_file_size": 1048576,
  "source_file_checksum": "string",
  "created_at": "string (ISO 8601)",
  "created_by_user_id": "string (UUID)"
}
```

> **Маппинг `artifact_status` → user status** выполняется оркестратором (см. [high-architecture.md](high-architecture.md), раздел 5.2).

---

### 1.1.3 Артефакты

#### GET /api/v1/documents/{document_id}/versions/{version_id}/artifacts — список артефактов

**Когда вызывается:** Results Aggregator для определения доступных артефактов.

**Response (200 OK):** Массив дескрипторов артефактов с метаданными (тип, размер, schema_version, created_at).

---

#### GET /api/v1/documents/{document_id}/versions/{version_id}/artifacts/{type} — получение артефакта

**Когда вызывается:** Results Aggregator для загрузки конкретного артефакта. Export Service для экспортных артефактов.

**Параметр `{type}`** — тип артефакта:

| Тип артефакта | Источник | Описание |
|---------------|----------|----------|
| `EXTRACTED_TEXT` | DP | Извлечённый нормализованный текст |
| `DOCUMENT_STRUCTURE` | DP | Логическая структура документа |
| `SEMANTIC_TREE` | DP | Семантическое дерево |
| `OCR_RAW` | DP | Сырой результат OCR |
| `PROCESSING_WARNINGS` | DP | Предупреждения обработки |
| `CLASSIFICATION_RESULT` | LIC | Тип договора |
| `KEY_PARAMETERS` | LIC | Ключевые параметры (стороны, предмет, цена) |
| `RISK_ANALYSIS` | LIC | Список рисков |
| `RISK_PROFILE` | LIC | Агрегированный профиль рисков |
| `RECOMMENDATIONS` | LIC | Рекомендации по формулировкам |
| `SUMMARY` | LIC | Краткое резюме для нетехнических пользователей |
| `DETAILED_REPORT` | LIC | Детальный отчёт |
| `AGGREGATE_SCORE` | LIC | Сводная оценка |
| `EXPORT_PDF` | RE | Экспортный отчёт PDF |
| `EXPORT_DOCX` | RE | Экспортный отчёт DOCX |

**Response:**
- **Для JSON-артефактов (metadata types):** `200 OK` с JSON-содержимым артефакта.
- **Для blob-артефактов (EXPORT_PDF, EXPORT_DOCX):** `302 Found` с заголовком `Location` → presigned S3 URL.

**Ошибки:**

| HTTP | error_code | Описание |
|------|------------|----------|
| 404 | `NOT_FOUND` | Артефакт не найден (ещё не создан или версия не существует) |

---

### 1.1.4 Сравнение версий (diff)

#### GET /api/v1/documents/{document_id}/diffs/{base_vid}/{target_vid} — получение diff

**Когда вызывается:** Results Aggregator при отображении результатов сравнения.

**Response (200 OK):**

```json
{
  "document_id": "string (UUID)",
  "base_version_id": "string (UUID)",
  "target_version_id": "string (UUID)",
  "text_diffs": [
    {
      "type": "string (added | removed | modified)",
      "path": "string",
      "old_text": "string | null",
      "new_text": "string | null"
    }
  ],
  "structural_diffs": [
    {
      "type": "string (added | removed | modified | moved)",
      "node_id": "string",
      "old_value": "object | null",
      "new_value": "object | null"
    }
  ],
  "text_diff_count": 5,
  "structural_diff_count": 3
}
```

**Ошибки:**

| HTTP | error_code | Описание |
|------|------------|----------|
| 404 | `NOT_FOUND` | Diff не найден (не запускался или ещё не готов) |

---

### 1.1.5 Аудит

#### GET /api/v1/audit — записи аудита

**Когда вызывается:** Admin Proxy Service для аудиторского журнала.

**Query parameters:** `page`, `size`, `document_id`, `action`, `actor_id`.

**Response (200 OK):**

```json
{
  "items": [
    {
      "audit_id": "string (UUID)",
      "organization_id": "string (UUID)",
      "document_id": "string (UUID, optional)",
      "version_id": "string (UUID, optional)",
      "action": "string (DOCUMENT_CREATED | VERSION_CREATED | DOCUMENT_ARCHIVED | DOCUMENT_DELETED | ...)",
      "actor_type": "string (USER | SYSTEM | DOMAIN)",
      "actor_id": "string",
      "job_id": "string (UUID, optional)",
      "correlation_id": "string (UUID, optional)",
      "details": "object (optional, JSON)",
      "created_at": "string (ISO 8601)"
    }
  ],
  "total": 100,
  "page": 1,
  "size": 20
}
```

---

### 1.1.6 Таймауты и retry-политика DM Client

| Параметр | Значение | Описание |
|----------|----------|----------|
| Read timeout | 5 сек | GET-запросы (метаданные, артефакты, списки) |
| Write timeout | 10 сек | POST/PUT/DELETE (создание, архивация, удаление) |
| Retry attempts | 3 | При транзиентных ошибках (5xx, timeout) |
| Retry backoff | Exponential (100ms, 200ms, 400ms) | Задержка между retry |
| Circuit breaker | Open after 5 consecutive failures, half-open after 30s | Защита от каскадных отказов |

---

## 1.2 Organization Policy Management (OPM) — REST API

> **ASSUMPTION-ORCH-04:** OPM ещё не спроектирован. Описанный контракт — минимальный набор, необходимый оркестратору для проксирования административных запросов (UR-12, роль R-3).

Оркестратор проксирует запросы `ORG_ADMIN` в OPM. Заголовки аналогичны DM: `Authorization`, `X-Organization-ID`, `X-User-ID`, `X-Correlation-ID`.

### Эндпоинты

#### GET /api/v1/policies?organization_id={org_id} — список политик

**Когда вызывается:** Admin Proxy Service при отображении настроек строгости.

**Response (200 OK):**

```json
{
  "policies": [
    {
      "policy_id": "string (UUID)",
      "organization_id": "string (UUID)",
      "name": "string",
      "description": "string",
      "strictness_level": "string (strict | moderate | lenient)",
      "rules": ["object"],
      "created_at": "string (ISO 8601)",
      "updated_at": "string (ISO 8601)"
    }
  ]
}
```

---

#### PUT /api/v1/policies/{policy_id} — обновление политики

**Когда вызывается:** Admin Proxy Service при изменении настроек строгости.

**Request:**

```json
{
  "name": "string (optional)",
  "description": "string (optional)",
  "strictness_level": "string (strict | moderate | lenient)",
  "rules": ["object (optional)"]
}
```

**Response (200 OK):** Обновлённая политика.

---

#### GET /api/v1/checklists?organization_id={org_id} — список чек-листов

**Response (200 OK):**

```json
{
  "checklists": [
    {
      "checklist_id": "string (UUID)",
      "organization_id": "string (UUID)",
      "name": "string",
      "items": [
        {
          "item_id": "string",
          "label": "string",
          "is_mandatory": true
        }
      ],
      "created_at": "string (ISO 8601)",
      "updated_at": "string (ISO 8601)"
    }
  ]
}
```

---

#### PUT /api/v1/checklists/{checklist_id} — обновление чек-листа

**Request:**

```json
{
  "name": "string (optional)",
  "items": [
    {
      "item_id": "string",
      "label": "string",
      "is_mandatory": true
    }
  ]
}
```

**Response (200 OK):** Обновлённый чек-лист.

### Обработка недоступности OPM

При недоступности OPM (timeout, 5xx) — оркестратор возвращает HTTP 502 с сообщением `"Сервис настроек временно недоступен"`. OPM — **опциональная зависимость**: при её недоступности основные сценарии (загрузка, просмотр результатов) продолжают работать.

---

## 1.3 User & Organization Management (UOM) — REST API

> **ASSUMPTION-ORCH-03:** UOM ещё не спроектирован. Описанный контракт — минимальный набор для аутентификации и получения профиля.

### Эндпоинты

#### POST /api/v1/auth/login — аутентификация

**Когда вызывается:** Проксирование login-запроса от frontend.

**Request:**

```json
{
  "email": "string",
  "password": "string"
}
```

**Response (200 OK):**

```json
{
  "access_token": "string (JWT, TTL 15 мин)",
  "refresh_token": "string (opaque, TTL 30 дней)",
  "token_type": "Bearer",
  "expires_in": 900,
  "user": {
    "user_id": "string (UUID)",
    "organization_id": "string (UUID)",
    "email": "string",
    "name": "string",
    "role": "string (LAWYER | BUSINESS_USER | ORG_ADMIN)"
  }
}
```

**JWT claims (access token):**

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

**Ошибки:**

| HTTP | error_code | Описание |
|------|------------|----------|
| 401 | `INVALID_CREDENTIALS` | Неверный email или пароль |

---

#### POST /api/v1/auth/refresh — обновление токена

**Request:**

```json
{
  "refresh_token": "string"
}
```

**Response (200 OK):**

```json
{
  "access_token": "string (JWT, новый)",
  "refresh_token": "string (новый, ротация)",
  "token_type": "Bearer",
  "expires_in": 900
}
```

**Ошибки:**

| HTTP | error_code | Описание |
|------|------------|----------|
| 401 | `TOKEN_EXPIRED` | Refresh token истёк |
| 401 | `TOKEN_REVOKED` | Refresh token отозван (logout, блокировка) |

---

#### POST /api/v1/auth/logout — выход

**Request:**

```json
{
  "refresh_token": "string"
}
```

**Response (204 No Content).**

> UOM инвалидирует refresh token. Access token продолжает работать до истечения `exp` (до 15 мин). Это компромисс stateless JWT (ADR-5).

---

#### GET /api/v1/users/me — профиль текущего пользователя

**Заголовок:** `Authorization: Bearer <access_token>`.

**Response (200 OK):**

```json
{
  "user_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "email": "string",
  "name": "string",
  "role": "string (LAWYER | BUSINESS_USER | ORG_ADMIN)",
  "created_at": "string (ISO 8601)"
}
```

### Обработка недоступности UOM

UOM — **критическая зависимость** для login/refresh/logout. При недоступности UOM:
- `POST /auth/login`, `POST /auth/refresh`, `POST /auth/logout` → HTTP 503.
- JWT-валидация выполняется **локально** по публичному ключу (RSA/ECDSA) без обращения к UOM — запросы с валидным access token продолжают работать.
- `GET /users/me` — кэшируется в Redis (TTL 5 мин). При недоступности UOM и cache miss → HTTP 503.

---

## 1.4 Object Storage (Yandex Object Storage, S3-compatible)

### PutObject — загрузка исходного файла

**Когда вызывается:** Contract Upload Coordinator при загрузке договора.

| Параметр | Значение |
|----------|----------|
| Bucket | Общий с DM (ASSUMPTION-ORCH-05) |
| Key | `uploads/{organization_id}/{document_id}/{uuid}/{filename}` |
| Content-Type | `application/pdf` (из MIME-валидации) |
| Content-Length | Из multipart header |
| Upload method | Streaming (не буферизация в память) |
| Max file size | 20 МБ |
| Retry | 3 попытки, exponential backoff |

**Результат:** `storage_key` — передаётся в DM при создании версии.

### DeleteObject — очистка при ошибке

**Когда вызывается:** При ошибке на любом шаге после upload (ошибка DM, ошибка RabbitMQ publish).

| Параметр | Значение |
|----------|----------|
| Key | `storage_key` из PutObject |
| Retry | 1 попытка. При неудаче — логирование WARN (orphan file будет удалён DM orphan cleanup job) |

---

# 2. Асинхронные интеграции (RabbitMQ)

Полные JSON-схемы всех событий, описания полей и реакции оркестратора — см. [event-catalog.md](event-catalog.md).

## 2.1 Исходящие команды (Orchestrator → DP / LIC)

| Топик | Событие | Потребитель | Публикуется |
|-------|---------|-------------|-------------|
| `dp.commands.process-document` | `ProcessDocumentRequested` | DP | Contract Upload Coordinator, Re-check Coordinator |
| `dp.commands.compare-versions` | `CompareDocumentVersionsRequested` | DP | Comparison Coordinator |
| `orch.commands.user-confirmed-type` | `UserConfirmedType` | LIC | Type Confirmation Handler (POST /confirm-type) |

**Гарантии публикации:**

| Параметр | Значение |
|----------|----------|
| Publisher confirms | Включены (mandatory + confirm mode) |
| Retry | 3 попытки при ошибке publish |
| Retry backoff | Exponential (100ms, 200ms, 400ms) |
| Поведение при исчерпании retry | Версия уже создана в DM (статус `PENDING`). Логирование CRITICAL. HTTP 202 возвращается пользователю. Оператор может переотправить команду вручную |

## 2.2 Входящие события (DP/LIC/RE/DM → Orchestrator)

| # | Топик | Событие | Источник | Назначение в оркестраторе |
|---|-------|---------|----------|--------------------------|
| 1 | `dp.events.status-changed` | `StatusChangedEvent` | DP | Маппинг DP-статусов → SSE push |
| 2 | `dp.events.processing-completed` | `ProcessingCompletedEvent` | DP | Логирование, метрики (финальный статус) |
| 3 | `dp.events.processing-failed` | `ProcessingFailedEvent` | DP | SSE push FAILED/REJECTED, метрики |
| 4 | `dp.events.comparison-completed` | `ComparisonCompletedEvent` | DP | SSE push, уведомление о готовности diff |
| 5 | `dp.events.comparison-failed` | `ComparisonFailedEvent` | DP | SSE push ошибки сравнения |
| 6 | `lic.events.status-changed` | `LICStatusChangedEvent` | LIC | SSE push: прогресс/ошибка анализа (ASSUMPTION-ORCH-13) |
| 7 | `lic.events.classification-uncertain` | `ClassificationUncertain` | LIC | Перевод версии в `AWAITING_USER_INPUT` + SSE push `type_confirmation_required` (FR-2.1.3); запуск watchdog `ORCH_USER_CONFIRMATION_TIMEOUT` |
| 8 | `re.events.status-changed` | `REStatusChangedEvent` | RE | SSE push: прогресс/ошибка отчётов (ASSUMPTION-ORCH-13) |
| 9 | `dm.events.version-created` | `VersionCreated` | DM | Информирование о создании версии |
| 10 | `dm.events.version-artifacts-ready` | `VersionProcessingArtifactsReady` | DM | SSE push: ANALYZING |
| 11 | `dm.events.version-analysis-ready` | `VersionAnalysisArtifactsReady` | DM | SSE push: GENERATING_REPORTS |
| 12 | `dm.events.version-reports-ready` | `VersionReportsReady` | DM | SSE push: READY (финальный) |
| 13 | `dm.events.version-partially-available` | `VersionPartiallyAvailable` | DM | SSE push: PARTIALLY_FAILED (safety net — DM Watchdog) |

---

# 3. Корреляция и трассировка

## 3.1 Корреляционные поля

| Поле | Тип | Описание | Генерируется | Присутствует в |
|------|-----|----------|-------------|---------------|
| `correlation_id` | UUID v4 | Сквозной ID бизнес-операции (от HTTP-запроса до завершения pipeline) | Оркестратор (при приёме HTTP-запроса) | Все события, команды, sync-заголовки, логи, audit |
| `job_id` | UUID v4 | ID задачи обработки/сравнения в DP | Оркестратор (перед публикацией команды в DP) | Команды, события DP, привязка артефактов |
| `document_id` | UUID v4 | ID документа | DM (при создании документа) | Все операции с документом |
| `version_id` | UUID v4 | ID версии документа | DM (при создании версии) | Операции с версией, артефакты, события |
| `organization_id` | UUID | ID организации (tenant) | UOM (из JWT claim `org`) | Все запросы (tenant isolation), события |
| `requested_by_user_id` | UUID | ID пользователя, инициировавшего операцию | UOM (из JWT claim `sub`) | Команды, audit trail |

## 3.2 Схема пробрасывания полей

```
Frontend HTTP-запрос
    │
    ▼ Оркестратор генерирует: correlation_id, job_id
    │ Извлекает из JWT: organization_id, requested_by_user_id
    │
    ├── Sync (DM): X-Correlation-ID, X-Organization-ID, X-User-ID (заголовки)
    ├── Sync (OPM): X-Correlation-ID, X-Organization-ID, X-User-ID (заголовки)
    ├── Sync (UOM): X-Correlation-ID (заголовок)
    └── Async (DP): correlation_id, job_id, document_id, version_id,
                     organization_id, requested_by_user_id (поля сообщения)
```

**Входящие события от DP/DM:** Содержат `correlation_id`, `job_id`, `document_id`. Оркестратор использует эти поля для:
1. Маршрутизации SSE-события к нужной организации (`organization_id`).
2. Логирования со сквозным `correlation_id`.
3. Метрик по `job_id`.

## 3.3 Пример сквозной трассировки

```
1. Frontend → POST /api/v1/contracts/upload
   Оркестратор генерирует: correlation_id=COR-001, job_id=JOB-001

2. Orchestrator → DM: POST /documents (X-Correlation-ID: COR-001)
   DM возвращает: document_id=DOC-001

3. Orchestrator → DM: POST /documents/DOC-001/versions (X-Correlation-ID: COR-001)
   DM возвращает: version_id=VER-001

4. Orchestrator → RabbitMQ: ProcessDocumentRequested
   {correlation_id: COR-001, job_id: JOB-001, document_id: DOC-001, version_id: VER-001}

5. DP → RabbitMQ: StatusChangedEvent
   {correlation_id: COR-001, job_id: JOB-001, document_id: DOC-001, new_status: IN_PROGRESS}
   → Orchestrator → SSE: {document_id: DOC-001, status: PROCESSING}

6. DM → RabbitMQ: VersionProcessingArtifactsReady
   {correlation_id: COR-001, document_id: DOC-001, version_id: VER-001}
   → Orchestrator → SSE: {document_id: DOC-001, status: ANALYZING}

7. DM → RabbitMQ: VersionReportsReady
   {correlation_id: COR-001, document_id: DOC-001, version_id: VER-001}
   → Orchestrator → SSE: {document_id: DOC-001, status: READY}
```

---

# 4. Очереди RabbitMQ для оркестратора

## 4.1 Именование очередей подписки

Оркестратор создаёт **собственные очереди** для каждого топика подписки:

```
orch.sub.{topic_name}
```

| Очередь | Binding (topic) |
|---------|-----------------|
| `orch.sub.dp.events.status-changed` | `dp.events.status-changed` |
| `orch.sub.dp.events.processing-completed` | `dp.events.processing-completed` |
| `orch.sub.dp.events.processing-failed` | `dp.events.processing-failed` |
| `orch.sub.dp.events.comparison-completed` | `dp.events.comparison-completed` |
| `orch.sub.dp.events.comparison-failed` | `dp.events.comparison-failed` |
| `orch.sub.lic.events.status-changed` | `lic.events.status-changed` |
| `orch.sub.lic.events.classification-uncertain` | `lic.events.classification-uncertain` |
| `orch.sub.re.events.status-changed` | `re.events.status-changed` |
| `orch.sub.dm.events.version-created` | `dm.events.version-created` |
| `orch.sub.dm.events.version-artifacts-ready` | `dm.events.version-artifacts-ready` |
| `orch.sub.dm.events.version-analysis-ready` | `dm.events.version-analysis-ready` |
| `orch.sub.dm.events.version-reports-ready` | `dm.events.version-reports-ready` |
| `orch.sub.dm.events.version-partially-available` | `dm.events.version-partially-available` |

## 4.2 Обоснование отдельных очередей

RabbitMQ поддерживает множество потребителей через отдельные queue bindings к одному exchange/routing key. Оркестратор **не конкурирует** с другими потребителями (LIC, RE) за те же сообщения:

- `dm.events.version-artifacts-ready` → LIC подписан через `lic.sub.dm.events.version-artifacts-ready`, оркестратор — через `orch.sub.dm.events.version-artifacts-ready`. Каждый получает копию.
- `dm.events.version-analysis-ready` → RE подписан через свою очередь, оркестратор — через свою.

## 4.3 Параметры очереди

| Параметр | Значение |
|----------|----------|
| Durable | `true` (переживает рестарт брокера) |
| Auto-delete | `false` |
| Exclusive | `false` (несколько инстансов оркестратора разделяют одну очередь) |
| Prefetch count | 10 (конфигурируемо) |
| ACK mode | Manual (после успешной обработки) |

## 4.4 Обработка ошибок при получении событий

| Ситуация | Действие |
|----------|----------|
| Невалидный JSON | Логирование WARN + ACK (событие отбрасывается). Не блокирует очередь |
| Отсутствуют обязательные поля | Логирование WARN + ACK |
| Ошибка обработки (транзиентная) | NACK + requeue. Retry до 3 раз (по header `x-retry-count`). После исчерпания — ACK + логирование ERROR |
| Ошибка обработки (нетранзиентная) | ACK + логирование ERROR |
| SSE broadcast failed | ACK (событие обработано, но не доставлено клиенту). Клиент получит актуальный статус при polling fallback |

---

# 5. Диаграмма интеграций

```
                        Frontend / External API
                                │
                       HTTPS (REST + SSE)
                                │
           ┌────────────────────┴─────────────────────┐
           │          API/Backend Orchestrator          │
           │                                           │
           │  Publishes (async):                       │
           │  ┌─────────────────────────────────────┐  │
           │  │ dp.commands.process-document        │  │
           │  │ dp.commands.compare-versions        │  │
           │  │ orch.commands.user-confirmed-type   │  │
           │  └─────────────────────────────────────┘  │
           │                                           │
           │  Subscribes (async):                      │
           │  ┌─────────────────────────────────────┐  │
           │  │ dp.events.* (5 events)              │  │
           │  │ lic.events.* (2 events)             │  │
           │  │ re.events.* (1 event)               │  │
           │  │ dm.events.* (5 events)              │  │
           │  └─────────────────────────────────────┘  │
           │                                           │
           │  Sync REST calls:                         │
           │  ┌──────────┬──────────┬──────────────┐   │
           │  │ DM API   │ OPM API │ UOM API      │   │
           │  │ (CRUD,   │ (policy │ (login,      │   │
           │  │ artifacts,│ proxy)  │ refresh,     │   │
           │  │ diff)    │         │ profile)     │   │
           │  └──────────┴──────────┴──────────────┘   │
           │                                           │
           │  S3 API:                                  │
           │  ┌──────────────────────────────────────┐ │
           │  │ Object Storage (PutObject, Delete)   │ │
           │  └──────────────────────────────────────┘ │
           └───────────────────────────────────────────┘
                    │           │           │
          ┌────────┘    ┌──────┘    ┌──────┘
          ▼             ▼           ▼
    ┌──────────┐  ┌──────────┐  ┌──────────────┐
    │    DM    │  │    DP    │  │ Object       │
    │ (stateful)│  │(stateless)│  │ Storage (S3) │
    └──────────┘  └──────────┘  └──────────────┘
```

---

# 6. Связанные документы

| Документ | Описание |
|----------|----------|
| [event-catalog.md](event-catalog.md) | Полные JSON-схемы всех событий оркестратора (исходящие команды, входящие события, DLQ) |
| [high-architecture.md](high-architecture.md) | Верхнеуровневая архитектура оркестратора (компоненты, сценарии, ADR, маппинг статусов) |
| [api-specification.yaml](api-specification.yaml) | OpenAPI 3.0 спецификация frontend-facing API оркестратора |
| [DM integration-contracts.md](../../DocumentManagement/architecture/integration-contracts.md) | Интеграции и контракты Document Management |
| [DM event-catalog.md](../../DocumentManagement/architecture/event-catalog.md) | Полный каталог событий DM (JSON-схемы) |
| [DM state-machine.md](../../DocumentManagement/architecture/state-machine.md) | State machine `artifact_status` версии документа |
| [DP high-architecture.md](../../DocumentProcessing/architecture/high-architecture.md) | Архитектура Document Processing (pipeline, стадии, статусы) |
