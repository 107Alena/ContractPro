# ContractPro Frontend

SPA для AI-проверки договоров по ГК РФ. React 18 + TypeScript 5.4 (strict), Feature-Sliced Design v2, Vite 5 + SWC, TanStack Query, React Router 6, Radix + Tailwind + shadcn/ui, i18next (RU), MSW (mocks), Playwright (e2e), Storybook 8, Sentry + OpenTelemetry.

Источники истины архитектуры: [`architecture/high-architecture.md`](architecture/high-architecture.md), [`architecture/adr/`](architecture/adr/README.md), [`../ApiBackendOrchestrator/architecture/api-specification.yaml`](../ApiBackendOrchestrator/architecture/api-specification.yaml).

---

## Предпосылки

- **Node 20 LTS** — точная версия закреплена в [`.nvmrc`](./.nvmrc). Используйте `nvm use` (или `fnm use`, `volta`) — `package.json` не декларирует `engines`, версия берётся из `.nvmrc` в CI ([`.github/workflows/frontend-ci.yml`](../.github/workflows/frontend-ci.yml)).
- **npm 10+** (поставляется с Node 20).
- Доступ к соседнему каталогу `../ApiBackendOrchestrator/architecture/api-specification.yaml` — из него генерируются типы API (`npm run gen:api`, см. §15.2 архитектуры). В CI это каталог в том же checkout'е репозитория.
- Опционально: Docker 24+ для сборки prod-образа (`Dockerfile`, §13.1).

---

## Quickstart (5 минут)

```bash
# 1. Переключиться на Node 20
nvm use                  # либо fnm/volta — см. .nvmrc

# 2. Перейти во Frontend и установить зависимости
cd Frontend
npm ci                   # prepare-хук запустит gen:api + husky install

# 3. Запустить dev-сервер
npm run dev              # http://localhost:5173
```

После `npm run dev` откройте http://localhost:5173. По умолчанию запросы к `/api/*` идут на backend, указанный в `public/config.js` (`API_BASE_URL: '/api/v1'`). Для работы без поднятого backend'а используйте mock-режим (`npm run dev:e2e`, см. [Окружения и конфигурация](#окружения-и-конфигурация)).

**E2E role-override для `/admin`:** вход под ролью `ADMIN` в e2e-режиме делается через фикстуру `tests/e2e/fixtures/auth-state.ts` (§5.5 архитектуры). В dev используйте UI-логин — mock-пользователи описаны в `tests/msw/handlers/`.

---

## NPM-скрипты

Во Frontend нет `Makefile` — все задачи выполняются через `npm run <script>` ([`package.json`](./package.json)).

### Dev / Build

| Скрипт                  | Назначение                                                                         |
| ----------------------- | ---------------------------------------------------------------------------------- |
| `npm run dev`           | Vite dev-server на `http://localhost:5173`                                         |
| `npm run dev:e2e`       | То же, в режиме `e2e` (MSW включён, `--strictPort`) — см. [`.env.e2e`](./.env.e2e) |
| `npm run build`         | `tsc --noEmit` + `vite build` → `dist/`                                            |
| `npm run preview`       | Локальный просмотр production-сборки                                               |
| `npm run gen:api`       | Перегенерация `src/shared/api/openapi.d.ts` из OpenAPI-спеки                       |
| `npm run gen:api:check` | CI-gate: проверка дрейфа между спекой и коммитом (§15.2)                           |

### Quality

| Скрипт                 | Назначение                                                             |
| ---------------------- | ---------------------------------------------------------------------- |
| `npm run typecheck`    | `tsc --noEmit` для `src/` + `tests/e2e/`                               |
| `npm run lint`         | ESLint flat config, `--max-warnings=0` (FSD boundaries, §2.1)          |
| `npm run lint:fix`     | Автофиксы ESLint                                                       |
| `npm run format`       | Prettier write по всему проекту                                        |
| `npm run format:check` | Prettier check (CI-вариант)                                            |
| `npm run size-limit`   | Проверка бюджетов бандла (§13 CI, размеры в `package.json#size-limit`) |

### Тесты

| Скрипт               | Назначение                                     |
| -------------------- | ---------------------------------------------- |
| `npm run test`       | Vitest watch (unit + компонентные)             |
| `npm run test:ci`    | `vitest run --coverage` (CI-режим)             |
| `npm run e2e`        | Playwright, self-hosted webServer в `e2e`-моде |
| `npm run e2e:headed` | Playwright с видимым браузером (debug)         |
| `npm run e2e:ci`     | `CI=true` + list-reporter                      |

### Storybook

| Скрипт                    | Назначение                                                  |
| ------------------------- | ----------------------------------------------------------- |
| `npm run storybook`       | Storybook dev на `http://localhost:6006`                    |
| `npm run build-storybook` | Статическая сборка Storybook для Chromatic                  |
| `npm run chromatic`       | Публикация Storybook в Chromatic (visual regression, §10.4) |

---

## Окружения и конфигурация

### Runtime-конфигурация (`public/config.js` → `window.__ENV__`)

Согласно §13.5 архитектуры, DSN, endpoint'ы и feature-флаги инжектятся в контейнере через `docker/entrypoint.sh`, переписывая `/config.js`. В dev этот файл закоммичен с безопасными дефолтами и читается через [`src/shared/config/runtime-env.ts`](./src/shared/config/runtime-env.ts).

| Ключ                           | По умолчанию    | Описание                                                          |
| ------------------------------ | --------------- | ----------------------------------------------------------------- |
| `API_BASE_URL`                 | `/api/v1`       | Базовый путь к Orchestrator (same-origin)                         |
| `SENTRY_DSN`                   | `''` (выключен) | Sentry DSN; пусто → init — no-op (§14.2)                          |
| `SENTRY_ENVIRONMENT`           | —               | Переопределяет `import.meta.env.MODE` в Sentry-событиях           |
| `OTEL_ENDPOINT`                | `''` (выключен) | OTLP/HTTP collector URL; пусто → OTel не инициализируется (§14.3) |
| `FEATURES.FEATURE_SSO`         | `false`         | Включение SSO-кнопки на Login                                     |
| `FEATURES.FEATURE_DOCX_UPLOAD` | `false`         | Приём DOCX наравне с PDF на NewCheck (§3)                         |

### Build-time переменные (Vite)

| Ключ              | Назначение                                                                                                                                  |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `VITE_GIT_SHA`    | Release tag для Sentry/OTel (`contractpro-frontend@<sha>`). В CI ставится явно; локально берётся из `git rev-parse HEAD` в `vite.config.ts` |
| `VITE_ENABLE_MSW` | `true` включает MSW-worker в браузере. Установлен в [`.env.e2e`](./.env.e2e), подхватывается `vite --mode e2e`                              |

### CI-only секреты

Не требуются локально. Настраиваются в GitHub Secrets/Variables:

- `CHROMATIC_TOKEN` — публикация Storybook;
- `REGISTRY_USERNAME` / `REGISTRY_PASSWORD` / `REGISTRY_HOST` — push docker-образа на master.

Подробности — [`.github/workflows/frontend-ci.yml`](../.github/workflows/frontend-ci.yml).

---

## Структура проекта

Feature-Sliced Design v2 (§2 архитектуры). Направление зависимостей: `app → processes → pages → widgets → features → entities → shared` (одностороннее, enforced через ESLint boundaries, см. [`eslint.config.js`](./eslint.config.js)).

```
Frontend/
├── public/                 # статика + runtime config.js + MSW worker
├── src/
│   ├── app/                # providers, router, стили
│   ├── processes/          # cross-page flows (auth, upload)
│   ├── pages/              # одна страница = один слайс
│   ├── widgets/            # композиция нескольких features
│   ├── features/           # бизнес-фичи (изолированы друг от друга)
│   ├── entities/           # доменные типы + query-ключи + UI-кирпичи
│   ├── shared/             # api, auth, ui, lib, config, i18n, observability
│   ├── main.tsx
│   └── test-setup.ts
├── tests/
│   ├── e2e/                # Playwright
│   └── msw/                # MSW handlers
├── architecture/
│   ├── high-architecture.md
│   └── adr/                # ADR + индекс (README.md)
├── docker/                 # entrypoint.sh, security-headers.conf
├── scripts/                # prepare-husky.cjs
├── Dockerfile              # multi-stage (§13.1)
├── nginx.conf              # SPA + reverse-proxy + SSE (§13.2)
├── eslint.config.js
├── vite.config.ts
├── tsconfig.json
└── package.json
```

См. CONTRIBUTING — правила FSD, PR-гейты, code review checklist: [`CONTRIBUTING.md`](./CONTRIBUTING.md).

---

## Troubleshooting

| Симптом                                                              | Причина                                                                                                        | Решение                                                                                                                    |
| -------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `npm ci` падает на `prepare` → `openapi-typescript ... no such file` | Нет доступа к `../ApiBackendOrchestrator/architecture/api-specification.yaml`                                  | Клонировать/подтянуть соседний модуль. Обходной путь: `npm ci --ignore-scripts && npm run gen:api` после подключения спеки |
| `husky - command not found` или hooks не ставятся                    | Запуск с `CI=true` (husky пропускает install — см. [`scripts/prepare-husky.cjs`](./scripts/prepare-husky.cjs)) | Для CI это ожидаемо. Локально — снять `CI=true` и выполнить `npm run prepare`                                              |
| `The engine "node" is incompatible` / `Unsupported Node`             | Неверная версия Node                                                                                           | `nvm install 20 && nvm use` (см. [`.nvmrc`](./.nvmrc))                                                                     |
| `Port 5173 is already in use`                                        | Занят другой dev-сервер                                                                                        | `npm run dev -- --port 5174` или освободить порт                                                                           |
| В браузере `GET /mockServiceWorker.js 404` в e2e-режиме              | Отсутствует MSW worker-файл в `public/`                                                                        | `npx msw init public/ --save` (подтверждено в §10.3)                                                                       |
| `gen:api:check` падает в CI                                          | OpenAPI-спека изменилась, `src/shared/api/openapi.d.ts` не перегенерирован                                     | Локально: `npm run gen:api`, закоммитить результат (§15.2)                                                                 |
| `size-limit` падает после добавления зависимости                     | Превышение бюджета (§13.3)                                                                                     | Проверить `manualChunks` в [`vite.config.ts`](./vite.config.ts), обосновать рост в PR или перенести импорт в lazy-chunk    |

---

## Ссылки

- Архитектура: [`architecture/high-architecture.md`](architecture/high-architecture.md)
- ADR: [`architecture/adr/README.md`](architecture/adr/README.md)
- Правила участия: [`CONTRIBUTING.md`](./CONTRIBUTING.md)
- OpenAPI-спека: [`../ApiBackendOrchestrator/architecture/api-specification.yaml`](../ApiBackendOrchestrator/architecture/api-specification.yaml)
- CI: [`.github/workflows/frontend-ci.yml`](../.github/workflows/frontend-ci.yml)
- Журнал прогресса: [`progress.md`](./progress.md)
- Бэклог задач: [`tasks.json`](./tasks.json), [`backlog-tasks.json`](./backlog-tasks.json)
