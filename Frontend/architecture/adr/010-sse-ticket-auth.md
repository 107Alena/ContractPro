# ADR-FE-10: Аутентификация SSE через одноразовый `sse_ticket` вместо JWT в URL

| Поле | Значение |
|---|---|
| Статус | **Proposed** |
| Дата | 2026-04-15 |
| Автор | Frontend Architecture |
| Зависит от | Backend-эндпоинт `POST /auth/sse-ticket` (`ORCH-TASK-047`, `ApiBackendOrchestrator/backlog-tasks.json`) |
| Связано с | ADR-FE-03 (хранение JWT), ADR-FE-06 (SSE с EventSource), `security.md` §1.7 + threat T-8 |

---

## Контекст

Frontend использует браузерный `EventSource` API для real-time обновлений статусов обработки документов через `GET /api/v1/events/stream`. EventSource API **не поддерживает** кастомные HTTP-заголовки, поэтому JWT передаётся в query-параметре:

```
GET /api/v1/events/stream?token=eyJhbGciOiJSUzI1NiIs...
```

Backend (см. `ApiBackendOrchestrator/architecture/security.md` §1.7) митигирует часть рисков:
1. JWT исключён из structured logging middleware.
2. nginx/envoy настроены на исключение `token` из proxy access-логов.
3. Access token short-lived (15 минут).
4. SSE Connection Manager валидирует JWT идентично HTTP middleware.

Threat-модель (T-8) оценивает риск как «Средний».

### Остаточные риски, не покрываемые backend-митигациями

| Поверхность утечки | Чем плохо |
|---|---|
| Browser history | URL с токеном сохраняется в истории, может попадать в backup/sync между устройствами |
| Browser DevTools Network | URL виден целиком; скриншот → токен в открытом виде |
| Third-party JS (Sentry, GA, GTM, расширения) | Любой `fetch`/`XMLHttpRequest`-interceptor видит URL |
| Referer header | Зависит от `Referrer-Policy`; может содержать SSE URL при cross-origin запросах |
| CDN / Cloud provider raw access logs | Yandex Cloud, CloudFront могут логировать необработанные access-логи независимо от nginx config |
| Browser extensions | Расширения с правами `webRequest` видят URL целиком |
| Shared screen / pair programming | URL виден на скриншотах, Zoom-демо, скринкастах |
| XSS exploitation surface | Снижает порог: атакующему не нужно читать `sessionStore`, достаточно перехватить URL |

Митигации backend закрывают **серверную поверхность** (логи). Клиентская поверхность (8 пунктов выше) **остаётся**.

---

## Решение

Заменить полный JWT в query-параметре на короткоживущий одноразовый `sse_ticket`:

```
1. Frontend → POST /api/v1/auth/sse-ticket
   Authorization: Bearer <full JWT>           ← в заголовке, безопасно

2. Backend → 200 OK
   { "ticket": "9f3a7b2c8e1d4f5a", "expires_in": 60 }

3. Frontend → GET /api/v1/events/stream?ticket=9f3a7b2c8e1d4f5a
   ← короткий opaque-ticket вместо JWT

4. Backend валидирует ticket в Redis (sse-ticket:9f3a7b2c8e1d4f5a → user_id, org_id, exp),
   удаляет ключ (одноразовость), открывает SSE-соединение.
```

### Свойства `sse_ticket`

- **Не JWT**, а opaque random string (UUID v4 или 128-bit random base64).
- **TTL 60 секунд** — Redis ключ автоматически удаляется.
- **Одноразовый** — backend удаляет ключ при первом `GET /events/stream`.
- **Привязан к конкретному `user_id` + `organization_id`** (взяты из исходного JWT).
- **Скоупирован только на SSE** — нельзя использовать для других API.
- **Новый ticket на каждый reconnect** — frontend перезапрашивает перед каждым `EventSource` open.

### Сравнение поверхностей утечки

| Поверхность | До (full JWT 15 мин) | После (sse_ticket 60с одноразовый) |
|---|---|---|
| Browser history | JWT даёт полный API-доступ 15 мин | Ticket бесполезен после использования |
| Third-party JS | JWT даёт полный API-доступ 15 мин | Ticket бесполезен |
| Browser extensions | JWT даёт полный API-доступ 15 мин | Ticket бесполезен |
| CDN raw logs | JWT даёт полный API-доступ 15 мин | Ticket бесполезен (даже до использования — только SSE-скоуп) |
| Скриншот / DevTools | JWT даёт полный API-доступ 15 мин | Ticket бесполезен |
| Replay attack | JWT работает много раз | Ticket — один раз и `expires_in: 60` |

Это типичный **defense-in-depth**: backend закрывает серверную поверхность, ticket-pattern закрывает клиентскую.

---

## Альтернативы

### 1. Cookie с JWT (включая `HttpOnly`)
- **Плюсы:** EventSource автоматически отправляет cookies; нет JWT в URL.
- **Минусы:** конфликт с ADR-FE-03 (JWT в памяти, не в storage); CSRF-vector (требует SameSite=Strict + double-submit token); cookie shared между всеми API, не только SSE; refresh-token в HttpOnly уже планируется (открытый вопрос §18 п.1) — два разных cookie с разной семантикой усложняют менеджмент.
- **Вердикт:** rejected — нарушает архитектурные решения по JWT-storage.

### 2. Polyfill `event-source-polyfill`
- **Плюсы:** заменяет EventSource на XHR-стриминг с поддержкой Authorization-header; JWT никогда в URL.
- **Минусы:** ~10 КБ кода в bundle; известные edge-case баги (не закрывает соединения корректно при reconnect под некоторыми прокси); в §3 high-architecture.md явно отклонён («в поддержке v1 нет — не включается»); не использует нативную browser-оптимизацию EventSource.
- **Вердикт:** rejected — overhead и нестабильность не оправдывают выигрыш по сравнению с ticket-подходом.

### 3. WebSocket вместо SSE
- **Плюсы:** полная поддержка заголовков в первом фрейме (через subprotocol); двунаправленность.
- **Минусы:** требует переписывания backend SSE-инфраструктуры (Connection Manager, Redis Pub/Sub, broadcast); теряет HTTP-кеширование и простоту прокси-passthrough; нет нативной auto-reconnect логики (надо реализовывать); нарушает ADR-FE-06 («SSE с нативным EventSource»).
- **Вердикт:** rejected — слишком большой scope для решения одной проблемы.

### 4. Принять текущее решение «как есть» (status quo)
- **Плюсы:** ничего не делать; backend уже митигирует серверную поверхность; threat T-8 оценён как «Средний».
- **Минусы:** клиентская поверхность утечки **никак не закрывается**; при росте использования third-party JS (Sentry session replay, аналитика) поверхность растёт; при инциденте утечки JWT через расширение/screenshot — 15 минут полного API-доступа атакующему.
- **Вердикт:** acceptable для v1 (т.к. внедрение блокируется backend-эндпоинтом), но миграция должна планироваться.

---

## Trade-offs выбранного решения

| Аспект | Влияние |
|---|---|
| Безопасность | ✅ Существенно снижает риск утечки (см. таблицу выше) |
| UX | Нейтрально — лишний REST-запрос (~50–100ms) перед открытием SSE; для пользователя незаметно |
| Сложность frontend | +1 запрос в `openEventStream`; reconnect-логика чуть усложняется (новый ticket перед каждым reconnect); ~30 строк кода |
| Сложность backend | Новый endpoint + Redis storage с TTL; небольшой объём |
| Производительность | Дополнительный Redis ключ на каждый SSE-connection; объём незначительный |
| Backwards compatibility | Backend может временно поддерживать оба механизма (`?token=` для текущего frontend, `?ticket=` для нового) — позволяет постепенный rollout |
| Network failure при истечении ticket | Edge-case: ticket истёк до использования (например, network задержка) → frontend ловит 401, запрашивает новый ticket; нужна retry-логика |
| Тестирование | Mock SSE-flow в MSW усложняется на один шаг; e2e-тесты на login → SSE требуют корректной последовательности |

---

## План перехода

1. **Сейчас (v1):** оставляем `?token=` с полным JWT. В коде `shared/api/sse.ts` стоит security-комментарий, ссылающийся на этот ADR.
2. **Backend:** реализация `ORCH-TASK-047` — endpoint `POST /api/v1/auth/sse-ticket`, Redis storage, валидация в SSE Connection Manager.
3. **Backwards compatibility window:** backend поддерживает оба механизма (`?token=` и `?ticket=`) одновременно, по фича-флагу `ORCH_SSE_TICKET_AUTH_ENABLED`.
4. **Frontend миграция:** переписать `openEventStream` на двухступенчатый flow (запрос ticket → открытие EventSource); обработка 401 при истечении ticket.
5. **Switchover:** после успешного rollout — отключить `?token=` на backend (фича-флаг → false), удалить fallback на frontend.
6. **Этот ADR переходит в Accepted** после завершения шагов 2–5.

---

## Метрики успеха

- 100% SSE-соединений используют `sse_ticket` (после switchover).
- 0 инцидентов утечки JWT через SSE URL после миграции (мониторинг через Sentry / SOC).
- Нет регрессии по UX — p95 времени до первого SSE-события не растёт более чем на 100ms.

---

## Связанные документы

- `Frontend/architecture/high-architecture.md` §7.7 (реализация SSE)
- `Frontend/architecture/high-architecture.md` §18 п.2 (открытый вопрос — этот ADR)
- `ApiBackendOrchestrator/architecture/security.md` §1.7 (текущая SSE-аутентификация + митигации)
- `ApiBackendOrchestrator/architecture/security.md` §13 threat T-8 (модель угроз)
- `ApiBackendOrchestrator/backlog-tasks.json` `ORCH-TASK-047` (backend-задача)
