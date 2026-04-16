# Безопасность, доступ и аудит API/Backend Orchestrator

В рамках документа описана модель безопасности компонента **API/Backend Orchestrator** платформы **ContractPro**. Оркестратор является единственной точкой входа для frontend и внешних интеграций (ASSUMPTION-ORCH-01) и реализует стратегию **defense in depth**: каждый уровень обработки запроса применяет собственные проверки.

**Цепочка защиты при каждом HTTP-запросе:**

```
Request → TLS (reverse proxy) → CORS → JWT Auth → RBAC → Rate Limiter
    → Input Validation → Handler → Tenant-scoped downstream call
```

**Ссылки:**
- [high-architecture.md](high-architecture.md) -- компонентная архитектура, ADR-5 (JWT), матрица доступа
- [DM security.md](../../DocumentManagement/architecture/security.md) -- безопасность Document Management (tenant isolation, audit trail, шифрование)

---

## 1. Аутентификация

### 1.1 Стратегия

Оркестратор использует **JWT (JSON Web Tokens)** для stateless-аутентификации (ADR-5 из high-architecture.md). Access token валидируется локально по публичному ключу без обращения к UOM на каждый запрос (ASSUMPTION-ORCH-02).

### 1.2 JWT-токен

**Формат:** JWS (JSON Web Signature), алгоритм подписи RS256 (RSA) или ES256 (ECDSA).

**Claims (минимальный набор):**

```json
{
  "sub": "550e8400-e29b-41d4-a716-446655440000",
  "org": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "role": "LAWYER",
  "exp": 1712400000,
  "iat": 1712399100,
  "jti": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

| Claim | Тип | Описание |
|-------|-----|----------|
| `sub` | UUID | `user_id` -- идентификатор пользователя |
| `org` | UUID | `organization_id` -- идентификатор организации |
| `role` | string enum | Роль: `LAWYER`, `BUSINESS_USER`, `ORG_ADMIN` |
| `exp` | Unix timestamp | Время истечения токена |
| `iat` | Unix timestamp | Время выдачи токена |
| `jti` | UUID | Уникальный идентификатор токена (для отзыва и аудита) |

### 1.3 Валидация подписи

Публичный ключ для проверки подписи JWT загружается одним из двух способов (в порядке приоритета):

1. **Переменная окружения** `ORCH_JWT_PUBLIC_KEY` -- PEM-encoded RSA/ECDSA public key. Подходит для development и простых deployment-сценариев.
2. **JWKS endpoint от UOM** -- `ORCH_JWT_JWKS_URL` (например, `https://uom-service/api/v1/.well-known/jwks.json`). Ключи кэшируются в памяти с TTL 1 час и обновляются при ошибке валидации (key rotation support).

**Проверки при валидации JWT:**

1. Подпись корректна (RSA-SHA256 или ECDSA-SHA256).
2. `exp` > текущее время (с допуском clock skew 30 секунд).
3. `iat` <= текущее время.
4. `sub` -- валидный UUID.
5. `org` -- валидный UUID.
6. `role` -- одно из значений `LAWYER`, `BUSINESS_USER`, `ORG_ADMIN`.
7. `jti` -- присутствует (используется для аудита; blacklist для отозванных токенов -- опционально, через Redis).

### 1.4 Время жизни токенов

| Тип токена | TTL | Хранение | Отзыв |
|------------|-----|----------|-------|
| Access token | 15 минут | Клиент (memory / httpOnly cookie) | По истечении `exp`. Blacklist через Redis -- опционально (ORCH-R-5). |
| Refresh token | 30 дней | UOM (PostgreSQL) | UOM поддерживает отзыв: `POST /auth/logout`, блокировка пользователя. |

Short-lived access token (15 мин) минимизирует окно уязвимости при компрометации. Refresh token с отзывом в UOM -- механизм для logout и блокировки.

### 1.5 Потоки аутентификации

**Login (получение JWT-пары):**

```
Frontend                    Orchestrator                 UOM
   │                            │                          │
   │ POST /api/v1/auth/login    │                          │
   │ {email, password}          │                          │
   │ ──────────────────────────►│                          │
   │                            │ POST /auth/login         │
   │                            │ {email, password}        │
   │                            │ ─────────────────────────►
   │                            │                          │
   │                            │ 200 {access_token,       │
   │                            │      refresh_token}      │
   │                            │ ◄─────────────────────────
   │ 200 {access_token,         │                          │
   │      refresh_token}        │                          │
   │ ◄──────────────────────────│                          │
```

**Refresh (обновление access token):**

```
Frontend                    Orchestrator                 UOM
   │                            │                          │
   │ POST /api/v1/auth/refresh  │                          │
   │ {refresh_token}            │                          │
   │ ──────────────────────────►│                          │
   │                            │ POST /auth/refresh       │
   │                            │ {refresh_token}          │
   │                            │ ─────────────────────────►
   │                            │                          │
   │                            │ 200 {access_token}       │
   │                            │ ◄─────────────────────────
   │ 200 {access_token}         │                          │
   │ ◄──────────────────────────│                          │
```

**Ошибки аутентификации:**

| Ситуация | HTTP-статус | Тело ответа |
|----------|-------------|-------------|
| Отсутствует заголовок Authorization | 401 | `{"error_code": "AUTH_TOKEN_MISSING", "message": "Требуется аутентификация"}` |
| Истёк access token (`exp` в прошлом) | 401 | `{"error_code": "AUTH_TOKEN_EXPIRED", "message": "Срок действия токена истёк"}` |
| Невалидная подпись / повреждённый токен / отсутствуют обязательные claims | 401 | `{"error_code": "AUTH_TOKEN_INVALID", "message": "Недействительный токен авторизации"}` |
| Невалидный refresh token | 401 | `{"error_code": "AUTH_TOKEN_INVALID", "message": "Недействительный токен обновления"}` |
| UOM недоступен (login/refresh) | 502 | `{"error_code": "UOM_UNAVAILABLE", "message": "Сервис аутентификации временно недоступен"}` |

### 1.6 Публичные endpoints (без JWT)

Следующие endpoints **не требуют** JWT-аутентификации:

| Endpoint | Назначение |
|----------|------------|
| `POST /api/v1/auth/login` | Получение JWT-пары |
| `POST /api/v1/auth/refresh` | Обновление access token |
| `GET /healthz` | Liveness probe |
| `GET /readyz` | Readiness probe |
| `GET /metrics` | Prometheus metrics |

Все остальные endpoints требуют валидный JWT в заголовке `Authorization: Bearer <token>`.

### 1.7 SSE-аутентификация

Стандартный `EventSource` API браузера не поддерживает кастомные HTTP-заголовки. Для аутентификации SSE-соединений используется JWT в query-параметре:

```
GET /api/v1/events/stream?token=eyJhbGciOiJSUzI1NiIs...
```

**Меры безопасности при передаче JWT в URL:**

1. JWT в URL **не логируется** -- middleware для structured logging исключает query-параметр `token` из записи.
2. Reverse proxy (nginx/envoy) настроен на исключение `token` из access log.
3. Access token short-lived (15 мин) -- минимизирует риск при утечке из логов/истории.
4. SSE Connection Manager валидирует JWT аналогично middleware (подпись, exp, claims).

---

## 2. Авторизация (RBAC)

### 2.1 Ролевая модель

Система использует **Role-Based Access Control** с тремя ролями:

| Роль | Идентификатор | Описание |
|------|---------------|----------|
| Юрист | `LAWYER` (R-1) | Полный доступ к результатам анализа, экспорту, сравнению. Основной пользователь системы. |
| Бизнес-пользователь | `BUSINESS_USER` (R-2) | Доступ к загрузке и краткому резюме. Детальные результаты анализа недоступны. |
| Администратор организации | `ORG_ADMIN` (R-3) | Все права R-1 + управление политиками и настройками организации. |

Роль извлекается из claim `role` JWT-токена. Назначение и изменение ролей -- ответственность UOM.

### 2.2 Матрица доступа

| Endpoint | Метод | LAWYER | BUSINESS_USER | ORG_ADMIN |
|----------|-------|--------|---------------|-----------|
| `/contracts/upload` | POST | Допуск | Допуск | Допуск |
| `/contracts` | GET | Допуск | Допуск | Допуск |
| `/contracts/{id}` | GET | Допуск | Допуск | Допуск |
| `/contracts/{id}/versions/{vid}/results` | GET | Допуск | Запрет | Допуск |
| `/contracts/{id}/versions/{vid}/summary` | GET | Допуск | Допуск | Допуск |
| `/contracts/{id}/versions/{vid}/export` | POST | Допуск | По политике OPM | Допуск |
| `/contracts/{id}/versions/{vid}/compare` | POST | Допуск | Запрет | Допуск |
| `/contracts/{id}/versions/{vid}/recheck` | POST | Допуск | Запрет | Допуск |
| `/contracts/{id}/versions/{vid}/confirm-type` | POST | Допуск | Запрет | Допуск |
| `/contracts/{id}/versions/{vid}/feedback` | POST | Допуск | Допуск | Допуск |
| `/contracts/{id}/archive` | POST | Допуск | Запрет | Допуск |
| `/contracts/{id}` | DELETE | Допуск | Запрет | Допуск |
| `/admin/policies` | GET/PUT | Запрет | Запрет | Допуск |
| `/admin/checklists` | GET/PUT | Запрет | Запрет | Допуск |
| `/events/stream` (SSE) | GET | Допуск | Допуск | Допуск |

### 2.3 Реализация RBAC middleware

RBAC middleware выполняется **после** JWT Auth middleware (claims уже извлечены и помещены в Go context).

**Логика:**

1. Из Go context извлекается `role`.
2. По маршруту (route pattern) определяется минимально требуемый набор ролей.
3. Если `role` не входит в допустимый набор -- возвращается HTTP 403.

**Конфигурация правил доступа (в коде):**

```go
var accessRules = map[string][]string{
    "POST /api/v1/contracts/upload":                         {"LAWYER", "BUSINESS_USER", "ORG_ADMIN"},
    "GET  /api/v1/contracts":                                {"LAWYER", "BUSINESS_USER", "ORG_ADMIN"},
    "GET  /api/v1/contracts/{id}/versions/{vid}/results":    {"LAWYER", "ORG_ADMIN"},
    "GET  /api/v1/contracts/{id}/versions/{vid}/summary":    {"LAWYER", "BUSINESS_USER", "ORG_ADMIN"},
    "POST /api/v1/contracts/{id}/versions/{vid}/compare":      {"LAWYER", "ORG_ADMIN"},
    "POST /api/v1/contracts/{id}/versions/{vid}/recheck":      {"LAWYER", "ORG_ADMIN"},
    "POST /api/v1/contracts/{id}/versions/{vid}/confirm-type": {"LAWYER", "ORG_ADMIN"},
    "POST /api/v1/contracts/{id}/archive":                     {"LAWYER", "ORG_ADMIN"},
    "DELETE /api/v1/contracts/{id}":                          {"LAWYER", "ORG_ADMIN"},
    "GET  /api/v1/admin/policies":                           {"ORG_ADMIN"},
    "PUT  /api/v1/admin/policies/{id}":                      {"ORG_ADMIN"},
}
```

**Ответ при недостаточных правах:**

```json
{
  "error_code": "FORBIDDEN",
  "message": "Недостаточно прав для выполнения операции"
}
```

HTTP-статус: **403 Forbidden**.

### 2.4 Фильтрация данных по роли

Помимо доступа к endpoints, оркестратор фильтрует **содержимое ответов** по роли:

| Роль | Доступные артефакты из DM |
|------|--------------------------|
| `LAWYER` | Все: RISK_ANALYSIS, RISK_PROFILE, SUMMARY, RECOMMENDATIONS, KEY_PARAMETERS, CLASSIFICATION_RESULT, AGGREGATE_SCORE, EXPORT_PDF, EXPORT_DOCX |
| `BUSINESS_USER` | Только: SUMMARY, AGGREGATE_SCORE |
| `ORG_ADMIN` | Все (аналогично LAWYER) |

Results Aggregator проверяет роль из auth context и запрашивает из DM только разрешённые типы артефактов.

Дополнительно для BUSINESS_USER: доступ к экспорту (`EXPORT_PDF`/`EXPORT_DOCX`) определяется computed-флагом `permissions.export_enabled` в `UserProfile` (см. high-architecture.md §6.21 Permissions Resolver). Флаг агрегируется из политики OPM `business_user_export` с fallback на `ORCH_OPM_FALLBACK_BUSINESS_USER_EXPORT` (default `false`). Frontend читает флаг из `GET /users/me`, не дёргает OPM напрямую.

---

## 3. Multi-tenancy enforcement

### 3.1 Принцип

Оркестратор реализует принцип **Tenant-first** (принцип 5 из high-architecture.md): `organization_id` из JWT -- обязательный контекст в каждом downstream-вызове. Этот идентификатор **никогда не принимается из тела запроса или URL** -- только из верифицированного JWT-токена.

### 3.2 Механизмы изоляции

| Уровень | Механизм |
|---------|----------|
| HTTP-вход | `organization_id` извлекается из JWT claim `org` и помещается в Go context. |
| Downstream REST-вызовы (DM, OPM, UOM) | Заголовок `X-Organization-ID: {organization_id}` добавляется ко всем исходящим HTTP-запросам. DM валидирует, что запрашиваемый ресурс принадлежит указанной организации. |
| Async-команды (RabbitMQ → DP) | Поле `organization_id` включается в тело каждой команды (`ProcessDocumentRequested`, `CompareDocumentVersionsRequested`). |
| SSE-каналы | Redis Pub/Sub channel scoped по организации: `sse:broadcast:{organization_id}`. Пользователь получает события только своей организации. |
| Rate limiting | Счётчики привязаны к `organization_id` (не к `user_id`). Ограничение применяется на уровне организации. |
| Audit logging | Каждая запись аудита содержит `organization_id`. |
| Upload path | Object Storage prefix: `uploads/{organization_id}/{document_id}/{uuid}`. |

### 3.3 Защита от подмены

Оркестратор не доверяет `organization_id` из пользовательского ввода:

- **Request body:** Любое поле `organization_id` в теле запроса **игнорируется**. Используется только значение из JWT.
- **URL path:** Маршруты не содержат `organization_id` -- tenant isolation прозрачен для frontend.
- **Query parameters:** `organization_id` из query-параметров **игнорируется**.

**Пример: загрузка файла**

Даже если злоумышленник отправит `organization_id` в теле запроса, оркестратор использует значение из JWT:

```
POST /api/v1/contracts/upload
Authorization: Bearer <token с org=ORG_A>

→ Object Storage key: uploads/ORG_A/...
→ DM: POST /documents с X-Organization-ID: ORG_A
→ DP command: organization_id=ORG_A
```

---

## 4. Rate limiting

### 4.1 Стратегия

Rate limiting реализован per-organization через **token bucket** алгоритм. Состояние хранится в Redis.

### 4.2 Лимиты

| Класс операции | HTTP-методы | Лимит по умолчанию | Env-переменная |
|----------------|-------------|--------------------:|----------------|
| Read (чтение) | GET | 200 RPS | `ORCH_RATE_LIMIT_READ_RPS` |
| Write (запись) | POST, PUT, DELETE | 50 RPS | `ORCH_RATE_LIMIT_WRITE_RPS` |
| Upload (загрузка файлов) | POST `/contracts/upload` | 10 concurrent | `ORCH_RATE_LIMIT_UPLOAD_CONCURRENT` |

### 4.3 Реализация

**Redis keys:**

```
rl:{organization_id}:read     -- counter, TTL 1s (sliding window)
rl:{organization_id}:write    -- counter, TTL 1s (sliding window)
rl:{organization_id}:upload   -- counter текущих загрузок (инкремент при начале, декремент при завершении)
```

**Ответ при превышении лимита:**

```
HTTP/1.1 429 Too Many Requests
Retry-After: 1
Content-Type: application/json

{
  "error_code": "RATE_LIMIT_EXCEEDED",
  "message": "Превышен лимит запросов. Повторите через 1 секунду."
}
```

Заголовок `Retry-After` указывает количество секунд до восстановления доступности.

### 4.4 Upload concurrency limiter

Для endpoint загрузки файлов (`POST /api/v1/contracts/upload`) применяется отдельный лимит на количество **одновременных** загрузок (не RPS). Это защищает от исчерпания ресурсов при параллельных крупных загрузках:

1. При начале загрузки -- `INCR rl:{org_id}:upload`. Если значение > 10 -- отклонение с 429.
2. При завершении загрузки (успех или ошибка) -- `DECR rl:{org_id}:upload`.
3. TTL ключа: 120s (защита от zombie-записей при крахе процесса).

---

## 5. CORS (Cross-Origin Resource Sharing)

> **Архитектурное решение (ADR-6, high-architecture.md):** v1 production deployment — **same-origin** (единый nginx раздаёт SPA-статику и проксирует `/api/*` на Orchestrator). CORS-middleware **не активируется** в production. Конфигурация ниже — заготовка на случай:
> - будущего разделения доменов (cross-origin deployment),
> - внешних кросс-доменных интеграций (ЭДО, CRM — публичный API),
> - не-стандартных dev-окружений.

### 5.1 Конфигурация

| Параметр | Значение по умолчанию | Env-переменная |
|----------|----------------------|----------------|
| Allowed Origins | Same-origin only (пусто = CORS не активируется) | `ORCH_CORS_ALLOWED_ORIGINS` (comma-separated) |
| Allowed Methods | `GET, POST, PUT, DELETE, OPTIONS` | -- |
| Allowed Headers | `Authorization, Content-Type, X-Correlation-Id, traceparent, tracestate` | -- |
| Exposed Headers | `X-Request-Id, Retry-After, traceparent` | -- |
| Max-Age (preflight cache) | 3600 секунд (1 час) | -- |
| Allow Credentials | `true` | -- |

**Канонический case заголовков:** `X-Correlation-Id`, `X-Request-Id` (mixed case). HTTP-спецификация case-insensitive, но строгие прокси/WAF могут отличаться — фиксируем единую форму, совпадающую с frontend-кодом (`Frontend §7.2`).

**Заголовки `traceparent` / `tracestate`** — W3C Trace Context (RFC 9110). Frontend OpenTelemetry-инструментация (`@opentelemetry/instrumentation-fetch`, см. Frontend §14.3) автоматически инжектит их в каждый запрос. При cross-origin без них в Allow-Headers preflight будет блокировать **все** запросы.

### 5.2 Правила

1. **Production v1 (same-origin) — `ORCH_CORS_ALLOWED_ORIGINS` пустой.** CORS middleware пропускает запросы без CORS-обработки. nginx-proxy единый для SPA и API (см. Frontend §13.2 nginx.conf).
2. **Production cross-origin (будущее) — `ORCH_CORS_ALLOWED_ORIGINS=https://app.contractpro.ru`** — список явных доменов frontend-приложения.
3. **Development cross-origin — `ORCH_CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:5173`** — для случаев, когда Vite proxy не используется.
4. **Wildcard `*` запрещён** (credentials mode несовместим с `*`).

### 5.3 Preflight-запросы

Для `OPTIONS`-запросов middleware возвращает ответ сразу, не передавая запрос дальше по цепочке (только если `ORCH_CORS_ALLOWED_ORIGINS` непустой):

```
OPTIONS /api/v1/contracts
Origin: https://app.contractpro.ru
Access-Control-Request-Method: POST
Access-Control-Request-Headers: authorization, content-type, x-correlation-id, traceparent

→ 204 No Content
  Access-Control-Allow-Origin: https://app.contractpro.ru
  Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
  Access-Control-Allow-Headers: Authorization, Content-Type, X-Correlation-Id, traceparent, tracestate
  Access-Control-Expose-Headers: X-Request-Id, Retry-After, traceparent
  Access-Control-Allow-Credentials: true
  Access-Control-Max-Age: 3600
  Vary: Origin
```

---

## 6. Валидация загрузки файлов

### 6.1 Параметры валидации

| Параметр | Значение | Env-переменная |
|----------|----------|----------------|
| Максимальный размер файла | 20 МБ | `ORCH_UPLOAD_MAX_SIZE` |
| Разрешённые MIME-типы | `application/pdf` (v1) | `ORCH_UPLOAD_ALLOWED_MIME_TYPES` |
| Timeout загрузки | 60 секунд | `ORCH_UPLOAD_TIMEOUT` |
| Максимальный размер request body (не upload) | 1 МБ | `ORCH_REQUEST_BODY_MAX_SIZE` |

### 6.2 Цепочка валидации

Валидация выполняется в следующем порядке:

1. **Размер файла.** Проверяется через `Content-Length` заголовок (до чтения тела) и через streaming counter (во время чтения). При превышении 20 МБ -- немедленное прерывание с HTTP 400:
   ```json
   {"error_code": "FILE_TOO_LARGE", "message": "Файл превышает максимальный размер 20 МБ"}
   ```

2. **MIME-тип.** Проверяется значение Content-Type из multipart part. Должен быть `application/pdf`. При несоответствии -- HTTP 400:
   ```json
   {"error_code": "UNSUPPORTED_FORMAT", "message": "Поддерживается только формат PDF"}
   ```

3. **Magic bytes.** Первые 5 байт файла проверяются на соответствие PDF-сигнатуре `%PDF-` (hex: `25 50 44 46 2D`). Это защита от подмены MIME-типа -- злоумышленник может отправить исполняемый файл с Content-Type `application/pdf`. При несоответствии -- HTTP 400:
   ```json
   {"error_code": "INVALID_FILE_CONTENT", "message": "Содержимое файла не соответствует формату PDF"}
   ```

4. **Sanitization имени файла:**
   - Удаление path-traversal последовательностей (`../`, `..\\`).
   - Удаление управляющих символов (C0/C1 control characters).
   - Удаление null bytes (`\x00`).
   - Ограничение длины имени файла: 255 символов.
   - Сохранение оригинального имени только для отображения; для хранения используется UUID.

5. **Проверка на исполняемый контент.** Файл не должен содержать PE-header (`MZ`), ELF-header (`\x7fELF`), или shebang (`#!`) в первых байтах. Реализуется как часть magic bytes проверки.

### 6.3 Streaming upload

Файл **не буферизуется в память** целиком. Используется streaming:

1. `multipart.Reader` читает файл chunk по chunk.
2. Каждый chunk передаётся в Object Storage через S3 Multipart Upload или PutObject с io.Reader.
3. Параллельно вычисляется SHA-256 checksum (через `io.TeeReader`).
4. При ошибке на любом этапе -- прерывание upload, cleanup в Object Storage.

Это защищает оркестратор от DoS через загрузку файлов, занимающих всю доступную память.

---

## 7. Защита от SSRF

Оркестратор **не принимает URL от пользователей** для последующей загрузки. Все внешние вызовы направлены к заранее сконфигурированным сервисам:

| Сервис | Источник адреса | Env-переменная |
|--------|----------------|----------------|
| Document Management | Конфигурация | `ORCH_DM_BASE_URL` |
| Organization Policy Management | Конфигурация | `ORCH_OPM_BASE_URL` |
| User & Organization Management | Конфигурация | `ORCH_UOM_BASE_URL` |
| Yandex Object Storage | Конфигурация | `ORCH_STORAGE_ENDPOINT` |
| RabbitMQ | Конфигурация | `ORCH_BROKER_ADDRESS` |
| Redis | Конфигурация | `ORCH_REDIS_ADDRESS` |

Дополнительно:

- HTTP-клиенты для DM/OPM/UOM **не следуют** за HTTP redirects на адреса вне сконфигурированного base URL.
- DNS rebinding: HTTP-клиенты используют фиксированные DNS-resolved адреса при старте (для k8s -- ClusterIP services).

---

## 8. Валидация входных данных и sanitization

### 8.1 Общие правила

| Правило | Реализация |
|---------|------------|
| Строковые поля | Trimmed (пробелы по краям удалены), ограничение длины per field |
| UUID-поля | Строгая проверка формата UUID v4 (regex `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`) |
| Pagination | `page` >= 1, `size` от 1 до 100 (default: 20) |
| Enum-поля | Whitelist допустимых значений |
| Request body size | Max 1 МБ для JSON endpoints (кроме upload) |

### 8.2 Защита по типам уязвимостей

| Уязвимость | Применимость к оркестратору | Мера защиты |
|------------|---------------------------|-------------|
| SQL Injection | Не применимо | Оркестратор не имеет собственной БД. Все запросы к DM -- через HTTP API. |
| XSS | Минимальный риск | Response `Content-Type: application/json` (никогда `text/html`). Заголовок `X-Content-Type-Options: nosniff`. |
| CSRF | Низкий риск (Bearer JWT) | JWT передаётся в заголовке Authorization, а не в cookie. Browser не отправляет его автоматически при cross-origin запросах. |
| Path Traversal | Не применимо | Оркестратор не читает файлы по путям из пользовательского ввода. Имя загруженного файла sanitized и не используется для хранения. |
| Request Smuggling | На уровне reverse proxy | Reverse proxy (nginx/envoy) нормализует HTTP-запросы. Оркестратор использует `net/http` из Go stdlib, корректно обрабатывающий `Content-Length` и `Transfer-Encoding`. |

### 8.3 Валидация конкретных endpoints

**Upload (`POST /api/v1/contracts/upload`):**

| Поле | Тип | Ограничения |
|------|-----|-------------|
| `file` | binary (multipart) | PDF, <= 20 МБ, magic bytes `%PDF-` |
| `title` | string | Обязательное, 1--500 символов, trimmed |

**Compare (`POST /api/v1/contracts/{id}/versions/{vid}/compare`):**

| Поле | Тип | Ограничения |
|------|-----|-------------|
| `base_version_id` | UUID | Обязательное, формат UUID v4 |
| `target_version_id` | UUID | Обязательное, формат UUID v4, != base_version_id |

**Feedback (`POST /api/v1/contracts/{id}/versions/{vid}/feedback`):**

| Поле | Тип | Ограничения |
|------|-----|-------------|
| `is_useful` | boolean | Обязательное |
| `comment` | string | Опциональное, 0--2000 символов, trimmed |

**List contracts (`GET /api/v1/contracts`):**

| Параметр | Тип | Ограничения |
|----------|-----|-------------|
| `page` | int | >= 1, default 1 |
| `size` | int | 1--100, default 20 |
| `status` | string | Опциональный, whitelist: `UPLOADED`, `QUEUED`, `PROCESSING`, `ANALYZING`, `AWAITING_USER_INPUT`, `GENERATING_REPORTS`, `READY`, `PARTIALLY_FAILED`, `FAILED`, `REJECTED` |
| `search` | string | Опциональный, 1--200 символов, trimmed |

---

## 9. TLS и шифрование транспорта

### 9.1 Внешний трафик

TLS termination выполняется на уровне reverse proxy / load balancer (NFR-3.1). Оркестратор принимает HTTP-трафик от reverse proxy по внутренней сети.

```
Frontend ──(HTTPS/TLS 1.2+)──► Reverse Proxy ──(HTTP)──► Orchestrator
```

**Требования к reverse proxy:**

| Параметр | Значение |
|----------|----------|
| Минимальная версия TLS | 1.2 |
| Рекомендуемая версия TLS | 1.3 |
| Cipher suites | Только AEAD (AES-GCM, ChaCha20-Poly1305) |
| HSTS | `Strict-Transport-Security: max-age=31536000; includeSubDomains` |
| Сертификат | Let's Encrypt или managed certificate (Yandex Certificate Manager) |

### 9.2 Внутренний трафик

| Соединение | Протокол | TLS | Env-переменная для TLS |
|------------|----------|-----|----------------------|
| Orchestrator → DM | HTTP/HTTPS | Конфигурируемо | `ORCH_DM_TLS_ENABLED` |
| Orchestrator → OPM | HTTP/HTTPS | Конфигурируемо | `ORCH_OPM_TLS_ENABLED` |
| Orchestrator → UOM | HTTP/HTTPS | Конфигурируемо | `ORCH_UOM_TLS_ENABLED` |
| Orchestrator → RabbitMQ | AMQP/AMQPS | Конфигурируемо | `ORCH_BROKER_TLS_ENABLED` |
| Orchestrator → Redis | Redis/TLS | Конфигурируемо | `ORCH_REDIS_TLS_ENABLED` |
| Orchestrator → Object Storage | HTTPS | Всегда | -- (S3 endpoint всегда HTTPS) |

В production-среде рекомендуется включить TLS для всех внутренних соединений.

### 9.3 Шифрование данных at rest

Оркестратор **не хранит** persistent данных (ASSUMPTION-ORCH-07). Шифрование at rest -- ответственность доменных сервисов:

| Хранилище | Шифрование | Ответственный |
|-----------|-----------|---------------|
| PostgreSQL (документы, пользователи) | dm-crypt / managed DB encryption | DM, UOM |
| Object Storage (файлы, артефакты) | SSE-S3 / SSE-KMS | DM (конфигурация bucket) |
| Redis (ephemeral state) | Не содержит чувствительных бизнес-данных. Чувствительные поля (JWT в SSE registry) не хранятся. | Orchestrator |

---

## 10. Аудит действий пользователей

### 10.1 Аудируемые операции

| Действие | Описание | Trigger |
|----------|----------|---------|
| `AUTH_LOGIN` | Вход в систему | `POST /auth/login` (успешный) |
| `AUTH_LOGIN_FAILED` | Неудачная попытка входа | `POST /auth/login` (неуспешный) |
| `AUTH_REFRESH` | Обновление токена | `POST /auth/refresh` |
| `CONTRACT_UPLOAD` | Загрузка договора | `POST /contracts/upload` |
| `CONTRACT_VIEW` | Просмотр договора | `GET /contracts/{id}` |
| `CONTRACT_LIST` | Просмотр списка договоров | `GET /contracts` |
| `RESULTS_VIEW` | Просмотр результатов анализа | `GET /contracts/{id}/versions/{vid}/results` |
| `SUMMARY_VIEW` | Просмотр резюме | `GET /contracts/{id}/versions/{vid}/summary` |
| `REPORT_EXPORT` | Экспорт отчёта | `POST /contracts/{id}/versions/{vid}/export` |
| `VERSION_COMPARE` | Запуск сравнения версий | `POST /contracts/{id}/versions/{vid}/compare` |
| `VERSION_RECHECK` | Запуск повторной проверки | `POST /contracts/{id}/versions/{vid}/recheck` |
| `FEEDBACK_SUBMIT` | Отправка обратной связи | `POST /contracts/{id}/versions/{vid}/feedback` |
| `CONTRACT_ARCHIVE` | Архивация договора | `POST /contracts/{id}/archive` |
| `CONTRACT_DELETE` | Удаление договора | `DELETE /contracts/{id}` |
| `POLICY_VIEW` | Просмотр политик | `GET /admin/policies` |
| `POLICY_UPDATE` | Изменение политик | `PUT /admin/policies/{id}` |
| `CHECKLIST_VIEW` | Просмотр чек-листов | `GET /admin/checklists` |
| `CHECKLIST_UPDATE` | Изменение чек-листов | `PUT /admin/checklists/{id}` |

### 10.2 Формат записи аудита

Записи аудита -- structured JSON logs, отправляемые в систему агрегации логов (ELK / Grafana Loki):

```json
{
  "timestamp": "2026-04-06T14:23:17.456Z",
  "level": "AUDIT",
  "action": "CONTRACT_UPLOAD",
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "organization_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "resource_type": "contract",
  "resource_id": "d4e5f6a7-b8c9-0123-4567-89abcdef0123",
  "correlation_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "ip_address": "203.0.113.42",
  "user_agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ...",
  "details": {
    "file_name": "contract_2026.pdf",
    "file_size": 5242880,
    "version_id": "e5f6a7b8-c9d0-1234-5678-9abcdef01234"
  },
  "result": "SUCCESS",
  "duration_ms": 1234
}
```

### 10.3 Поля записи аудита

| Поле | Тип | Описание |
|------|-----|----------|
| `timestamp` | ISO 8601 | Время события (UTC) |
| `level` | string | Всегда `AUDIT` (отделение от application logs) |
| `action` | string | Код действия (из таблицы 10.1) |
| `user_id` | UUID | Из JWT claim `sub`. Для публичных endpoints -- `null`. |
| `organization_id` | UUID | Из JWT claim `org`. Для публичных endpoints -- `null`. |
| `resource_type` | string | Тип ресурса: `contract`, `version`, `policy`, `checklist`, `auth` |
| `resource_id` | UUID / null | Идентификатор ресурса (если применимо) |
| `correlation_id` | UUID | Из заголовка `X-Correlation-Id` или сгенерированный |
| `ip_address` | string | IP-адрес клиента (из `X-Forwarded-For` или `RemoteAddr`) |
| `user_agent` | string | Из заголовка `User-Agent` |
| `details` | object / null | Дополнительные данные, специфичные для действия |
| `result` | string | `SUCCESS` или `FAILURE` |
| `duration_ms` | int | Длительность обработки запроса в миллисекундах |

### 10.4 Хранение и retention

| Параметр | Значение |
|----------|----------|
| Хранение | Structured logs в системе агрегации (ELK / Grafana Loki) |
| Retention | 3 года (NFR-3.5), конфигурируемо на уровне log aggregation system |
| Immutability | Логи -- append-only в aggregation system. Оркестратор не предоставляет API для удаления аудита. |
| Доступ | Через Kibana / Grafana dashboards. Фильтрация по `organization_id`, `user_id`, `action`, `timestamp`. |

### 10.5 Неудачные попытки входа

Неудачные попытки входа (`AUTH_LOGIN_FAILED`) логируются с `ip_address` и `user_agent`, но **без** email/пароля. Это позволяет обнаруживать brute-force атаки через мониторинг:

- Alert при > 10 неудачных попыток с одного IP за 5 минут.
- Alert при > 5 неудачных попыток для одного email за 5 минут (через UOM -- оркестратор не знает email).

---

## 11. Security headers

Оркестратор добавляет следующие HTTP-заголовки ко всем ответам:

| Заголовок | Значение | Назначение |
|-----------|----------|------------|
| `X-Content-Type-Options` | `nosniff` | Запрет MIME-type sniffing браузером. Предотвращает интерпретацию JSON как HTML. |
| `X-Frame-Options` | `DENY` | Запрет встраивания в iframe. Защита от clickjacking. |
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` | HSTS: браузер обращается только по HTTPS. Устанавливается reverse proxy, но оркестратор дублирует для defense in depth. |
| `Cache-Control` | `no-store` | Для всех endpoints с чувствительными данными (результаты анализа, списки договоров, admin). Исключения: `/healthz`, `/readyz`, `/metrics`. |
| `X-Request-Id` | `{correlation_id}` | Идентификатор запроса для корреляции с логами. Если клиент передал `X-Correlation-Id` -- используется он; иначе генерируется UUID v4. |
| `Content-Type` | `application/json; charset=utf-8` | Все ответы оркестратора -- JSON (кроме SSE: `text/event-stream` и metrics: `text/plain`). |

**Заголовки, НЕ устанавливаемые оркестратором** (ответственность reverse proxy):

| Заголовок | Причина |
|-----------|---------|
| `Content-Security-Policy` | Оркестратор не отдаёт HTML. CSP -- ответственность frontend. |
| `Referrer-Policy` | Релевантен для браузерной навигации; API-ответы не содержат ссылок. |

---

## 12. Сводная таблица конфигурации безопасности

Все параметры безопасности конфигурируются через переменные окружения с prefix `ORCH_`:

| Переменная | Описание | Значение по умолчанию | Обязательная |
|------------|----------|-----------------------|:------------:|
| `ORCH_JWT_PUBLIC_KEY` | PEM-encoded публичный ключ для JWT | -- | Да (если нет JWKS) |
| `ORCH_JWT_JWKS_URL` | URL JWKS endpoint от UOM | -- | Да (если нет PEM key) |
| `ORCH_JWT_CLOCK_SKEW` | Допустимое расхождение часов для JWT exp | `30s` | Нет |
| `ORCH_CORS_ALLOWED_ORIGINS` | Разрешённые origins (comma-separated) | -- (same-origin) | Нет |
| `ORCH_RATE_LIMIT_READ_RPS` | Лимит read-запросов per org | `200` | Нет |
| `ORCH_RATE_LIMIT_WRITE_RPS` | Лимит write-запросов per org | `50` | Нет |
| `ORCH_RATE_LIMIT_UPLOAD_CONCURRENT` | Лимит одновременных загрузок per org | `10` | Нет |
| `ORCH_UPLOAD_MAX_SIZE` | Максимальный размер файла | `20971520` (20 МБ) | Нет |
| `ORCH_UPLOAD_ALLOWED_MIME_TYPES` | Допустимые MIME-типы | `application/pdf` | Нет |
| `ORCH_UPLOAD_TIMEOUT` | Timeout загрузки файла | `60s` | Нет |
| `ORCH_REQUEST_BODY_MAX_SIZE` | Макс. размер JSON body | `1048576` (1 МБ) | Нет |
| `ORCH_DM_TLS_ENABLED` | TLS для DM | `false` | Нет |
| `ORCH_OPM_TLS_ENABLED` | TLS для OPM | `false` | Нет |
| `ORCH_UOM_TLS_ENABLED` | TLS для UOM | `false` | Нет |
| `ORCH_BROKER_TLS_ENABLED` | TLS для RabbitMQ | `false` | Нет |
| `ORCH_REDIS_TLS_ENABLED` | TLS для Redis | `false` | Нет |

---

## 13. Модель угроз (summary)

| # | Угроза | Вектор | Уровень риска | Митигация |
|---|--------|--------|---------------|-----------|
| T-1 | Компрометация JWT-ключа | Утечка приватного ключа UOM | Критический | Ротация ключей (JWKS). Short-lived access token 15 мин. Refresh token с отзывом. Redis blacklist для jti. (ORCH-R-5) |
| T-2 | Подмена organization_id | Craft JWT / манипуляция запросом | Критический | organization_id только из верифицированного JWT. Игнорирование organization_id из body/URL/query. |
| T-3 | Brute-force login | Перебор паролей через /auth/login | Высокий | Rate limiting per IP (на уровне reverse proxy). Audit logging неудачных попыток. UOM account lockout. |
| T-4 | DoS через загрузку файлов | Множественные параллельные загрузки крупных файлов | Средний | Upload concurrency limiter (10 per org). Streaming upload. File size limit 20 МБ. Request timeout 60s. |
| T-5 | Утечка данных между tenant-ами | Ошибка в фильтрации organization_id | Критический | organization_id из JWT (не из ввода). DM RLS как второй уровень защиты. Audit logging всех операций. |
| T-6 | XSS через загруженный файл | Вредоносный PDF с JavaScript | Низкий | Оркестратор не рендерит файлы. Content-Type: application/json. X-Content-Type-Options: nosniff. DM presigned URL для скачивания (Content-Disposition: attachment). |
| T-7 | Replay attack (повтор JWT) | Перехват access token | Средний | TLS обязателен. Short-lived token 15 мин. jti для одноразовости (опционально). |
| T-8 | Утечка JWT из SSE URL | JWT в query-параметре попадает в логи | Средний | JWT исключён из application и proxy logs. Short-lived token. |
| T-9 | Cascade failure при недоступности UOM | UOM down → нет аутентификации → 503 | Высокий | JWT валидация локальная (по публичному ключу). UOM нужен только для login/refresh. Кэширование JWKS. |
