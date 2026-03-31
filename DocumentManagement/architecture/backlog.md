# Бэклог будущих доработок архитектуры Document Management

Артефакты, которые необходимо подготовить для перехода от архитектурной концепции к реализации.

| # | Артефакт | Описание | Приоритет |
|---|----------|----------|-----------|
| ~~1~~ | ~~**Sequence diagrams**~~ | ~~Для каждого из 11 сценариев (раздел 8 high-architecture.md)~~ | ~~Высокий~~ | Готово: [sequence-diagrams.md](sequence-diagrams.md) |
| ~~2~~ | ~~**State machine diagram**~~ | ~~`artifact_status` переходы для DocumentVersion~~ | ~~Высокий~~ | Готово: [state-machine.md](state-machine.md) |
| ~~3~~ | ~~**Event catalog**~~ | ~~Полный каталог всех событий DM (входящие, исходящие, DLQ) с JSON schema~~ | ~~Высокий~~ | Готово: [event-catalog.md](event-catalog.md) |
| 4 | **ER diagram** | Визуализация модели данных (раздел 10.1 high-architecture.md) | Высокий |
| ~~5~~ | ~~**API specification**~~ | ~~OpenAPI 3.0 для sync REST API (раздел 9.3 high-architecture.md)~~ | ~~Высокий~~ | Готово: [api-specification.yaml](api-specification.yaml) |
| ~~6~~ | ~~**Backlog на реализацию**~~ | ~~Task breakdown по фазам (аналогично tasks.json DP)~~ | ~~Высокий~~ | Готово: [../tasks.json](../tasks.json) (35 задач) + [../progress.md](../progress.md) |
| 7 | **Component diagram** (визуальный) | Визуализация диаграммы из раздела 7.1 high-architecture.md | Средний |
| 8 | **ADR list** | Формализация ключевых решений (outbox, idempotency, monolith) | Средний |
| 9 | **Configuration reference** | Полный список `DM_*` переменных окружения (по аналогии с `configuration.md` DP) | Средний |
| 10 | **Schema contracts (JSON Schema)** | Формальные схемы для каждого event type | Средний |
| 11 | **Deployment guide** | Docker, Docker Compose, миграции, secrets | Средний |
