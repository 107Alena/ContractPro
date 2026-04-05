# Стратегия миграций PostgreSQL (REV-030 / BRE-022)

## Обзор

Миграции схемы БД управляются через [golang-migrate/v4](https://github.com/golang-migrate/migrate) с embedded SQL-файлами. Миграции выполняются **отдельным бинарником `dm-migrate`** как init-container перед стартом приложения `dm-service`.

## Архитектура

```
┌─────────────┐     ┌──────────────┐     ┌──────────────┐
│  dm-migrate │────▶│  PostgreSQL   │◀────│  dm-service  │
│ (init-cont.)│     │              │     │ (app)        │
└─────────────┘     └──────────────┘     └──────────────┘
   1. migrate up       schema ready        2. version check
   2. exit 0                               3. start serving
```

### Принцип разделения

- **`dm-migrate`** (`cmd/dm-migrate/`) — единственный компонент, выполняющий DDL-операции. CLI с командами `up`, `down`, `goto <N>`, `version`.
- **`dm-service`** (`cmd/dm-service/`) — приложение. При старте проверяет, что schema version > 0 и не dirty. Если проверка не проходит — fail fast.

### Docker Compose

```yaml
dm-migrate:
  entrypoint: ["/usr/local/bin/dm-migrate", "up"]
  depends_on:
    postgres: { condition: service_healthy }
  restart: "no"

dm-service:
  depends_on:
    dm-migrate: { condition: service_completed_successfully }
```

## Файлы миграций

Расположение: `internal/infra/postgres/migrations/`

| Версия | Файл | Описание |
|--------|------|----------|
| 000001 | initial_schema | 7 таблиц: documents, document_versions, artifact_descriptors, version_diff_references, audit_records, outbox_events, orphan_candidates |
| 000002 | dlq_records | Таблица dm_dlq_records для DLQ replay |
| 000003 | rls_policies | Row-Level Security для tenant isolation (5 таблиц) |
| 000004 | audit_partitions | Конвертация audit_records в PARTITION BY RANGE (created_at) |
| 000005 | audit_protection | Append-only triggers + dm_audit_writer роль |

### Правила именования

- Формат: `{NNNNNN}_{description}.{up|down}.sql`
- Каждая версия ОБЯЗАТЕЛЬНО имеет пару up + down
- Все up-миграции обёрнуты в `BEGIN/COMMIT` для атомарности

## Online Migration Safety

### Безопасные операции (zero-downtime)

- `CREATE TABLE` — блокировка только на новой (пустой) таблице
- `CREATE INDEX` на пустой таблице — мгновенно
- `ALTER TABLE ENABLE ROW LEVEL SECURITY` — каталог-only, миллисекунды
- `CREATE POLICY` — каталог-only
- `CREATE FUNCTION` / `CREATE TRIGGER` — каталог-only
- `ADD COLUMN ... DEFAULT` (PostgreSQL 11+) — без table rewrite

### Операции требующие maintenance window

| Операция | Блокировка | Когда |
|----------|-----------|-------|
| `ALTER TABLE RENAME` | ACCESS EXCLUSIVE | 000004: безопасно на пустой таблице при первичном deploy |
| `INSERT INTO ... SELECT *` | ACCESS EXCLUSIVE (в рамках одной транзакции с LOCK) | 000004: только при наличии данных |
| `LOCK TABLE ... IN ACCESS EXCLUSIVE MODE` | Явная блокировка | 000004: документировано |

### Рекомендации для будущих миграций

1. **Индексы на существующих таблицах**: используйте `CREATE INDEX CONCURRENTLY` (вне транзакции)
2. **Добавление колонок**: `ADD COLUMN ... DEFAULT` (PG 11+, без table rewrite)
3. **Удаление колонок**: сначала удалите из кода, потом `ALTER TABLE DROP COLUMN`
4. **Переименование**: создайте новое, мигрируйте данные, удалите старое
5. **RLS / Triggers**: каталог-only, безопасны для online
6. **`lock_timeout`**: для live миграций устанавливайте `SET lock_timeout = '2s'` перед DDL для предотвращения длительных блокировок

## Процедура Rollback

### Откат одной версии

```bash
# Docker Compose:
docker compose run --rm dm-migrate goto 4    # откат с 5 до 4

# Локально (при наличии DSN):
DM_DB_DSN="postgres://..." ./dm-migrate goto 4
```

### Полный откат

```bash
docker compose run --rm dm-migrate down --confirm-destroy
```

> **Внимание:** `down` удаляет ВСЕ таблицы. Флаг `--confirm-destroy` обязателен для предотвращения случайного удаления данных.

### Проверка текущей версии

```bash
docker compose run --rm dm-migrate version
```

### Dirty state

Если миграция завершилась с ошибкой, schema_migrations будет в dirty state.
`dm-service` откажется стартовать. Действия:

1. Проверить `docker compose run --rm dm-migrate version` — покажет версию и dirty=true
2. Исправить проблему вручную в БД (если partial apply)
3. Обновить `schema_migrations`: `UPDATE schema_migrations SET dirty = false`
4. Повторить: `docker compose run --rm dm-migrate up`

## Concurrent Migration Safety

`golang-migrate` автоматически использует PostgreSQL advisory lock (`pg_advisory_lock`) при выполнении миграций. Это означает:

- В multi-replica Kubernetes deployment несколько init-containers могут запустить `dm-migrate up` одновременно
- Только один экземпляр получит advisory lock и выполнит миграции
- Остальные будут ждать освобождения lock, после чего увидят, что все миграции уже применены
- `dm-service` не стартует до завершения init-container (гарантия `service_completed_successfully`)

Дополнительной координации (distributed lock, leader election) не требуется.

## RLS и миграции

RLS policies применяются в миграции 000003 — **до старта приложения** (через init-container). Это гарантирует, что:

- Все таблицы защищены tenant isolation ДО первого запроса
- Fallback resolver (REV-001/REV-002) работает корректно (пустой `app.organization_id` → все строки видны)
- System-level операции (outbox poller, watchdog) не блокируются RLS

## Тестирование миграций

Unit-тесты (`migrate_test.go`) проверяют:

- Все SQL-файлы embedded и не пустые
- Каждая версия имеет пару up + down
- Версии последовательны (1..N без пробелов)
- Все up-файлы содержат BEGIN/COMMIT
- pgxDSN helper конвертирует схемы корректно
- Количество версий соответствует ожидаемому (обновлять при добавлении миграций)
