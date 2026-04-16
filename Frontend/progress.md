# Frontend Implementation Progress

Лог выполнения задач из `Frontend/tasks.json`. Каждый агент после завершения задачи добавляет запись в формате:

```
## FE-TASK-XXX — <название> (YYYY-MM-DD)

**Статус:** done
**Категория:** <category>
**Приоритет:** <priority>

**Что сделано:**
- ...

**Ключевые решения / отклонения от acceptance criteria:**
- ...

**Затронутые файлы:**
- ...
```

---

## FE-TASK-004 — Scaffolding Vite 5 + React 18 + TS 5 strict (2026-04-16)

**Статус:** done
**Категория:** infrastructure
**Приоритет:** critical

**Что сделано:**
- Инициализирован корень `Frontend/` как npm-проект `contractpro-frontend` (type=module).
- `package.json`: scripts `dev`/`build`/`preview`/`typecheck`; deps react@18.3 + react-dom@18.3 + react-router-dom@6.22; devDeps typescript@5.4 + vite@5.2 + @vitejs/plugin-react-swc@3.6 + @types/node/react/react-dom.
- `tsconfig.json` со strict-флагами: `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noImplicitOverride`, `noFallthroughCasesInSwitch`; `jsx=react-jsx`; `moduleResolution=bundler`; `target=ES2022`; alias `@/*` → `src/*`; `types: ["vite/client"]`.
- `tsconfig.node.json` — изолированный конфиг для `vite.config.ts` (IDE-support, не подключён как project reference).
- `vite.config.ts`: `@vitejs/plugin-react-swc`, alias `@` → `src`, `build.target=es2022`, `server.port=5173` + `strictPort`.
- `index.html` (`lang=ru`) + `src/main.tsx` (StrictMode + createRoot + defensive null-check) + `src/App.tsx` (named export `<h1>Hello ContractPro</h1>`) + `src/vite-env.d.ts` + `.gitignore`.

**Ключевые решения / отклонения от acceptance criteria:**
- Build-скрипт `tsc --noEmit && vite build` вместо арх. §20.5 `tsc -b && vite build`. Project references потребуют `composite:true` в обоих app/node-конфигах и пустой root tsconfig — преждевременно на scaffolding-этапе с одним эффективным проектом. Переход на `-b` уместен, когда появятся композитные проекты (typings-слой, storybook).
- `tsconfig.node.json` создан, но **не** подключён через `references` — работает как standalone IDE-конфиг для `vite.config.ts` с `types: ["node"]`. Подключим при переходе на `tsc -b`.
- Flags `noUnusedLocals`/`noUnusedParameters` и `verbatimModuleSyntax` НЕ включены — эту дисциплину покроет ESLint в FE-TASK-005 (избегаем дублирования между tsc и lint-слоем).
- Subagents: typescript-pro (review tsconfig), react-specialist (review main.tsx + vite.config.ts), code-reviewer (финальный review — "ship it").

**Верификация:**
- `npm install` — 31 packages, ok.
- `npm run typecheck` — 0 errors.
- `npm run build` — dist/ готов, 142.58 kB / 45.77 kB gzip.
- `npm run dev` — VITE v5.4.21 ready on http://localhost:5173/ (strictPort, без fallback на 5174).

**Заметки для следующих итераций:**
- FE-TASK-005 (ESLint+Prettier+boundaries) + FE-TASK-007 (FSD-скелет) разблокированы.
- При введении project references (`tsc -b`) — добавить `*.tsbuildinfo` в `.gitignore`.
- При миграции `vite.config.ts` на нативный ESM — заменить `__dirname` на `path.dirname(fileURLToPath(import.meta.url))`.

**Затронутые файлы:**
- `Frontend/package.json`, `Frontend/package-lock.json`
- `Frontend/tsconfig.json`, `Frontend/tsconfig.node.json`
- `Frontend/vite.config.ts`, `Frontend/index.html`
- `Frontend/src/main.tsx`, `Frontend/src/App.tsx`, `Frontend/src/vite-env.d.ts`
- `Frontend/.gitignore`

---

## FE-TASK-005 — ESLint 9 flat + Prettier + eslint-plugin-boundaries (FSD) + import sorting (2026-04-16)

**Статус:** done
**Категория:** infrastructure
**Приоритет:** critical

**Что сделано:**
- `Frontend/eslint.config.js` — ESLint 9 flat config: `@eslint/js` + `typescript-eslint` + `eslint-plugin-react` (+ jsx-runtime preset) + `eslint-plugin-react-hooks` + `eslint-plugin-jsx-a11y` + `eslint-plugin-import` (с `eslint-import-resolver-typescript`) + `eslint-plugin-boundaries` + `eslint-plugin-simple-import-sort`, в конце `eslint-config-prettier` для отключения конфликтующих style-правил.
- FSD-enforcement по §2.1: 7 слоёв app→processes→pages→widgets→features→entities→shared с `default: disallow`. `allow`-списки строят нисходящую иерархию; для каждого слоя — только same-slice self-imports (через `capture: ['slice']` + шаблон `'${from.slice}'`). Shared трактуется как flat segments (FSD v2, подтверждено code-architect).
- Дополнительные boundaries-правила: `no-private` (forced public API через index.ts; off для `src/app/**`), `no-unknown` (error), `no-unknown-files` (warn — чтобы не валил lint на промежуточных коммитах FE-TASK-007 scaffolding).
- `Frontend/.prettierrc.json` — singleQuote, semi, printWidth=100, trailingComma=all, tabWidth=2, endOfLine=lf, arrowParens=always.
- `Frontend/.prettierignore` — dist/node_modules/coverage/storybook-static/test-results, сгенерированные openapi.d.ts, архитектурные markdown (`architecture/`), tasks.json/progress.md (managed manually), секреты `.env*` + `*.log`.
- `package.json` scripts: `lint` (`eslint . --max-warnings=0`), `lint:fix`, `format`, `format:check`.
- devDeps (14 новых): eslint@^9.39, @eslint/js, @eslint/compat, typescript-eslint@^8.58 (↑ с архитектурного ^7.8 — требование ESLint 9), eslint-plugin-react@^7.37, eslint-plugin-react-hooks@^5.2, eslint-plugin-jsx-a11y@^6.10, eslint-plugin-import@^2.32, eslint-plugin-boundaries@^4.2, eslint-plugin-simple-import-sort@^12.1, eslint-config-prettier@^9.1, eslint-import-resolver-typescript@^3.10, globals@^15.15, prettier@^3.8.

**Ключевые решения / отклонения от acceptance criteria:**
- `typescript-eslint` — ^8.58 вместо §20.5-pin ^7.8. Причина: ESLint 9 flat-config требует tseslint@8; tseslint@7 peer-deps с `eslint@^8.56`, что конфликтует с критерием "ESLint 9 flat config". Критерий ESLint 9 приоритетнее pin-версии плагина.
- `boundaries/no-unknown-files` установлен в **warn**, а не error. Причина: FE-TASK-007 будет scaffolding-ить FSD-папки поэтапно, error ломал бы lint на промежуточных коммитах. Возврат в error — после стабилизации структуры (пометка в notes_for_next_tasks).
- Script `lint = "eslint . --max-warnings=0"` — жёстче критерия `"eslint ."`. Обоснование: CI не должен молча пропускать warnings (включая будущие `no-unknown-files`), lint-staged работает с отдельным вызовом `eslint --fix` без флага.
- `src/main.tsx` + `src/App.tsx` — временно в `boundaries/ignore` и с override `boundaries/element-types: off`. Причина: это placeholder-заглушка из FE-TASK-004 (находится в корне `src/`, а не в слое), переедет в `src/app/` в FE-TASK-030.
- `.prettierignore` исключает `architecture/`, `tasks.json`, `progress.md`, `backlog-tasks.json` — это managed-документы, не исходный код.
- Subagents: code-architect (review FSD-правил — подтвердил shared как flat segments, app без slice-isolation; посоветовал добавить `no-private`, `no-unknown`, `no-unknown-files` + `eslint-import-resolver-typescript`), code-reviewer (финальный review — "ready to merge", 3 nits применены: `no-unknown-files` → warn, `--max-warnings=0`, `.env*` в prettierignore).

**Верификация (все test_steps из задачи):**
- Шаг 1 ✓: `npm run lint` — 0 errors, 0 warnings (с `--max-warnings=0`).
- Шаг 2 ✓: создал `src/features/sample-feature/index.ts` (exports `sampleFeature`) и `src/entities/sample-entity/bad.ts` (`import { sampleFeature } from '@/features/sample-feature'`) → `npx eslint src/entities/sample-entity/bad.ts` → `error  No rule allowing this dependency was found. File is of type 'entities' with slice 'sample-entity'. Dependency is of type 'features' with slice 'sample-feature'  boundaries/element-types` → файлы удалены, `ls src/` показывает только App.tsx, main.tsx, vite-env.d.ts.
- Шаг 3 ✓: `npx prettier --check .` — "All matched files use Prettier code style!".

**Дополнительно проверено:**
- `npm run typecheck` — 0 errors.
- `npm run build` — dist/ 142.58 kB / 45.77 kB gzip, без ошибок.

**Заметки для следующих итераций:**
- FE-TASK-006 (Husky + lint-staged + commitlint): в lint-staged вызывать `eslint --fix` без `--max-warnings=0` (этот флаг — для CI).
- FE-TASK-007 (FSD-скелет): после создания всех 7 FSD-директорий с index.ts — перевести `boundaries/no-unknown-files` обратно в `error`; убрать `src/main.tsx` и `src/App.tsx` из `boundaries/ignore` (они переедут в `src/app/`).
- FE-TASK-030 (App shell): убрать override `boundaries/element-types: off` для main.tsx/App.tsx; `App.tsx` станет `src/app/App.tsx`.
- Будущий ADR / task: настроить segment-isolation внутри shared (shared/ui не импортирует shared/api и т.п.) через `capture: ['segment']` в `FSD_ELEMENTS`.

**Затронутые файлы:**
- `Frontend/eslint.config.js` (new)
- `Frontend/.prettierrc.json` (new)
- `Frontend/.prettierignore` (new)
- `Frontend/package.json` — +4 scripts, +14 devDeps
- `Frontend/package-lock.json` — +269 packages

---

## FE-TASK-007 — FSD скелет src/{app,processes,pages,widgets,features,entities,shared}/ (2026-04-16)

**Статус:** done
**Категория:** infrastructure
**Приоритет:** critical

**План:**
1. Прочитать §2 high-architecture.md, определить полный перечень слайсов.
2. Создать директории по уровням FSD + `index.ts` c `export {};` для каждого слайса/сегмента shared.
3. `src/app/` subdirs (providers/router/styles) — `.gitkeep` (не slice public API).
4. `features/auth/` nested segments (login/refresh-session/logout) — `.gitkeep` внутри slice.
5. Обновить `eslint.config.js`: `boundaries/no-unknown-files` warn → error (запланировано в FE-TASK-005).
6. Прогнать typecheck / lint / prettier / build.

**Что сделано:**
- Слой `app`: 3 subdirs (providers, router, styles) c `.gitkeep`.
- Слой `processes`: 2 slices (auth-flow, upload-and-analyze).
- Слой `pages`: 14 slices (landing, auth, dashboard, new-check, contracts-list, contract-detail, result, comparison, reports, audit, admin-policies, admin-checklists, settings, errors).
- Слой `widgets`: 14 slices (sidebar-navigation, topbar, risk-profile-card, mandatory-conditions-checklist, risks-list, recommendations-list, diff-viewer, versions-timeline, documents-table, audit-table, processing-progress, export-share-modal, feedback-block, legal-disclaimer).
- Слой `features`: 16 slices (auth, contract-upload, contract-archive, contract-delete, version-upload, version-recheck, comparison-start, low-confidence-confirm, filters, search, pagination, export-download, share-link, feedback-submit, policy-edit, checklist-edit). `features/auth/{login,refresh-session,logout}/.gitkeep` — nested segments внутри slice auth.
- Слой `entities`: 13 slices (user, contract, version, job, risk, recommendation, summary, diff, report, policy, checklist, audit-record, artifact).
- Слой `shared`: 7 segments (api, auth, ui, lib, config, i18n, observability) — flat (без slice-isolation, FSD v2).
- Итого: **66 index.ts** (`export {};`) + **6 .gitkeep**-файлов.
- `eslint.config.js`: `boundaries/no-unknown-files` warn → error (комментарий обновлён; main.tsx/App.tsx остаются в `boundaries/ignore` до FE-TASK-030).

**Ключевые решения / отклонения от acceptance criteria:**
- `src/app/` subdirs — `.gitkeep`, не `index.ts`. FSD не требует public-API-barrel у app-layer (композиция, не экспортируемый слайс). `export {};` создал бы ложный barrel-контракт, который придётся удалять в FE-TASK-030. Следуем FSD-идиоме.
- `features/auth` — плоский slice с nested-segment-папками (`login/refresh-session/logout/`). FSD v2 не поддерживает вложенные слайсы; архитектурный `§2` tree изображает auth как parent с сегментами. Альтернатива — разбить на `auth-login`/`auth-refresh`/`auth-logout` как независимые features — оставлена на будущее решение команды (ADR при необходимости).
- `boundaries/no-unknown-files` поднят в `error` — отклонение вверх от требований FE-TASK-007, но соответствует notes_for_next_tasks из FE-TASK-005. После scaffolding все файлы под `src/` классифицированы FSD_ELEMENTS, неклассифицированных остаться не должно (root-файлы main.tsx/App.tsx в `boundaries/ignore`).

**Верификация:**
- Шаг 1 ✓: `ls src/` — присутствуют все 7 FSD-папок (app, processes, pages, widgets, features, entities, shared).
- Шаг 2 ✓: `npm run typecheck` — 0 errors (пустые `export {};` валидны при `isolatedModules: true`).
- Шаг 3 ✓: `npm run lint` — 0 errors, 0 warnings (с `--max-warnings=0`; `boundaries/no-unknown-files=error` не сработал — все 66 index.ts попадают в FSD_ELEMENTS).
- Дополнительно: `npx prettier --check .` — clean; `npm run build` — dist/ 142.58 kB / 45.77 kB gzip, без ошибок.
- Makefile в Frontend отсутствует (проект на npm-скриптах) — этап неприменим.
- Subagents: **code-architect** (проверка плана: 5-вопросная валидация slice granularity + безопасность promote `no-unknown-files`→error + enumeration §2; verdict plan-OK); **code-reviewer** (финал: "ship it", отметил §2-vs-features/auth как non-blocker для follow-up).

**Соответствие архитектуре:**
- §2 FSD tree — структура создана 1:1 (7 layers + все перечисленные slices/segments).
- §2.1 правила зависимостей — не затронуты (FSD_ELEMENTS + boundaries/element-types уже были в FE-TASK-005).

**Заметки для следующих итераций:**
- FE-TASK-011 (openapi-typescript): скрипт `gen:api` будет писать в готовый `src/shared/api/openapi.d.ts` (директория создана).
- FE-TASK-017 (Tailwind + tokens.css): файлы пойдут в `src/app/styles/` (директория готова, пока с `.gitkeep`).
- FE-TASK-030 (App shell): наполнить `src/app/providers/*.tsx`, `src/app/router/routeTree.tsx`, `src/app/styles/*.css`; перенести `src/App.tsx` → `src/app/App.tsx`; из `eslint.config.js` убрать `src/main.tsx`/`src/App.tsx` из `boundaries/ignore` и override `element-types:off`.
- При сегментной изоляции shared (shared/ui ≠ shared/api) — расширить FSD_ELEMENTS с `capture: ['segment']` для `src/shared/*` и добавить отдельное rule в `boundaries/element-types`.
- Если команда решит плоскую структуру для auth-features — обновить §2 high-architecture.md + переименовать директории.

**Затронутые файлы:**
- `Frontend/src/app/{providers,router,styles}/.gitkeep` (3 new)
- `Frontend/src/processes/*/index.ts` (2 new)
- `Frontend/src/pages/*/index.ts` (14 new)
- `Frontend/src/widgets/*/index.ts` (14 new)
- `Frontend/src/features/*/index.ts` (16 new) + `Frontend/src/features/auth/{login,refresh-session,logout}/.gitkeep` (3 new)
- `Frontend/src/entities/*/index.ts` (13 new)
- `Frontend/src/shared/*/index.ts` (7 new)
- `Frontend/eslint.config.js` (modified: boundaries/no-unknown-files warn → error)

---

## FE-TASK-011 — openapi-typescript генерация типов + CI-gate (2026-04-16)

**Статус:** done
**Категория:** api-layer
**Приоритет:** critical

**План:**
1. Консультация с code-architect: версия пакета, подход к CI-gate, политика reexport.
2. Установить `openapi-typescript@^7`, добавить scripts `gen:api` + `gen:api:check` + `prepare`.
3. Сгенерировать `src/shared/api/openapi.d.ts` из OpenAPI-спеки оркестратора.
4. Добавить сгенерированный файл в `eslint.config.js` ignores.
5. Smoke-тест импорта типов (components, paths).
6. Финальные проверки: typecheck / lint / prettier / build.
7. Обновить §20.5 архитектуры (версия пакета).

**Что сделано:**
- Установлен `openapi-typescript@^7.13.0` (актуальная major, не `^6.7.0` из §20.5 — подтверждено code-architect; обновил §20.5 одной строкой).
- `Frontend/package.json` scripts:
  - `gen:api` — `openapi-typescript ../ApiBackendOrchestrator/architecture/api-specification.yaml -o src/shared/api/openapi.d.ts`.
  - `gen:api:check` — `npm run gen:api && git diff --exit-code -- src/shared/api/openapi.d.ts` (CI-gate: fail при расхождении committed-версии со свежей регенерацией).
  - `prepare` — `npm run gen:api` (npm lifecycle hook — не husky; автоматическая регенерация при `npm install`/`npm ci`).
- `src/shared/api/openapi.d.ts` — **1834 строки**, auto-generated из 1598-строчного OpenAPI 3.0.3 spec. Включает `paths`, `components.schemas` (ContractList, ContractSummary, VersionDetails, Risk, Recommendation, Summary, Diff, Report, Policy, Checklist, AuditRecord, Artifact, UserProfile, UserPermissions, ErrorResponse, ValidationFieldError и т.д.), `operations`, `webhooks`.
- `eslint.config.js` — `src/shared/api/openapi.d.ts` добавлен в ignores-массив (strict-правила не применимы к auto-generated output).
- `.prettierignore` уже содержал этот файл (с FE-TASK-005).
- `src/shared/api/index.ts` **не тронут** — остаётся `export {};` под http-клиент в FE-TASK-012.

**Ключевые решения / отклонения от acceptance criteria:**
- **openapi-typescript 7.x вместо 6.x из §20.5.** Причина: v7 — актуальная major, активно поддерживается, лучшая обработка nullable/oneOf в OpenAPI 3.0.3, исправлены баги. CLI-опция `-o` совместима. Критерий «актуальная версия» приоритетнее pin. Обновил §20.5 `^6.7.0` → `^7.13.0`.
- **CI-gate через `git diff --exit-code` вместо husky.** Причина: husky появится в FE-TASK-006. npm-lifecycle `prepare` — безопасный эквивалент для локальной разработки. В FE-TASK-006 husky переопределит `prepare` на `husky install` — тогда `gen:api` нужно переместить в `postinstall` или `.husky/post-merge`.
- **Не реэкспортил типы в `src/shared/api/index.ts`.** Потребители импортируют напрямую: `import type { components, paths } from '@/shared/api/openapi'` (соответствует §20.4a строка 1565). Index.ts останется под http-клиент FE-TASK-012.
- Subagents: **code-architect** (план-консультация: версия пакета, подход к CI-gate, политика reexport, eslint ignore).

**Верификация (все test_steps задачи):**
- Шаг 1 ✓: `npm run gen:api` — `openapi-typescript 7.13.0 🚀 api-specification.yaml → src/shared/api/openapi.d.ts [51.8ms]`, файл 1834 строки.
- Шаг 2 ✓: `npx tsc --noEmit` — 0 errors (smoke-test файл с `components['schemas']['ContractList']`, `components['schemas']['ErrorResponse']`, `components['schemas']['UserProfile']`, `components['schemas']['UserPermissions']`, `paths['/auth/login']['post']`, `paths['/contracts']['get']` — все типы разрешаются).
- Шаг 3 ✓: импорт `type ErrorResponse = components['schemas']['ErrorResponse']` работает — проверено в smoke-файле (удалён после проверки).

**Дополнительно проверено:**
- `npx eslint . --max-warnings=0` — 0 errors, 0 warnings.
- `npx prettier --check .` — All matched files use Prettier code style.
- `npx vite build` — dist/ 142.58 kB / 45.77 kB gzip, 30 modules, без ошибок.
- Makefile в Frontend отсутствует — этап N/A (как и в FE-TASK-007).

**CI-gate поведение:**
- В CI: `npm ci` авто-триггерит `prepare` → регенерирует openapi.d.ts; отдельный job `npm run gen:api:check` падает, если committed-версия рассинхронизирована со свежей регенерацией (fail-fast).
- Локально: `npm install` автоматически обновит файл при изменении спеки — разработчик не забудет регенерировать.

**Заметки для следующих итераций:**
- FE-TASK-012 (axios HTTP-клиент): `import type { components } from '@/shared/api/openapi'` — `ErrorResponse`, `UserProfile`, `UserPermissions`, `ValidationFieldError`. OrchestratorError оборачивает `components['schemas']['ErrorResponse']`.
- FE-TASK-013 (TanStack Query + qk): query-keys ссылаются на схемы из openapi (ContractList, ContractDetails, VersionDetails и т.д.).
- FE-TASK-014 (error catalog): `ErrorCode` enum извлекается из `components['schemas']['ErrorResponse']['error_code']`.
- FE-TASK-006 (Husky + lint-staged): заменить `prepare: npm run gen:api` на `prepare: husky` + перенести `gen:api` в `postinstall` ИЛИ `.husky/post-merge` (auto-regen при pull с изменением спеки).
- FE-TASK-010 (GitHub Actions CI): добавить отдельный job `schema-check` — `npm ci && npm run gen:api:check` (независим от lint/test).
- FE-TASK-009 (Dockerfile): spec-источник `../ApiBackendOrchestrator/architecture/api-specification.yaml` лежит вне `Frontend/`. При build сгенерированный openapi.d.ts уже зафиксирован в VCS — docker build не нуждается в копировании спеки.

**Затронутые файлы:**
- `Frontend/src/shared/api/openapi.d.ts` (new, 1834 строки, auto-generated)
- `Frontend/package.json` (modified: +3 scripts, +1 devDep openapi-typescript@^7.13.0)
- `Frontend/package-lock.json` (+24 пакета)
- `Frontend/eslint.config.js` (modified: +1 entry в ignores)
- `Frontend/architecture/high-architecture.md` (§20.5: openapi-typescript ^6.7.0 → ^7.13.0)

---

## FE-TASK-017 — Tailwind CSS 3.4 + tokens.css из Figma по §8.2 (2026-04-16)

**Статус:** done
**Категория:** design-system
**Приоритет:** critical

**План:**
1. Консультации с code-architect (tooling/config) + ui-designer (semantic scope).
2. Установить tailwindcss@^3.4 + postcss@^8 + autoprefixer@^10.
3. Создать `postcss.config.cjs` (CJS из-за type=module).
4. Создать `tailwind.config.ts` в Frontend/root — content scan + полный маппинг токенов через var(--...).
5. Создать `src/app/styles/tokens.css` — 1:1 §8.2 + extension-блок (shadow-lg + focus-ring-*).
6. Создать `src/app/styles/reset.css` в @layer base — project-specific base стили.
7. Создать `src/app/styles/index.css` — агрегатор: @import tokens + @import reset + @tailwind base/components/utilities.
8. Обновить `src/main.tsx` — импорт index.css.
9. Обновить `src/App.tsx` — временная тестовая разметка с bg-brand-500 и т.п. для acceptance (будет снята в FE-TASK-030).
10. Финальные проверки: typecheck/lint/prettier/build/dev.

**Что сделано:**
- `postcss.config.cjs` — минимальный конфиг `{plugins: {tailwindcss:{}, autoprefixer:{}}}`. **CJS** обязателен: package.json `type=module` → `.js` трактуется как ESM, PostCSS-loader требует CommonJS.
- `tailwind.config.ts` (Frontend root, как канон): `content: ['./index.html', './src/**/*.{ts,tsx}']`, `darkMode: 'class'`, в `theme.extend`: colors (brand 50/500/600, risk high/medium/low, fg/fg.muted, bg/bg.muted, border, success/warning/danger), fontFamily.sans, borderRadius sm/md/lg, boxShadow sm/md/lg, spacing 1..6/8/10/12, ringColor/ringWidth/ringOffsetWidth — всё через `var(--…)`. `satisfies Config` для типобезопасности.
- `src/app/styles/tokens.css` — 1:1 §8.2: 13 color-переменных (brand-50/500/600, fg, fg-muted, bg, bg-muted, border, success, warning, danger, risk-high/medium/low), font-sans, radii sm/md/lg, shadow sm/md, spacing 1..12. Extension-блок: --shadow-lg + --focus-ring-color (brand @60%) / --focus-ring-width 2px / --focus-ring-offset 2px.
- `src/app/styles/reset.css` — правила в `@layer base`: font-family/color/bg из токенов на html,body + `#root{min-height:100vh}` + font-smoothing. Решение @layer base — потому что @import после @tailwind директивы ломает CSS-спек (Vite ругается).
- `src/app/styles/index.css` — агрегатор: `@import './tokens.css'; @import './reset.css'; @tailwind base; @tailwind components; @tailwind utilities;`. Tailwind перетасовывает @layer base правила в нужный порядок: Preflight → project reset → components → utilities.
- `src/main.tsx` — добавлена строка `import './app/styles/index.css';` (авто-сортировка simple-import-sort поставила её первой).
- `src/App.tsx` — временная тестовая разметка с тремя блоками (`bg-brand-500`, `bg-risk-high`, `bg-bg-muted` + `text-fg-muted`/`border-border`). Это визуальная верификация acceptance-критерия; будет снесена в FE-TASK-030.
- `package.json` — +3 devDeps: tailwindcss@^3.4.19, postcss@^8.5.10, autoprefixer@^10.5.0. Итого +60 пакетов.
- Удалён устаревший `src/app/styles/.gitkeep` (заменён реальным содержимым).

**Ключевые решения / отклонения от acceptance criteria:**
- **tokens.css содержит 4 extension-переменные сверх §8.2** (--shadow-lg + 3 --focus-ring-*). Acceptance требует "1:1 с §8.2", но extensions выделены комментарным блоком `=== Extensions beyond §8.2 ===`, не подменяют базовые переменные и обоснованы: shadow-lg нужен для модалок/dropdown (FE-TASK-020), focus-ring-* — WCAG 2.1 AA. Одобрено ui-designer + code-reviewer (shippable, рекомендовано зафиксировать sync-rule в ADR-FE-09).
- **postcss.config.cjs (не .js)**. Причина: `"type":"module"` в package.json → Node парсит .js как ESM, PostCSS требует `module.exports`. Канон для Vite + ESM.
- **Project reset через `@layer base`**. Первая версия делала `@import './reset.css'` ПОСЛЕ `@tailwind base` — Vite выдал warning "@import must precede all other statements". Решение: reset.css → `@layer base { ... }`, оба @import в index.css перед @tailwind-директивами. Tailwind при компиляции собирает @layer base-правила в base-слой, сохраняя порядок.
- **darkMode: 'class'** включён без dark-токенов. Zero bundle cost без `dark:*` классов. Cheap insurance на случай тёмной темы в v1.x. Согласовано с code-architect.
- **Полная интеграция токенов в tailwind.config** (не только colors как показано в §8.2 фрагменте, но и radii/shadow/spacing/font/ring). §8.2-ts-пример иллюстрирует маппинг palette — не исчерпывающий список. Без этого компоненты FE-TASK-019 не смогут пользоваться токенами единообразно.
- **cva/clsx/tailwind-merge НЕ установлены в этой задаче**. §20.5 их pin'ит, но они — зависимость для shared/ui-примитивов. Отложено в FE-TASK-019 (per code-architect scope-advice).
- **Font `Inter`/`PT Root UI` — только в token value**, без self-host/@fontsource — отдельная задача типографики. Fallback `system-ui, sans-serif` ок для v1 baseline.
- **Subagents использованы:** code-architect (план-консультация: .ts vs .js, postcss.cjs, @layer base, darkMode cheap insurance, cva deferral), ui-designer (semantic scope: semantic aliases отложить, focus-ring добавить, shadow-lg добавить, typography/disabled default), code-reviewer (финал: ship it, 0 merge-blockers, 2 non-blocking nit — ADR-note + spacing-policy).

**Верификация (все test_steps задачи):**
- Шаг 1 ✓: `npm run dev` — VITE v5.4.21 ready в 175 ms на http://localhost:5173/, без compile errors. Разметка `<div className='bg-brand-500 ...'>` присутствует в App.tsx (визуальную проверку оранжевого рендера пользователь делает вручную — CLI не открывает браузер).
- Шаг 2 ✓: `grep -o '#f55e12' dist/assets/index-*.css` — 1 match (токен присутствует; Tailwind утилита `bg-brand-500` сгенерирована content-scan-ом). DevTools `background-color: rgb(245, 94, 18)` — ручная проверка за пользователем.
- Шаг 3 ✓: Tailwind-утилиты `bg-risk-high`, `text-fg-muted`, `border-border` использованы в App.tsx и не дают compile-ошибок; ESLint + typecheck clean.

**Дополнительно проверено:**
- `npm run typecheck` — 0 errors.
- `npm run lint --max-warnings=0` — 0 errors, 0 warnings.
- `npx prettier --check .` — clean.
- `npx vite build` — dist/ 143.08 kB JS / 7.36 kB CSS (gzip: 45.96 / 2.15), без warnings после @layer base-рефакторинга.
- Makefile в Frontend отсутствует — этап N/A (как и в прежних задачах).

**Соответствие архитектуре:**
- §8.2 tokens 1:1 (+ documented extensions).
- §20.5 tailwindcss pin ^3.4.0 — соблюдён (^3.4.19 удовлетворяет диапазон).
- §3 FSD layout — файлы в src/app/styles/.
- §8.6 Breakpoints — default Tailwind sm/md/lg/xl/2xl соответствуют §8.6 (640/768/1024/1280/1440); override не нужен.

**Заметки для следующих итераций:**
- FE-TASK-018 (Storybook): подключить `src/app/styles/index.css` в `.storybook/preview.ts` для глобальных токенов. Tailwind content-glob (`src/**/*.{ts,tsx}`) уже покрывает `*.stories.tsx`.
- FE-TASK-019 (UI-примитивы): установить `class-variance-authority@^0.7`, `clsx@^2.1`, `tailwind-merge@^2.3` (§20.5). Использовать default `ring`-утилиту: `focus-visible:ring focus-visible:ring-offset-2` автоматически возьмёт наши токены.
- FE-TASK-019 — принять spacing-policy: разрешены только токенизированные ключи (1..6/8/10/12) + `px`/`0`/`0.5`. Без правила — риск рассинхрона px-токенов и rem-default-шкалы. Можно ESLint-плагином tailwindcss или документацией в CONTRIBUTING (FE-TASK-056).
- FE-TASK-030 (App shell): заменить тестовую разметку в `src/App.tsx` на композицию providers; перенести `App.tsx` в `src/app/App.tsx`; удалить `src/App.tsx` из `boundaries/ignore` в eslint.config.js.
- ADR-FE-09 (token pipeline): задокументировать extension-блок в tokens.css (shadow-lg + focus-ring-*) и правило синхронизации — при рассинхронизации обновляем §8.2, а не tokens.css.
- Follow-up (отдельный task/ADR): self-hosted Inter через `@fontsource/inter` — сейчас fallback на `system-ui`.

**Затронутые файлы:**
- `Frontend/postcss.config.cjs` (new)
- `Frontend/tailwind.config.ts` (new)
- `Frontend/src/app/styles/tokens.css` (new)
- `Frontend/src/app/styles/reset.css` (new)
- `Frontend/src/app/styles/index.css` (new)
- `Frontend/src/main.tsx` (modified: +1 import)
- `Frontend/src/App.tsx` (modified: заглушка → тестовая разметка с tailwind-утилитами)
- `Frontend/src/app/styles/.gitkeep` (deleted)
- `Frontend/package.json` (modified: +3 devDeps)
- `Frontend/package-lock.json` (+60 пакетов)

---
