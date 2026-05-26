# ADR — Architecture Decision Records

Индекс всех архитектурных решений ContractPro Frontend. Канонический источник — таблица §21 [`../high-architecture.md`](../high-architecture.md) («ADR-log»); этот файл повторяет её и добавляет ссылки на вынесенные документы.

---

## Статусы

- **Proposed** — предложение, открыто для обсуждения. Не является обязательным.
- **Accepted** — действующее решение, влияет на код и код-ревью.
- **Deprecated** — больше не применяется к новому коду, но существующий код пока соответствует.
- **Superseded** — заменено другим ADR. В теле ADR — ссылка на замену.

---

## Индекс

| ID | Заголовок | Статус | Файл | Контекст |
|---|---|---|---|---|
| ADR-FE-01 | Feature-Sliced Design как каркас | Accepted | §1.1, §2, §21 high-architecture.md | Выбор пакетной структуры — обоснование против «плоского» layout, DDD+Hexagonal, Nx |
| ADR-FE-02 | TanStack Query для серверного стейта + SSE как источник внешних инвалидаций | Accepted | §4, §7.7, §21 high-architecture.md | Отказ от Redux/RTK Query, интеграция `setQueryData`/`invalidateQueries` из EventSource |
| ADR-FE-03 | Access Token в памяти (Zustand), Refresh Token в `HttpOnly; Secure; SameSite=Strict` cookie (fallback: `sessionStorage`) | Accepted, зависит от backend | §5.2, §21 high-architecture.md | Защита от XSS, требует backend-изменения — fallback документирован как известная уязвимость до миграции |
| ADR-FE-04 | UI: Radix + Tailwind + shadcn, а не MUI/Ant | Accepted | §3, §8.2, §21 high-architecture.md | Минимальный bundle, предсказуемое соответствие Figma-токенам (`#F55E12`) |
| ADR-FE-05 | OpenAPI как единственный источник типов API; ручные типы запрещены | Accepted | §7.1, §15.2, §21 high-architecture.md | Автогенерация `src/shared/api/openapi.d.ts` через `openapi-typescript`; CI-gate `gen:api:check` |
| ADR-FE-06 | SSE с нативным `EventSource` + polling-fallback | Accepted | §7.7, §21 high-architecture.md | Отказ от `event-source-polyfill` и WebSocket; фоллбек — кратковременный poll при проблемах с соединением |
| ADR-FE-07 | Vite + SWC вместо Next.js (SSR не требуется) | Accepted | §3, §21 high-architecture.md | SPA за nginx покрывает требования; Next добавляет split-runtime сложность без выгоды |
| ADR-FE-08 | Ссылка на Figma — обязательный артефакт для каждого UI-PR | Proposed | §21 high-architecture.md, [`CONTRIBUTING.md`](../../CONTRIBUTING.md) §3 | Дисциплина review: без Figma-ссылки UI-PR не принимается |
| ADR-FE-09 | Token pipeline: синхронизация токенов Figma ↔ `tokens.css` | Accepted | [`009-token-pipeline.md`](./009-token-pipeline.md), §8.2 high-architecture.md, [`../figma-mapping.md`](../figma-mapping.md) | Формализован по итогам Этапа 2 Figma alignment (коммит `8fa8adb`). Процедура re-extraction, naming-конвенции, discipline-правила. Источник правды — `tokens.css`, экспорт в `tailwind.config.ts`, человекочитаемая таблица — §8.2 |
| ADR-FE-10 | Аутентификация SSE через одноразовый `sse_ticket` вместо JWT в URL | Proposed | [`010-sse-ticket-auth.md`](./010-sse-ticket-auth.md) | Снижение клиентской поверхности утечки JWT (history, DevTools, расширения). Блокируется backend-задачей `ORCH-TASK-047` |

### Связанные (backend-side) ADR

| ID | Заголовок | Релевантность для frontend |
|---|---|---|
| ADR-6 (backend) | Same-origin deployment topology | §7.2 (относительный `baseURL`), §13.2 (nginx.conf), §14.3 (OTel `traceparent` без CORS-блока) |

Полный backend-log — `ApiBackendOrchestrator/architecture/high-architecture.md` (§ADR).

---

## Как добавить новый ADR

1. **Зарезервировать номер.** Следующий свободный — `ADR-FE-11` (см. индекс выше). Если ADR заменяет существующий — явно укажите это в поле `Superseded-by`/`Supersedes` и переведите предыдущий в статус `Superseded`.
2. **Создать файл** `NNN-<kebab-slug>.md` в этом каталоге. Трёхзначный номер (`011-...`) даёт естественную сортировку файлов.
3. **Использовать шаблон** из раздела ниже. ADR — одностраничный: если получается больше 2 страниц, решение стоит декомпозировать.
4. **Обновить две точки синхронно** в одном PR:
   - эту таблицу (`architecture/adr/README.md`);
   - §21 ADR-log в [`../high-architecture.md`](../high-architecture.md).
5. **Открыть PR** с префиксом `docs(adr):` и ссылкой на задачу в [`../../tasks.json`](../../tasks.json), если ADR порождён конкретной задачей.
6. **Не переписывать Accepted-ADR задним числом.** Изменение решения оформляется новым ADR со статусом `Supersedes: ADR-FE-NN`.

---

## Шаблон

Используйте живой пример: [`010-sse-ticket-auth.md`](./010-sse-ticket-auth.md) — он следует ниже описанной структуре. Минимальный набор секций:

```markdown
# ADR-FE-NN: <Заголовок одной фразой>

| Поле | Значение |
|---|---|
| Статус | **Proposed** \| Accepted \| Deprecated \| Superseded |
| Дата | YYYY-MM-DD |
| Автор | <команда / роль> |
| Зависит от | <backend/frontend задачи, другие ADR> |
| Связано с | <ADR-FE-XX, §N.N high-architecture.md> |

## Контекст
Почему решение нужно принимать сейчас. Текущее состояние, болевая точка, ограничения.

## Решение
Что именно делаем. Конкретика — протоколы, поля, интерфейсы, порядок вызовов.

## Альтернативы
Для каждой — плюсы, минусы, вердикт (rejected/accepted-later/accepted). Это не формальность: решения без альтернатив позже не выдерживают ревизию.

## Trade-offs
Таблица «Аспект → Влияние» (безопасность, UX, сложность frontend/backend, производительность, тестирование, backwards compatibility).

## План перехода
Если ADR меняет существующее поведение — этапы миграции, backwards compatibility window, метрики switchover'а.

## Метрики успеха
Как поймём, что решение работает: KPI, SLO, инциденты.

## Связанные документы
Ссылки на §-секции архитектуры, задачи в `tasks.json`, релевантные backend-ADR.
```

---

## TODO

- Формализовать ADR-FE-01..08 отдельными файлами `001-...` … `008-...` — сейчас эти решения живут только в таблице §21 архитектуры.
- Добавить `000-template.md` как отдельный файл-болванку, когда будет создан первый формализованный ADR из пункта выше.
