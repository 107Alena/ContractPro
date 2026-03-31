# Безопасность, доступ и аудит Document Management

## 1. Multi-tenancy

**Модель:** Shared infrastructure, logical isolation.

- Все таблицы содержат `organization_id`.
- Все SQL-запросы фильтруются по `organization_id` (через middleware/interceptor на уровне repository).
- Object Storage: prefix `{organization_id}/` обеспечивает логическое разделение.
- **Row-level security (RLS)** в PostgreSQL как дополнительный уровень защиты (defence in depth):

```sql
-- RLS для всех таблиц с organization_id (REV-020)
ALTER TABLE documents ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON documents
    USING (organization_id = current_setting('app.current_org_id')::UUID);

ALTER TABLE document_versions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON document_versions
    USING (organization_id = current_setting('app.current_org_id')::UUID);

ALTER TABLE artifact_descriptors ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON artifact_descriptors
    USING (organization_id = current_setting('app.current_org_id')::UUID);

ALTER TABLE version_diff_references ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON version_diff_references
    USING (organization_id = current_setting('app.current_org_id')::UUID);

ALTER TABLE audit_records ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_records
    USING (organization_id = current_setting('app.current_org_id')::UUID);
```

- Async events: `organization_id` передаётся в каждом событии. DM валидирует, что `document_id` принадлежит указанной организации.

## 2. RBAC / ABAC assumptions

DM **не реализует** собственную систему авторизации. Модель доступа:

1. API Gateway / Backend-оркестратор аутентифицирует пользователя через UOM.
2. В каждый запрос к DM передаётся доверенный контекст: `organization_id`, `user_id`, `role`.
3. DM обеспечивает:
   - **Tenant isolation**: `organization_id` — обязательный фильтр.
   - **Role-based visibility** (если потребуется): например, R-2 (бизнес-пользователь) видит только `SUMMARY`, а R-1 (юрист) — все артефакты. Реализуется через middleware в API Handler.
4. Async events: доверие к `organization_id` в событии (events ходят по внутренней сети между сервисами).

## 3. Audit trail

- **Что аудируется:**
  - Создание/архивация/удаление документа.
  - Создание версии.
  - Сохранение каждого типа артефакта (с указанием producer domain).
  - Сохранение diff.
  - Чтение артефактов (через sync API и async GetArtifactsRequest — записывается `ARTIFACT_READ` с `actor_type=DOMAIN` для async).
  - Изменение `artifact_status`.

- **Формат:** Таблица `audit_records` в PostgreSQL (append-only).

- **Append-only гарантия на уровне DB (BRE-016):**
  ```sql
  CREATE OR REPLACE FUNCTION reject_audit_modify()
  RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'audit_records is append-only'; END; $$ LANGUAGE plpgsql;
  CREATE TRIGGER no_update_audit BEFORE UPDATE OR DELETE ON audit_records FOR EACH ROW EXECUTE FUNCTION reject_audit_modify();
  ```
  Отдельная DB-роль `dm_audit_writer` с правами INSERT only.

- **Retention:** 3 года (конфигурируемо).

- **Доступ к аудиту:** Отдельный API endpoint `/api/v1/audit?document_id=&from=&to=` с ограничением по роли (только R-3 администратор и системные операторы).

## 4. Шифрование и безопасный доступ к файлам

**At rest:**
- PostgreSQL: encryption at rest через managed database (Yandex Managed PostgreSQL) или dm-crypt.
- Object Storage: server-side encryption (SSE-S3 / SSE-KMS) — конфигурируется на уровне bucket.

**In transit:**
- Все коммуникации по TLS (NFR-3.1).
- RabbitMQ: TLS-соединение.
- PostgreSQL: sslmode=require.
- Object Storage: HTTPS.

**Доступ к файлам:**
- JSON-артефакты: DM читает blob из Object Storage и отдаёт в теле HTTP 200.
- Blob-артефакты (PDF, DOCX): HTTP 302 Redirect на presigned S3 URL. Единый endpoint `GET /documents/{id}/versions/{vid}/artifacts/{type}` (REV-019).
- Presigned URL: TTL 5 минут (BRE-017), привязан к `organization_id` (проверка до генерации).
- S3 server access logging включён для аудита скачиваний.
- IP генерации URL логируется в audit record.
