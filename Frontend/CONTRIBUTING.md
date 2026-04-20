# Правила участия — ContractPro Frontend

Этот документ — рабочий протокол, не декларация. Открывая PR, вы обязуетесь следовать §-ссылкам на [`architecture/high-architecture.md`](architecture/high-architecture.md) и проходить все гейты из [`.github/workflows/frontend-ci.yml`](../.github/workflows/frontend-ci.yml).

Начало работы — [`README.md`](./README.md) (Quickstart за 5 минут).

---

## 1. Feature-Sliced Design — обязательные правила

Полное описание — §1.1 и §2 архитектуры. Ниже — минимум, который проверяется `eslint-plugin-boundaries`.

### 1.1 Направление зависимостей

```
app  →  processes  →  pages  →  widgets  →  features  →  entities  →  shared
```

- Импорты вверх запрещены (`pages/*` не может импортировать из `app/`).
- Горизонтальные импорты между слайсами одного слоя запрещены (`features/contract-upload` не видит `features/contract-list`).
- Shared — плоский набор сегментов (`api/auth/ui/lib/config/i18n/observability/layout`), сегменты друг друга импортировать могут (FSD v2: segments, not slices).

Нарушение границ = ошибка ESLint. См. конфиг [`eslint.config.js`](./eslint.config.js).

### 1.2 Public API слайса

- Каждый слайс (`features/*`, `entities/*`, `widgets/*`, `pages/*`) публикует **только** то, что экспортировано из `index.ts`. Всё остальное приватно.
- Импорты вида `@/features/foo/internal/helpers` запрещены.

### 1.3 Правило «один экран — одна страница»

Page-компонент оркеструет widgets, не знает про API и не содержит бизнес-логики (§1.2).

### 1.4 Запрет глубокого prop-drilling

Props прокидываются максимум на 2 уровня. Глубже — widget или стор.

---

## 2. Conventional Commits

Формат: `<type>(<scope>): <subject>`. Enforce: commitlint ([`commitlint.config.cjs`](./commitlint.config.cjs)) в hook `commit-msg`.

**Разрешённые типы:** `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `build`, `ci`, `perf`, `revert`, `style`.

**Scope** — либо имя слайса (`contract-upload`, `admin`), либо `fe-task-XXX` из [`tasks.json`](./tasks.json).

Примеры:

```
feat: fe-task-051 — OpenTelemetry SDK + fetch/XHR instrumentation (§14.3)
fix(auth): refresh race — single-flight queue (§5.4)
docs(adr): add ADR-FE-11 — CSP report-only rollout plan
chore: bump playwright to 1.59
```

Subject — в императиве, без точки в конце, ≤100 символов. Body (опционально) — «почему», не «что». Co-authored-by указывается в теле (см. последние коммиты).

---

## 3. Ветки и PR

- Имя ветки: `feature/fe-task-XXX-kebab-slug` (либо `fix/`, `refactor/`, `docs/`).
- PR title = subject основного коммита. Draft → Ready только после зелёного CI.
- В описании PR — ссылка на задачу из [`tasks.json`](./tasks.json) и, для UI-изменений, ссылка на Figma (ADR-FE-08, §21).
- Merge: squash. История на master — линейна.

---

## 4. PR-гейты (CI)

Файл: [`.github/workflows/frontend-ci.yml`](../.github/workflows/frontend-ci.yml). Каждый шаг — блокер merge'а.

### Job `quality`

1. `npm ci` (без `--ignore-scripts` — `gen:api` запускается из `prepare`).
2. `npm run gen:api:check` — дрейф типов из OpenAPI (§15.2).
3. `npm run typecheck` — strict + `exactOptionalPropertyTypes` + `noUncheckedIndexedAccess`.
4. `npm run lint` — `--max-warnings=0`.
5. `npm run test:ci` — Vitest + coverage.
6. `npm run build` — TS + Vite production build.
7. `npx size-limit` — бюджеты из `package.json#size-limit` (§13.3).
8. Chromatic (только PR, если `CHROMATIC_TOKEN` задан) — визуальная регрессия для Storybook.

### Job `e2e`

`npm run e2e:ci` — Playwright против `vite --mode e2e` (MSW-mocks).

### Job `docker`

На push в master — `docker build` многоступенчатого образа (§13.1); push в registry условный.

---

## 5. Локальные проверки перед push

```bash
npm run typecheck && npm run lint && npm run test:ci && npm run build && npm run size-limit
```

Husky уже запустит `lint-staged` на `pre-commit` (eslint + prettier). На `commit-msg` — commitlint. `pre-push` — локальная реплика quality-job'а.

---

## 6. Code Review Checklist

Копируется в шаблон PR. Ревьюер и автор ставят галочки.

- [ ] **FSD-границы.** ESLint boundaries проходит. Нет горизонтальных импортов между слайсами одного слоя (§2.1).
- [ ] **Public API.** Новые слайсы экспортируют только через `index.ts`. Приватные модули не импортируются снаружи (§1.2).
- [ ] **OpenAPI — единственный источник типов API.** Ручные типы для `/api/*` запрещены; используется `src/shared/api/openapi.d.ts` (§15.2, ADR-FE-05).
- [ ] **TS strict.** Нет `any`, `as unknown as`, `// @ts-ignore/@ts-expect-error` без inline-обоснования.
- [ ] **TanStack Query.** Query-ключи централизованы в `shared/api/query-keys.ts`; нет прямых `fetch/axios` из компонентов (§4.2, §7.1).
- [ ] **SSE.** Новые real-time инвалидации проходят через `shared/api/sse` wrapper; не открываем EventSource вручную (§7.7, ADR-FE-06).
- [ ] **RBAC.** Видимость UI — через `useCan()`/guard-компоненты; клиентская проверка не подменяет серверную (§5.5, §5.6).
- [ ] **i18n.** Все пользовательские строки — через `t(...)`, ключи лежат в `shared/i18n/locales/ru/*`. Новый язык — только если ТЗ требует (NFR-5.2).
- [ ] **A11y.** Используются Radix-примитивы; у интерактивных элементов — aria-label/keyboard nav; контраст WCAG 2.1 AA (§10.4, §12.1).
- [ ] **Observability.** Ошибки не проглатываются, PII не попадает в Sentry (scrubber покрывает новые поля). `console.log/debug` отсутствует — только `shared/observability` (§14.1, §14.2).
- [ ] **Тесты.** Unit для utils/hooks; компонентные для виджетов; Playwright — для критических путей. Покрытие критичных слоёв не падает (§10.4).
- [ ] **Storybook.** Новый `shared/ui` или `widget` — `*.stories.tsx` со всеми состояниями из Figma (§8.5, §15.1).
- [ ] **Size-limit.** Бюджет не превышен. Рост ≥ 10% — обоснован в PR (ленивые импорты, tree-shaking, `manualChunks`).
- [ ] **Runtime-конфиг.** Новые env-ключи добавлены в `RuntimeEnv`, `public/config.js`, `docker/entrypoint.sh` и README-таблицу (§13.5).
- [ ] **Figma-ссылка** (для UI-PR) — приложена (ADR-FE-08).
- [ ] **ADR.** Значимое архитектурное решение сопровождается ADR в `architecture/adr/` и обновлением §21 архитектуры.

---

## 7. Политика зависимостей

- Новые runtime-зависимости обсуждаются в PR: владелец поля, размер в бандле, лицензия (§3).
- Обновление minor/patch — `chore(deps): ...` без ADR.
- Major-обновление фреймворка (React/Vite/Router/Query/Tailwind) — отдельный PR + ADR.
- Devtools-only зависимости (testing, linting) — только в `devDependencies`.

---

## 8. Безопасность

- Секреты в коде запрещены (лог/история). Runtime-конфиг — через `window.__ENV__` (§13.5).
- Security-replyся (CSP, headers) живут в [`docker/security-headers.conf`](./docker/security-headers.conf) и [`nginx.conf`](./nginx.conf) (§12.2).
- О возможной уязвимости сообщите приватно владельцу репозитория — не открывайте публичный issue.

---

## 9. Документация

- Архитектурные изменения → §-секция в `high-architecture.md` + при необходимости ADR (`architecture/adr/`).
- Новые npm-скрипты или env-переменные → README (таблицы «Скрипты» и «Окружения»).
- Публичные соглашения команды (коммиты, ветки, review) → этот файл.

Источник истины для ADR — [`architecture/adr/README.md`](architecture/adr/README.md).
