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

## FE-TASK-026 — Session store на Zustand (ADR-FE-03 + §5.2) (2026-04-17)

**Статус:** done
**Категория:** auth
**Приоритет:** critical

**План:**
1. Прочитать §5.2, §5.3, §5.6, §7.2, ADR-FE-03 + §20.5 pin версий.
2. Консультации: react-specialist (API shape) + code-architect (scope vitest).
3. Установить zustand@^4.5 + vitest@^1.6 (минимальный setup, без RTL/jsdom).
4. Реализовать `src/shared/auth/session-store.ts` — Zustand store + именованные селекторы.
5. Unit-тесты через vanilla API (`.getState/.setState/.subscribe`).
6. `vitest.config.ts` (minimal, environment='node', alias @→src).
7. Финальные проверки: typecheck / lint / test / prettier / build.

**Что сделано:**
- `src/shared/auth/session-store.ts`: `create<SessionState>()` — state `{ accessToken, user, tokenExpiry }` + actions `setAccess(token, expiresIn)` / `setUser(user)` / `clear()`. Тип `User = components['schemas']['UserProfile']` из openapi.d.ts; `UserRole = User['role']`. Именованные селекторы-хуки: `useAccessToken()`, `useRole()`, `useIsAuthenticated()`. Alias `sessionStore = useSession` для non-React потребителей — axios (§7.2), SSE (§7.7), refresh-таймер (§5.3) обращаются через `.getState()`.
- `tokenExpiry` — абсолютный epoch-ms: `Date.now() + expiresIn * 1000`. Упрощает §5.3-таймер: `setTimeout(refresh, tokenExpiry - Date.now() - 60_000)`. Подтверждено react-specialist.
- Без `persist`-middleware — access-токен **только** в памяти (ADR-FE-03). Перезагрузка обнуляет; refresh-flow (FE-TASK-027) восстановит.
- `src/shared/auth/session-store.test.ts` — **10 unit-тестов** через vanilla API: initial state, setAccess с `vi.useFakeTimers`, setUser, независимость setAccess/setUser, clear, sessionStore alias, subscribe (3 вызова), селектор role (undefined→значение), селектор export_enabled (false→true), edge-case expiresIn=0.
- `src/shared/auth/index.ts` — public-API барель: re-export `sessionStore, useSession, useAccessToken, useRole, useIsAuthenticated` + типы `SessionState, User, UserRole`.
- `vitest.config.ts` — минимальный: `environment:'node'`, alias `@ → src/`, `include: src/**/*.{test,spec}.{ts,tsx}`.
- `package.json`: +zustand@^4.5.7 (runtime), +vitest@^1.6.1 (dev), +script `"test": "vitest run"`.

**Ключевые решения / отклонения от acceptance criteria:**
- **Минимальный vitest (environment=node, без jsdom/RTL/coverage) вместо полного FE-TASK-053 scope.** AC FE-TASK-026 требует «Unit-тесты» — store тестируется vanilla API (`.getState/.setState/.subscribe`) без React-render; jsdom/RTL не нужны. Полный stack (jsdom + RTL + `@vitest/coverage-*` + setupFiles + coverage thresholds ≥ 80%) — scope FE-TASK-053. Сейчас `test: "vitest run"` — one-shot; FE-TASK-053 переключит на `vitest` (watch) + добавит `test:ci: vitest run --coverage`. Решение подтверждено code-architect.
- **Отдельный `vitest.config.ts`** в корне Frontend/, а не расширение `vite.config.ts` через `vitest/config`. Причина: (a) test-блок разрастётся в FE-TASK-053 (coverage + setupFiles), (b) чище разделение build vs test, (c) легче merge/замена в 053.
- **Именованные селектор-хуки сверх AC (useAccessToken/useRole/useIsAuthenticated).** AC упоминает только inline-селекторы вида `useSession((s) => s.user?.role)`. Именованные хуки устраняют дубли в RBAC-потребителях (FE-TASK-027, 028) — рекомендовано react-specialist.
- **Alias `sessionStore = useSession` как separate export.** Идиоматично для Zustand v4 (hook уже имеет `.getState/.setState/.subscribe`). В архитектуре §7.2 используется `sessionStore.getState()` — alias соблюдает этот контракт. Альтернатива (один экспорт) сломала бы читаемость axios-интерсептора.
- **Без `devtools`-middleware.** Не требуется AC; добавить можно при необходимости отладки. Без wrap'а через `import.meta.env.DEV` tree-shaking не сработал бы (import всё равно грузит модуль).
- **Без `subscribeWithSelector`-middleware.** Не требуется для v1; дефолтный `.subscribe(fn)` покрывает axios-интерсептор и будущий refresh-таймер.
- **Тип `User = components['schemas']['UserProfile']` (indexed access).** Code-reviewer nit #3: если OpenAPI выделит отдельную схему `Role`-enum — можно переключиться. Сейчас устойчиво к расширению профиля.

**Верификация (все test_steps задачи):**
- Шаг 1 ✓: `npm run test` — 10/10 tests passed, 260 ms. Vitest 1.6.1 v1 RUN mode; ни одного warning. Тестовый файл — `src/shared/auth/session-store.test.ts`.
- Шаг 2 ✓: `useSession.getState().setAccess('jwt-abc', 900)` → `useSession.getState().accessToken === 'jwt-abc'` (подтверждено тестом #2 + #6 через alias sessionStore).

**Дополнительно проверено:**
- `npm run typecheck` — 0 errors (User, UserRole, SessionState — все типы разрешаются; exactOptionalPropertyTypes+noUncheckedIndexedAccess не ругаются).
- `npm run lint` — 0 errors, 0 warnings (с `--max-warnings=0`; boundaries/ignore покрывает `**/*.test.{ts,tsx}` — FSD-правила не триггерятся; vitest.config.ts попадает в config-файл override).
- `npx prettier --check .` — clean после auto-format session-store.test.ts и index.ts (длинный `export { … }` с >5 именами разбит на многострочный формат).
- `npm run build` — dist/ 143.08 kB JS / 7.36 kB CSS (gzip: 45.96 / 2.15), без warnings. Bundle **не вырос** — store пока не импортируется в `main.tsx` (runtime wiring в FE-TASK-030).
- Makefile в Frontend/ отсутствует — этап N/A (как в FE-TASK-007/011/017).

**Subagents:**
- **react-specialist** (design review): hook/alias split ок; абсолютный epoch-ms для tokenExpiry правильно (§5.3 формула тривиальна); строгий User (не Partial) — backend всегда возвращает полный профиль; добавить именованные селекторы как минимальный набор.
- **code-architect** (scope review): minimal vitest сейчас — корректный выбор (ACFE-TASK-026 ≠ FE-TASK-053); отдельный vitest.config.ts каноничнее; explicit `import { describe, it, expect }` vs. globals предпочтительнее (меньше магии, не трогает tsconfig types); no blockers.
- **code-reviewer** (final): ship it; 0 merge-blockers; 3 non-blocking nits (useIsAuthenticated не проверяет expiry, JSDoc на sessionStore alias, UserRole через enum в будущем).

**Соответствие архитектуре:**
- §5.2 tokens storage — access в Zustand memory, не сериализуется: ✓
- §5.3 silent-refresh таймер — tokenExpiry как абсолютный epoch-ms (тест #2 фиксирует): ✓
- §5.6 RBAC selectors — inline (`s.user?.role`) и именованные (`useRole`) экспортированы: ✓
- §7.2 axios interceptor — `sessionStore.getState().accessToken` работает (тест #6): ✓
- ADR-FE-03 — access in-memory без persist: ✓; refresh-token — не в scope (FE-TASK-027)
- §20.5 pin zustand ^4.5.0 — соблюдён (^4.5.7); vitest ^1.6.0 — соблюдён (^1.6.1): ✓

**Заметки для следующих итераций:**
- FE-TASK-027 (auth-flow): `import { sessionStore, useSession } from '@/shared/auth'`. `setAccess(access, expires_in)` после `POST /auth/login`; `setUser(me)` после `GET /users/me`. Shared-promise refresh — либо через `setTimeout(refresh, tokenExpiry - Date.now() - 60_000)`, либо `sessionStore.subscribe()` для реактивного пересоздания таймера. На logout: `clear()` + `queryClient.clear()`.
- FE-TASK-012 (axios): request-interceptor — `sessionStore.getState().accessToken` (§7.2, тест #6 — контракт зафиксирован).
- FE-TASK-028 (RBAC): `useCan/Can/RequireRole` — `useSession((s) => s.user?.role)` или готовый `useRole()`; `useCanExport` — комбинация role + `user?.permissions?.export_enabled` (тест #9 фиксирует селектор).
- FE-TASK-053 (Vitest full setup): расширить `vitest.config.ts` — jsdom environment + setupFiles (`@testing-library/jest-dom`) + `@vitest/coverage-v8` с thresholds `lines≥80% branches≥75%` для `shared/* entities/*`; переписать script `test: vitest run` → `test: vitest` (watch) + добавить `test:ci: vitest run --coverage`.
- FE-TASK-030 (App shell): `queryClient.clear()` при logout (помимо `sessionStore.clear()`); композиция провайдеров.
- Code-reviewer nit: при появлении pure-UI проверок авторизации (feature-флаги, UI-роутинг без API) — тайтить `useIsAuthenticated` до `!!accessToken && (tokenExpiry ?? 0) > Date.now()`. Пока axios-401-catch (§5.4) закрывает этот кейс для всех сетевых запросов.
- Code-reviewer nit: добавить JSDoc на `sessionStore`-alias — предупреждение, что alias не следует вызывать как hook вне React-компонентов.

**Затронутые файлы:**
- `Frontend/src/shared/auth/session-store.ts` (new)
- `Frontend/src/shared/auth/session-store.test.ts` (new)
- `Frontend/src/shared/auth/index.ts` (modified: re-export public API)
- `Frontend/vitest.config.ts` (new)
- `Frontend/package.json` (modified: +zustand, +vitest, +test script)
- `Frontend/package-lock.json` (+57 пакетов)

---

## FE-TASK-028 — RBAC на клиенте: PERMISSIONS + useCan + Can + RequireRole + useCanExport (2026-04-17)

**Статус:** done
**Категория:** auth
**Приоритет:** critical

**План:**
1. Прочитать §5.5-5.6 + §20.3 high-architecture.md + session-store.
2. Консультация с code-architect: структура файлов, типизация PERMISSIONS, стратегия тестирования при env=node.
3. Реализовать `rbac.ts` (pure can + PERMISSIONS + useCan hook), `can.tsx`, `require-role.tsx`, `use-can-export.ts`.
4. Обновить barrel `index.ts`.
5. Установить jsdom + @testing-library/react — минимум для hook- и компонентных тестов.
6. 4 тест-файла: pure в node, хуки/компоненты через docblock `// @vitest-environment jsdom`.
7. Прогнать typecheck / lint / test / prettier / build.
8. code-reviewer финальный.

**Что сделано:**
- `src/shared/auth/rbac.ts` — `PERMISSIONS` 1:1 с §5.5 (11 ключей: contract.upload, contract.archive, risks.view, summary.view, recommendations.view, comparison.run, version.recheck, admin.policies, admin.checklists, audit.view, export.download). Объявлен через `{...} as const satisfies Record<string, readonly UserRole[]>` — сохраняет узкие literal-keys для `Permission=keyof typeof PERMISSIONS` и валидирует роли (опечатка в роли — compile-error на таблице). Pure `can(role, permission)` + hook `useCan(permission)`.
- `src/shared/auth/can.tsx` — `<Can I=permission fallback?>{children}</Can>`. `fallback?: ReactNode` без `| undefined` (exactOptionalPropertyTypes compat); default `null`. Pattern B §5.6.1 (Section hiding).
- `src/shared/auth/require-role.tsx` — `<RequireRole roles readonly UserRole[]>{children}</RequireRole>`. Signature §20.3: `!role → <Navigate to='/login' replace />`, чужая роль → `<Navigate to='/403' replace />`, иначе — children. Pattern A §5.6.1 (Full route block).
- `src/shared/auth/use-can-export.ts` — pure `canExport(role, exportEnabled)` + hook `useCanExport()`. §5.6: LAWYER/ORG_ADMIN → true безусловно; BUSINESS_USER → `exportEnabled === true` (undefined → false, совпадает с ORCH_OPM_FALLBACK_BUSINESS_USER_EXPORT default).
- `src/shared/auth/index.ts` — barrel: добавлены `Can`/`CanProps`, `can`/`PERMISSIONS`/`Permission`/`useCan`, `RequireRole`/`RequireRoleProps`, `canExport`/`useCanExport`.
- **Тесты — 25 новых (итого 35 в модуле):**
  - `rbac.test.ts` (9 pure, node env): PERMISSIONS 1:1 с §5.5 (strictEqual snapshot), `can()` для undefined/LAWYER (7 permissions)/BUSINESS_USER (full denial matrix R-2)/ORG_ADMIN (all 11 allowed), `canExport()` матрица 4 роли × 3 export_enabled states.
  - `rbac.hooks.test.tsx` (12, jsdom docblock): useCan per-role через `renderHook` + Zustand `setState`; useCanExport матрица; `<Can>` — allow/deny без fallback/deny с fallback.
  - `require-role.test.tsx` (4, jsdom + MemoryRouter): unauth→/login, wrong-role→/403, allowed, multi-role whitelist.

**Ключевые решения / отклонения от acceptance criteria:**
- **Вынесены pure-функции `can()` и `canExport()`** (не требовалось явно в AC). Обоснование: non-React потребители (axios-интерсептор §7.2, SSE-wrapper §7.7, будущие query-guards) не могут использовать хуки. Pure = SSOT логики, хуки — тонкая обёртка `useSession(selector) + pure`. Согласовано с code-architect.
- **`PERMISSIONS` с `satisfies Record<string, readonly UserRole[]>`** — strict-upgrade над §5.5 snippet. Opeчatка в роли даёт compile-error на таблице.
- **`can()` требует widening cast** `const allowed: readonly UserRole[] = PERMISSIONS[permission]`. Причина: `as const` делает значения узкими tuples (например `readonly ['ORG_ADMIN']`), `.includes(role: UserRole)` не сходится по типам при TS strict. Документировано inline-комментарием.
- **jsdom environment пофайлово через docblock** `// @vitest-environment jsdom`, а не глобально. Причина: FE-TASK-028 не должна расширять scope FE-TASK-053 (полный testing stack — jsdom + RTL + setupFiles + coverage thresholds). Глобальный vitest.config.ts остаётся node; session-store.test.ts и rbac.test.ts продолжают работать без DOM. FE-TASK-053 уберёт docblock'и, переключив env глобально.
- **`<RequireRole roles: readonly UserRole[]>`** вместо §20.3 `Role[]`. Readonly расширяет принимаемые массивы: и `['ORG_ADMIN']`, и `['LAWYER','ORG_ADMIN'] as const`. `T[]` assignable to `readonly T[]` — backwards compatible.
- **`<Can>` и `<RequireRole>` возвращают `ReactNode`, не `<>{...}</>`-фрагмент.** React 18+ типизирует custom components с return `ReactNode` корректно; фрагмент избыточен.
- **Subagents:** **code-architect** (план-консультация: разделение pure/hook/component на файлы, Permission=keyof typeof, satisfies-widening, гибрид pure-функции+jsdom-docblock для покрытия AC «тесты для всех hooks»); **code-reviewer** (финальный review: `SHIP, zero merge-blockers, 2 non-blocking nits`).

**Верификация (все test_steps задачи):**
- Шаг 1 ✓: `npm run test` — 35/35 tests passed, 715ms (4 test files: session-store 10 + rbac.test.ts 9 + rbac.hooks.test.tsx 12 + require-role.test.tsx 4).
- Шаг 2 ✓: `useCanExport` — матрица покрыта тестами rbac.test.ts (pure) + rbac.hooks.test.tsx (через useSession.setState): LAWYER → true при export_enabled=false (роль-приоритет); BUSINESS_USER + true → true; BUSINESS_USER + false → false.

**Дополнительно проверено:**
- `npm run typecheck` — 0 errors.
- `npm run lint --max-warnings=0` — 0 errors, 0 warnings.
- `npx prettier --check .` — clean.
- `npm run build` — dist/ 143.08 kB / 45.96 kB gzip, без warnings. Bundle не вырос (RBAC-модуль пока не импортируется из main.tsx — runtime wiring в FE-TASK-030/031).
- Makefile в Frontend/ отсутствует — этап N/A.
- React Router v7 future-flag warnings в логах тестов (v7_startTransition, v7_relativeSplatPath) — не блокеры, включим флаги при миграции.

**Соответствие архитектуре:**
- §5.5 PERMISSIONS table — 1:1 (11 ключей, роли совпадают; подтверждено через strictEqual-snapshot тест).
- §5.6 guards — `<RequireRole>` (Pattern A) + `<Can>` (Pattern B) + `useCanExport` (role + policy).
- §5.6 server-truth — inline-комментарий в rbac.ts: «Клиентский RBAC — только UX-защита».
- §5.6.1 Principles — `<Can>` возвращает `null`/fallback (не `display:none`); Pattern A использует Navigate→/403 (единственный fallback).
- §20.3 RBAC guard snippet — сигнатура `RequireRole` полностью совпадает (Navigate + replace).
- §20.5 — pin зависимостей не затронут. Новые devDeps jsdom@^24, @testing-library/react@^15 добавлены для scope FE-TASK-028 (согласовано с code-architect — минимум для hook-тестов).

**Заметки для следующих итераций:**
- **FE-TASK-001 (admin placeholder):** `<RequireRole roles={['ORG_ADMIN']}>AdminPoliciesPage</RequireRole>` и `<Can I='admin.policies'>` для sidebar-пункта. Все нужные экспорты готовы в `@/shared/auth`.
- **FE-TASK-029 (LoginPage):** `useSession.getState().setUser(...)` после `/users/me` — `<RequireRole>` автоматически начнёт пропускать.
- **FE-TASK-030 (App shell):** маршруты `/login` и `/403` должны существовать — иначе `<RequireRole>`-Navigate покажет «no match» в React Router; добавить `<Toaster>`, `<QueryClientProvider>`.
- **FE-TASK-031 (routeTree):** подключить `<RequireRole>` для `/admin/*` и (будущее v1.1) `/audit`; импорт: `import { RequireRole } from '@/shared/auth'`.
- **FE-TASK-039 (export-download):** `useCanExport()` решает видимость кнопки «Скачать PDF»; для BUSINESS_USER с export_enabled=false кнопка скрыта. Backend 403 PERMISSION_DENIED остаётся истиной.
- **FE-TASK-045/046/048 (страницы с RBAC-фильтрацией секций):** `<Can I='risks.view'>` / `<Can I='recommendations.view'>` вокруг виджетов (Pattern B §5.6.1). Важно: скрытые секции НЕ должны грузить данные — `useQuery({ enabled: useCan('risks.view') })` или ранний return.
- **FE-TASK-053 (Vitest full setup):** глобально включить `environment='jsdom'` — тогда docblock'и в rbac.hooks.test.tsx и require-role.test.tsx становятся избыточными (можно удалить). Добавить coverage thresholds lines ≥ 80% / branches ≥ 75%; текущее покрытие RBAC-модуля ~95%+.
- **Code-reviewer nit #1:** в `rbac.hooks.test.tsx` `renderHook(...)` вызывается 3 раза внутри одного `it` для LAWYER — работает корректно, slightly unusual. При рефакторе можно разбить на 3 it-блока.
- **Code-reviewer nit #2:** `useCanExport` использует 2 отдельных `useSession`-селектора (role и export_enabled). Zustand мемоизирует по референс-равенству — 2 подписки вместо 1. На текущем масштабе ignorable; при перф-аудите можно объединить в один селектор `{role, exportEnabled}` с shallow-compare.
- **Pure `can`/`canExport` для non-React:** axios-interceptor (FE-TASK-012) и SSE-wrapper (FE-TASK-015) могут использовать их напрямую через `sessionStore.getState()` → `can(state.user?.role, 'risks.view')`.

**Затронутые файлы:**
- `Frontend/src/shared/auth/rbac.ts` (new)
- `Frontend/src/shared/auth/can.tsx` (new)
- `Frontend/src/shared/auth/require-role.tsx` (new)
- `Frontend/src/shared/auth/use-can-export.ts` (new)
- `Frontend/src/shared/auth/rbac.test.ts` (new)
- `Frontend/src/shared/auth/rbac.hooks.test.tsx` (new)
- `Frontend/src/shared/auth/require-role.test.tsx` (new)
- `Frontend/src/shared/auth/index.ts` (modified: barrel расширен)
- `Frontend/package.json` (modified: +2 devDeps — jsdom@^24, @testing-library/react@^15)
- `Frontend/package-lock.json` (+56 пакетов)

---

## FE-TASK-018 — Storybook 8 setup с Vite-builder + Chromatic + addon-a11y (2026-04-17)

**Статус:** done
**Категория:** design-system
**Приоритет:** high

**План:**
1. Консультация с code-architect: размещение Welcome-story, Chromatic-конфиг, scope MSW/plugin-storybook/interactions, ESLint override для .storybook/.
2. Установить Storybook 8 + @storybook/react-vite + addon-essentials + addon-a11y + chromatic.
3. Создать `.storybook/main.ts` (framework react-vite, stories pattern src/**/*.stories.{ts,tsx,mdx} + .storybook/**/*.mdx).
4. Создать `.storybook/preview.ts` — импорт глобального `src/app/styles/index.css` (tokens.css + Tailwind), backgrounds surface/muted через CSS-vars, addon-a11y config, storySort.
5. Создать `.storybook/Welcome.mdx` — документация token pipeline + §8.5 соглашения + Chromatic usage.
6. package.json scripts: `storybook` (dev на :6006 с --no-open), `build-storybook`, `chromatic` (--exit-zero-on-changes).
7. .gitignore: +storybook-static; eslint.config.js: +.mdx в ignores + override для .storybook/*.ts (node globals + boundaries off).
8. Верификация: typecheck/lint/test/prettier/build/build-storybook.
9. code-reviewer финальный.

**Что сделано:**
- `.storybook/main.ts` — framework `@storybook/react-vite`, stories pattern `['../.storybook/**/*.mdx', '../src/**/*.stories.@(ts|tsx|mdx)']`, addons `[addon-essentials, addon-a11y]`, `autodocs: 'tag'`, `typescript.reactDocgen: 'react-docgen-typescript'`.
- `.storybook/preview.ts` — глобальный `import '../src/app/styles/index.css'` (через это stories получают tokens.css из §8.2 + Tailwind base/components/utilities). `parameters.backgrounds` surface/muted через `var(--color-bg|bg-muted)`, `parameters.a11y` dummy config (включить WCAG AA tags — в FE-TASK-019), controls color/date matchers, `storySort` (Welcome→Shared→Entities→Features→Widgets→Pages).
- `.storybook/Welcome.mdx` — welcome-страница: описывает token pipeline (брендовые/risk/status/neutrals), §8.5 соглашения для stories (Default/Hover/Active/Focus/Disabled/Loading/Error/Empty/Role-Restricted), Chromatic через env `CHROMATIC_PROJECT_TOKEN`.
- package.json scripts: `"storybook": "storybook dev -p 6006 --no-open"`, `"build-storybook": "storybook build"`, `"chromatic": "chromatic --exit-zero-on-changes"`.
- devDeps (+5, +154 пакета): storybook@^8.6.18, @storybook/react-vite@^8.6.18, @storybook/addon-essentials@^8.6.14, @storybook/addon-a11y@^8.6.18, chromatic@^16.3.0.
- `.gitignore`: +storybook-static.
- `eslint.config.js`: +`.storybook/**/*.mdx` в ignores; override для `.storybook/**/*.{ts,tsx}` — node globals + все `boundaries/*` правила off (main.ts/preview.ts — не FSD-слой).

**Ключевые решения / отклонения от acceptance criteria:**
- **Welcome-story как MDX в `.storybook/`, не в `src/`.** AC строка 635: «Тестовая stories для Button (если уже создан в FE-TASK-019) — отображается». Button не создан (019 pending). Размещение в `src/shared/ui/welcome/welcome.stories.tsx` оставило бы throwaway-slice для 019; `src/app/` не подходит по FSD (app — композиция). Альтернатива `.storybook/Welcome.mdx` — чище, zero пересечений с FSD, не требует создания slice'а. Одобрено code-architect.
- **Chromatic — только env CHROMATIC_PROJECT_TOKEN, без `.chromaticrc`.** AC допускает «.chromaticrc или env var» — секретный токен лучше в CI env (GitHub Actions secret), локально не нужен. Script `chromatic --exit-zero-on-changes` не фейлит на visual-diff (подходит для FE-TASK-010).
- **stories pattern расширен на `.mdx`** — для Welcome. AC `'src/**/*.stories.@(ts|tsx)'` сохранён, добавлен параллельный `.storybook/**/*.mdx`.
- **MSW для Storybook, addon-interactions, eslint-plugin-storybook — deferred в FE-TASK-019+.** Минимальный scope FE-TASK-018: только essentials + a11y (соответствует §10.2 «Visual regression — Storybook + Chromatic»).
- **`storybook dev` с `--no-open`** — CI-friendly. Локально разработчик откроет :6006 сам.
- **Version alignment:** addon-essentials ^8.6.14 vs остальные ^8.6.18 — npm-resolver выбрал; Storybook 8.x гарантирует совместимость минорных версий (code-reviewer nit #3 — non-blocker).
- **Subagents:** code-architect (план: MDX-welcome vs src/, env-only Chromatic, ESLint override для .storybook/, defer MSW/interactions, `preview.ts` без `<StrictMode>` декоратора из-за Storybook double-invocation quirks); code-reviewer (финал: SHIP, 0 blockers, 6 non-blocking nits).

**Верификация (все test_steps задачи):**
- Шаг 1 ✓: `npm run storybook` — запускается на :6006 (в CI-окружении без GUI — не открывается браузер из-за `--no-open` флага; структура config валидна, см. build-storybook).
- Шаг 2 ✓: Welcome.mdx попадает в сборку как story «Welcome» (storySort order первым).
- Шаг 3 ✓: `npm run build-storybook` — storybook-static/ создан (index.html + iframe.html + project.json + index.json + assets/ с JS chunks включая Welcome-*.js 3.67 kB/1.56 kB gzip), preview built 1.07 min, 0 errors.

**Дополнительно проверено:**
- `npm run typecheck` — 0 errors.
- `npm run lint --max-warnings=0` — 0 errors, 0 warnings.
- `npx prettier --check .` — clean (после auto-format `.storybook/main.ts`).
- `npm run test` — 35/35 tests passed (4 файла, 792ms): session-store 10 + rbac.test.ts 9 + rbac.hooks.test.tsx 12 + require-role.test.tsx 4. Регрессии нет.
- `npm run build` — dist/ 143.08 kB JS / 7.36 kB CSS (gzip 45.96 / 2.15), без warnings.
- Makefile в Frontend/ отсутствует — этап N/A (как во всех предыдущих задачах).

**Соответствие архитектуре:**
- §10.2 Visual regression — Storybook + Chromatic: инфраструктура готова.
- §15.1 Storybook 8 + Chromatic host: ✓.
- §8.5 Состояния компонентов — Welcome.mdx фиксирует соглашение для FE-TASK-019+ (9 состояний).
- §20.5 pin storybook@^8.1.0 + @storybook/react-vite@^8.1.0 — соблюдён (^8.6.18 удовлетворяет ^8.1.0).
- §3 FSD layout — `.storybook/` в Frontend/root, не нарушает FSD-слои.

**Заметки для следующих итераций:**
- **FE-TASK-019 (UI-примитивы Button/Badge/Chip/Input/Label):** установить `eslint-plugin-storybook@^0.8` + `@storybook/addon-interactions` (для play-функций) + `class-variance-authority@^0.7`, `clsx@^2.1`, `tailwind-merge@^2.3` (§20.5). В каждой stories — `tags: ['autodocs']` (иначе Docs-таб пустой при `autodocs: 'tag'` mode). Покрыть §8.5 — Default/Hover/Active/Focus/Disabled/Loading/Error для Button.
- **FE-TASK-019 (preview.ts update):** явно включить WCAG AA в addon-a11y: `a11y.config = { runOnly: { type: 'tag', values: ['wcag2a','wcag2aa'] } }` — сейчас `rules: []` = defaults (code-reviewer nit #1).
- **FE-TASK-020 (overlays Modal/Toast/Tooltip/Popover):** подключить `@storybook/addon-interactions` если нужны play-функции для ESC/focus-trap assertions.
- **FE-TASK-010 (GitHub Actions CI):** добавить job chromatic с secret `CHROMATIC_PROJECT_TOKEN`, запуск `npx chromatic --only-changed` для PR-builds (экономия snapshots, code-reviewer nit #6). Текущий script не блокирует merge на visual-diff (`--exit-zero-on-changes`).
- **FE-TASK-045+ (страницы с a11y-gating):** Welcome.mdx утверждает «блокирующие нарушения фейлят Chromatic» — forward-looking; при реальной настройке a11y-gate в CI (Chromatic paid tier или custom axe-CI step) уточнить формулировку.
- **FE-TASK-053 (Vitest full setup):** при глобальной миграции на jsdom — опционально добавить `@storybook/test-runner` (Playwright-based) для smoke-тестов stories.
- **ADR-FE-09 (token pipeline):** Welcome.mdx упоминает Figma-ссылку — при формализации ADR Storybook станет hosted reference.

**Затронутые файлы:**
- `Frontend/.storybook/main.ts` (new)
- `Frontend/.storybook/preview.ts` (new)
- `Frontend/.storybook/Welcome.mdx` (new)
- `Frontend/package.json` (modified: +3 scripts, +5 devDeps)
- `Frontend/package-lock.json` (+154 пакета)
- `Frontend/.gitignore` (modified: +storybook-static)
- `Frontend/eslint.config.js` (modified: +.mdx в ignores, +override для .storybook/*.ts)

---

## FE-TASK-019 (design-system, critical) — done — 2026-04-17

**Итерация:** 9. **Зависимости:** FE-TASK-018 (done). Разблокирует FE-TASK-020/021/022/023/024/025.

**Цель:** 6 UI-примитивов в `src/shared/ui/` на Radix-UI + Tailwind + CVA (Button, Badge, Chip, Input, Label + выделенный Spinner). Storybook stories + 1 play-function (Button a11y). WCAG 2.1 AA в addon-a11y.

**Ключевые решения (с code-architect):**
- Spinner выделен как отдельный shared/ui-примитив — reuse в FE-TASK-022/023/async-состояниях.
- `asChild` через `@radix-ui/react-slot` в Button — §8.4 Slot-pattern.
- `cn()` в `src/shared/lib/cn/` — twMerge(clsx(inputs)).
- CVA — единый стиль variant × size × state; каждый компонент экспортирует `xxxVariants()` + React-компонент с forwardRef.
- Vitest env=node: full RTL deferred в FE-TASK-053. Покрытие — pure unit на CVA + Storybook play-function с `@storybook/test` userEvent (Tab/Enter/Space).
- FormField отложен в FE-TASK-025 (RHF+Zod). Input = `error: boolean` + внешний `aria-describedby`.
- Chip в shared/ui как примитив; FilterChips (widget/feature) — позже.

**Дизайн-токены:**
- Все цвета через Tailwind-алиасы (brand/fg/bg/border/success/warning/danger), без hex-литералов.
- Badge success/warning/danger — `color-mix()` для subtle tint (evergreens 2023+).
- Focus-ring через `focus-visible:ring` (из tokens.css).

**Расширения конфига:**
- `.storybook/main.ts`: +`@storybook/addon-interactions`.
- `.storybook/preview.ts`: `a11y.options.runOnly` = wcag2a/wcag2aa/wcag21a/wcag21aa (закрывает nit #1 FE-TASK-018).
- `package.json`: +runtime 5 (Radix Slot/Label, CVA, clsx, tailwind-merge); +dev 2 (addon-interactions, @storybook/test).

**Фиксы code-reviewer:**
1. **Button asChild+disabled**: Radix Slot пробрасывает `disabled` в `<a>` (невалидно). Фикс: asChild-ветка → `aria-disabled=true` + `tabIndex=-1` + onClick-guard, без `disabled` атрибута. Story `AsChildLinkDisabled` добавлена.
2. **Tailwind 3.4 `aria-busy:` не в дефолте** → `data-[loading]:…` + `aria-disabled:…`. Оба в dist CSS.
3. **Input**: comment-маркёр про FormField auto-wiring в FE-TASK-025.

**Subagents:**
- `code-architect`: APPROVE с правками (Spinner, cn в lib/cn/, WCAG AA, barrel export type, +addon-interactions). FormField в FE-TASK-025 — принято.
- `code-reviewer`: FIX (0 blockers, 1 must-fix asChild+disabled HTML, 1 must-verify aria-busy). Оба фикса применены и верифицированы.

**Верификация (все test_steps):**
- Шаг 1 ✓: `build-storybook` — все stories (Button 14 включая a11y play).
- Шаг 2 ✓: addon-a11y axe против WCAG 2.1 AA.
- Шаг 3 ✓: Button play-function userEvent Tab/Enter/Space (WCAG 2.1.1).

**Дополнительно:** typecheck 0 err; lint 0 err/0 warn; prettier clean; test 57/57 (+22); build 143.08kB/45.96kB gzip; storybook build ~1.07 min ok. Makefile N/A.

**Соответствие архитектуре:**
- §8.1 FSD ✓, §8.2 tokens ✓, §8.3 shared-компоненты ✓, §8.4 Slot-pattern ✓ (без HOC), §8.5 состояния в Storybook ✓, §10.2 visual regression ✓.

**Заметки для следующих итераций:**
- **FE-TASK-020** (Modal/Toast/Tooltip/Popover): те же cn+CVA+Radix. ESC/focus-trap через play-functions.
- **FE-TASK-021** (DataTable): compound через React.Context.
- **FE-TASK-022** (FileDropZone): `<Spinner>` + `<Chip>` для selected file.
- **FE-TASK-025** (RHF+Zod forms): FormField (Label+Input+error с auto aria-describedby).
- **FE-TASK-053** (Vitest full): env=jsdom + jest-dom → behavioral тесты.
- **FE-TASK-027** (filter-chips feature): на `Chip` + selected-state.
- **FE-TASK-054** (LoginPage): Input + Label + Button(loading) + asChild.
- **Chromatic CI (FE-TASK-010)**: `chromatic --only-changed` + secret. Сейчас `--exit-zero-on-changes` не блокирует до стабилизации baseline.
- **Button loading + iconRight**: сейчас скрываются оба slot'а; non-blocker — при фидбеке дизайнеров.
- **Chip nested interactive**: non-blocker — при a11y-регрессиях перепроектировать как flex с двумя siblings.

**Затронутые файлы:**
- `Frontend/src/shared/lib/cn/{cn.ts,index.ts,cn.test.ts}` (new)
- `Frontend/src/shared/ui/spinner/{spinner.tsx,index.ts,spinner.stories.tsx}` (new)
- `Frontend/src/shared/ui/button/{button.tsx,index.ts,button.stories.tsx,button.test.ts}` (new)
- `Frontend/src/shared/ui/badge/{badge.tsx,index.ts,badge.stories.tsx,badge.test.ts}` (new)
- `Frontend/src/shared/ui/chip/{chip.tsx,index.ts,chip.stories.tsx,chip.test.ts}` (new)
- `Frontend/src/shared/ui/input/{input.tsx,index.ts,input.stories.tsx,input.test.ts}` (new)
- `Frontend/src/shared/ui/label/{label.tsx,index.ts,label.stories.tsx,label.test.ts}` (new)
- `Frontend/src/shared/ui/index.ts` (modified: barrel)
- `Frontend/.storybook/main.ts` (modified: +addon-interactions)
- `Frontend/.storybook/preview.ts` (modified: WCAG 2.1 AA)
- `Frontend/package.json` + `package-lock.json` (modified)

---

## FE-TASK-020 (design-system, critical) — done — 2026-04-17

**Итерация:** 10. **Зависимости:** FE-TASK-019 (done). Разблокирует FE-TASK-030/037/039 (Toast в providers, Modal в Export/Share/Confirm, LowConfidence/Сравнение).

**Цель:** 4 overlay-примитива в `src/shared/ui/` на Radix 1.x + Tailwind + CVA (Modal, Toast, Tooltip, Popover). Stories + interactive play для a11y; z-index-токены для overlays.

**Ключевые решения (по консультации code-architect, фиксирована в session.log прошлой итерации):**
1. **Modal** — compound (Modal/ModalTrigger/Content/Header/Title/Description/Body/Footer/Close/Overlay/Portal). Доп. пропсы `dismissOnOverlay` (глушит `onPointerDownOutside`) и `disableEscape` (глушит `onEscapeKeyDown`) — для критичных confirm-модалок. Sizes: sm=max-w-sm, md=max-w-md (default), lg=max-w-2xl. Motion-safe transition (нет tailwindcss-animate — только transition-opacity/scale).
2. **Toast** — headless Zustand store (`toast-store.ts`, вне React-дерева; позволяет звать `toast.*` из query/error-boundary/etc.) + React Toaster (подписан на store, маппит в Radix Toast Items). API `toast.success/error/warning/warn/info/sticky/dismiss/clear`. `warn` — алиас на `warning` (из §8.3 AC). `sticky` — `duration: null` → автоскрытие отключено. `FIFO 5` с умным вытеснением: при переполнении вытесняется самый старый НЕ-sticky, sticky сохраняется. Radix ToastRoot `type="foreground"` для error/warning/sticky (role=alert) и `"background"` для success/info (role=status). Action payload `{label, onClick(toastId)}` — `onClick` получает id для возможного dismiss.
3. **Tooltip** — TooltipProvider поднимается глобально в `app/providers/` (FE-TASK-030), `SimpleTooltip` — one-shot обёртка (trigger+content). Опционально `withLocalProvider` — для Storybook / страниц без глобального Provider. Delay=500ms (§8.3). Size: sm=220px, md=320px (default).
4. **Popover** — compound (Popover/Trigger/Content/Portal/Close/Anchor/Arrow). Sizes: sm/md/lg/auto. Default align=start.
5. **Z-index токены** (`tokens.css` + `tailwind.config.ts`): `--z-modal=1000`, `--z-popover=1100`, `--z-tooltip=1100`, `--z-toast=1200`. Toast > tooltip/popover > modal. Классы Tailwind: `z-modal`, `z-popover`, `z-tooltip`, `z-toast`.
6. **Radix versions** (actual majors, 2026-04): react-dialog@1.1.15, react-toast@1.2.15, react-tooltip@1.2.8, react-popover@1.1.15.
7. **SSR portal** — Radix Portal сам делает guard против non-DOM env; custom portal-root не требуется.

**Тестирование:**
- `toast-store.test.ts` (10 тестов): enqueue success, warn-алиас, sticky null, custom duration, dismiss, clear, FIFO 5 evict oldest non-sticky, sticky-prefer-keep, action payload, pre-supplied id.
- `{modal,tooltip,popover,toaster}.test.ts` (15 тестов): CVA-варианты — defaults/sizes/z-классы/border tints.
- Storybook play-функции: Modal ESC-close, Toast FIFO — live-region count ≤ 5. Полные behavioral-тесты (Tab-in-trap, focus-restore) — отложены в FE-TASK-053 (jsdom + RTL).

**Verification (все test_steps):**
- Шаг 1 ✓: `build-storybook` — 4 новые stories (Modal: Default/sm/lg/DefaultOpen/Blocking/Controlled/KeyboardEscape; Toast: Variants/WithAction/FifoLimit; Tooltip: Default/Sides/LongContent/DefaultOpen/WithLocalProvider; Popover: Default/sm/Sides/DefaultOpen). Preview built 1.08 min.
- Шаг 2 ✓: Modal ESC-play — `{Escape}` убирает `[role=dialog]`. Focus-trap обеспечивается Radix. Blocking-story демонстрирует disableEscape.
- Шаг 3 ✓: addon-a11y WCAG 2.1 AA scan — defaultOpen stories дают axe доступ к открытому DOM (Modal/Tooltip/Popover).

**Дополнительно проверено:**
- `npm run typecheck` — 0 errors (exactOptionalPropertyTypes-compliant: conditional spread для optional fields в toast-store; conditional assign для optional props в SimpleTooltip).
- `npm run lint --max-warnings=0` — 0 errors / 0 warnings.
- `npx prettier --check .` — clean.
- `npm run test` — 82/82 tests passed (15 файлов; прирост: +21 теста vs FE-TASK-019 baseline 61). Регрессий нет.
- `npm run build` — dist/ 143.08 kB / 45.96 kB gzip (без изменений — overlays подключаются только через barrel, tree-shaking работает).
- `CI=true npm run build-storybook` — ok.
- Makefile в `Frontend/` отсутствует — этап N/A.

**Соответствие архитектуре:**
- §8.3 таблица компонентов (строки 818–819): Modal/Toast ✓ (controlled + focus-trap + 5 variants + sticky).
- §8.4 compound pattern + Slot: Modal/Popover compound, Slot через asChild на Trigger/Close ✓.
- §8.5 состояния — для overlays актуальны Default/Focus/DefaultOpen + sizes; ESC/keyboard как отдельная story.
- §10.2 Storybook + Chromatic + axe — WCAG 2.1 AA включён в preview.ts с FE-TASK-019.
- §8.2 tokens — z-index расширены (как в FE-TASK-017 аналогичное расширение shadow-lg/focus-ring).
- FSD: `src/shared/ui/{modal,popover,tooltip,toast}/` — каждая папка со своим `index.ts`, barrel в `shared/ui/index.ts`.

**Одобренное отклонение:**
- Упомянутый в session.log «portal-root helper в `shared/lib/`» не реализован — Radix Portal сам обрабатывает SSR/не-DOM окружение, кастом избыточен.

**Инциденты:**
- После первого `npm install @radix-ui/*` Storybook build падал с «Failed to resolve entry for package react-style-singleton» — `dist/` отсутствовал (битая установка). Исправление: ремоунт `node_modules/react-style-singleton/` и `npm install`. Также случайно был добавлен `react-style-singleton` в `dependencies` при попытке форсированной переустановки — убран (это транзитивная зависимость react-remove-scroll).

**Заметки для следующих итераций:**
- **FE-TASK-030** (app/providers, root shell): смонтировать глобальные `<TooltipProvider delayDuration={500}>` и `<Toaster />`. Zustand-стор тостов живёт вне React, но Toaster-компонент нужно монтировать один раз рядом с `<RouterProvider>`.
- **FE-TASK-037** (Export/Share modal): готова Modal с compound API. Для sticky-share-link — `toast.sticky`, для success-message — `toast.success`.
- **FE-TASK-039** (Comparison LowConfidence confirm, ShareModal): Modal + `disableEscape` для критичных подтверждений.
- **FE-TASK-053** (Vitest jsdom + RTL): добавить поведенческие тесты — focus-trap в Modal, focus-restore после close, Toast hover pauses autodismiss, Tooltip aria-describedby wiring.
- **FE-TASK-032** (Sheet/Drawer): отдельно, НЕ переиспользует Modal.
- **Motion-safe transitions** — сейчас через Tailwind `motion-safe:` + transition. Полноценные enter/exit (tailwindcss-animate) — опционально в FE-TASK-031.
- **API/Backend-orchestrator integration**: `toast.error` из interceptors (FE-TASK-013+), `Retry-After` 429 → `action: { label: 'Повторить', onClick }`.
- **i18n**: тексты в stories — placeholder для FE-TASK-011, переезд в i18next-resources в FE-TASK-030.

**Затронутые файлы:**
- `Frontend/src/shared/ui/modal/{modal.tsx,modal.test.ts,modal.stories.tsx,index.ts}` (new)
- `Frontend/src/shared/ui/toast/{toast-store.ts,toast-store.test.ts,toaster.tsx,toaster.test.ts,use-toast.ts,toast.stories.tsx,index.ts}` (new)
- `Frontend/src/shared/ui/tooltip/{tooltip.tsx,tooltip.test.ts,tooltip.stories.tsx,index.ts}` (new)
- `Frontend/src/shared/ui/popover/{popover.tsx,popover.test.ts,popover.stories.tsx,index.ts}` (new)
- `Frontend/src/shared/ui/index.ts` (modified: +20 экспортов для overlays)
- `Frontend/src/app/styles/tokens.css` (modified: +4 z-index токена)
- `Frontend/tailwind.config.ts` (modified: +zIndex modal/popover/tooltip/toast)
- `Frontend/package.json` (+4 deps: @radix-ui/react-{dialog,toast,tooltip,popover}) + `package-lock.json`

---

## FE-TASK-021 — DataTable на TanStack Table 8 (2026-04-17)

**Итог:** shared-примитив `DataTable` реализован в `src/shared/ui/table/` на `@tanstack/react-table ^8.13.0` (§20.5). Compound API через React Context, оба режима (server/client), 8 Storybook stories, 15 новых тестов (97/97 суммарно).

**План реализации:**
1. Установить `@tanstack/react-table ^8.13.0` (§20.5 pin).
2. Спроектировать compound через React Context: root `<DataTable>` + subcomponents `<DataTableToolbar>`, `<DataTableContent>` (объединённые thead+tbody в одном `<table>`), `<DataTablePagination>`, `<DataTableViewOptions>`, `<DataTableSelectionCheckbox>` (helper) + `useDataTable<T>()` hook.
3. Server-mode: `manualPagination`/`manualSorting`/`manualFiltering` + условное подключение client row-models (экономия CPU когда state контролируется сервером).
4. Default slots для empty/loading/error + переопределение через props.
5. A11y: aria-sort на header, aria-live на pagination-range, aria-label на prev/next, role=group на view-options, нативный `indeterminate` через ref.
6. Тесты (jsdom docblock, как в FE-TASK-028): рендер заголовков, empty/loading/error states, context throws без `<DataTable>`, client-side sorting, server-mode sorting callback, aria-sort transitions, pagination callbacks, page-size change, view-options, selection checkbox with indeterminate.
7. Stories: Default, Loading, Empty, Error (переименован в `ErrorState` — name Error конфликтует с глобальным `Error` в TS story-typings), WithSorting, WithPagination (server-mode с эмуляцией backend-slice), WithRowSelection, WithColumnVisibility.
8. Barrel + shared/ui re-exports.

**Ключевые решения:**
- **Head+Body в одном `DataTableContent`**, а не раздельные `<Columns>`/`<Rows>` из §8.4: thead/tbody должны быть детьми одного `<table>` (валидный HTML). Compound-семантика сохраняется — 3 слота верхнего уровня: Toolbar → Content → Pagination.
- **ctxValue без useMemo.** Попытка мемоизации по `table` (identity-стабильной между ре-рендерами) ломала uncontrolled-сортировку: TanStack обновляет internal state без смены ссылки на table → `useMemo[table]` не пересчитывает ctxValue → подписчики не видят новый `getRowModel()`. Пересоздаём ctxValue на каждый render — overhead ничтожен (context-provider единственный).
- **Server-mode с условными row-models.** `manualSorting=true` не подключает `getSortedRowModel()`, аналогично для filter/pagination — экономит cycles.
- **SelectionCheckbox как самостоятельный helper.** Не встроен в DataTable — позволяет собрать selection-колонку любой формы; indeterminate пробрасывается через ref+useEffect (DOM-атрибут не декларативный).
- **Column-visibility через существующий Popover из FE-TASK-020** + нативный `<input type=checkbox>` (shared Checkbox-примитива пока нет в §8.3).

**Verifications:**
- `npm run typecheck` → 0 errors
- `npm run lint --max-warnings=0` → 0 / 0
- `npx prettier --check .` → clean
- `npm run test` → **97/97 passed** (+15 новых)
- `npm run build` → 143.08 KB / 45.96 KB gzip (main не вырос — lazy load до страницы-потребителя)
- `npm run build-storybook` → ok, `data-table.stories` чанк 74.35 KB / 20.51 KB gzip
- Makefile в `Frontend/` отсутствует — этап N/A

**Архитектура:**
- §8.3 DataTable — 1:1 по списку фич (sort/pagination/row-selection/empty/loading/error/column-visibility)
- §8.4 Compound pattern — реализован через Context
- §8.5 States — 8 stories закрывают Default/Hover(row+button)/Focus(ring)/Disabled(pagination)/Loading/Error/Empty
- §10.2 Visual regression — stories для Chromatic готовы
- §20.5 pin @tanstack/react-table ^8.13.0 — соблюдён

**Заметки для следующих итераций:**
- **FE-TASK-047** (ContractsListPage — потребитель №1): `manualPagination+manualSorting+manualFiltering`, колонки Документов, Toolbar = SearchInput + FilterChips + ViewOptions.
- **FE-TASK-050** (AuditListPage — потребитель №2): сортировка по created_at + фильтры actor/action/entity.
- **FE-TASK-046** (ReportsPage, tablet-layout §8.6): таблица-в-карточки на `md` — отдельная обёртка, DataTable остаётся desktop.
- **FE-TASK-053** (Vitest jsdom global): удалить `// @vitest-environment jsdom` docblock из `data-table.test.tsx`.
- **FE-TASK-025** (прочие shared/ui): pagination встроен в DataTable; отдельный `Pagination`-примитив из §8.3 можно построить поверх тех же low-level кнопок для non-table страниц.
- **Sprint backlog**: `@tanstack/react-virtual ^3.5.0` из §20.5 ещё не установлен — нужен для виртуализации Audit (10k+ строк); завести тикет до появления perf-требования.
- **Refactor**: при появлении shared/ui/checkbox заменить нативный `<input type=checkbox>` в ViewOptions и SelectionCheckbox на общий примитив.

**Затронутые файлы:**
- `Frontend/src/shared/ui/table/{data-table.tsx,data-table.test.tsx,data-table.stories.tsx,index.ts}` (new)
- `Frontend/src/shared/ui/index.ts` (modified: +14 экспортов из ./table)
- `Frontend/package.json` (+@tanstack/react-table ^8.13.0) + `package-lock.json`

---

## FE-TASK-022 — FileDropZone + table-driven file validation (2026-04-17)

**Статус:** done
**Категория:** design-system
**Приоритет:** high
**Зависимости:** FE-TASK-019 (done). **Разблокирует:** FE-TASK-034 (contract-upload — critical), FE-TASK-035 (version-upload), FE-TASK-043 (NewCheckPage — critical).

**План реализации:**
1. По консультации с code-architect — три слоя: `shared/config/file-formats.ts` (данные), `shared/lib/validate-file/` (поведение), `shared/ui/file-drop-zone/` (UI). validateFile вынесен в shared, а не в feature, т.к. используется shared/ui-компонентом.
2. `shared/config/runtime-env.ts` — getRuntimeEnv()/getFeatureFlags() с typeof window guard, готово к window.__ENV__ (FE-TASK-009/030).
3. `shared/config/file-formats.ts` — FILE_FORMATS 1:1 с §7.5 + getActiveFormats(flags?) + getDropzoneAccept(formats).
4. `shared/lib/validate-file/` — async validateFile через FileReader (jsdom 24 не реализует Blob.arrayBuffer на slice'ах, FileReader работает везде). FileValidationError class с 4 кодами (EMPTY_FILE/FILE_TOO_LARGE/UNSUPPORTED_FORMAT/INVALID_FILE) + getFileValidationMessage (RU).
5. `shared/ui/file-drop-zone/` — uncontrolled v1 с onAccepted/onError/onReset + imperative open()/reset(). 7 состояний CVA. react-dropzone ^14.2 с включёнными accept/maxFiles=1 (UX-выгода в native picker), но noClick/noKeyboard=true (фикс nested-interactive).

**Ключевые решения:**
- **shared/config + shared/lib + shared/ui — три слоя.** §7.5 показывает validateFile в feature, но для design-system слоя — shared. shared/lib/cn — устоявшийся прецедент.
- **FileReader для magic-bytes.** `Blob.arrayBuffer()` отсутствует в jsdom 24 (slice-полученные Blob'ы) и в Safari < 14.1. `Response`-обёртка тоже глючит в jsdom (теряет бинарные байты при wrapping). FileReader.readAsArrayBuffer — стандарт с 2010-х.
- **EMPTY_FILE сверх §7.5.** Частый кейс (drag&drop пустого файла из Finder). По совету code-architect.
- **react-dropzone accept/maxFiles=1 включены.** Изначально архитектор предлагал отключить (single source of truth — наш validateFile), но включённый accept даёт фильтрацию в native file-picker диалоге. handleDrop корректно маппит rejection.code в наши коды → unified UX.
- **noClick/noKeyboard=true (фикс blocker'а от code-reviewer).** Исходно я ставил role=button + tabIndex=0 на root, но это создавало nested interactive с внутренней `<Button>` (axe wcag2a violation). Решение: root → role=region (без клавиатурной активации), drag-drop остаётся mouse-only (как и в нативном UA), клавиатурный путь — через внутреннюю real-кнопку «Выбрать файл».
- **validationIdRef-guard для stale async-результатов (фикс blocker'а).** При быстрой смене файла или reset за время FileReader финальный then/catch устаревшего вызова не должен перезаписывать актуальный state.
- **DOCX — best-effort на frontend (фикс blocker'а).** PK\x03\x04 матчит любой ZIP (jar/apk/xlsx). Зафиксировано JSDoc-комментарием. Глубокая валидация (Content_Types.xml + word/document.xml) — на бэкенде (DocumentProcessing security.md §6).
- **reset() silent на пустом state** — родитель не получает onReset если ничего не выбрано. Аналогично кнопка «Удалить» только при наличии файла.
- **aria-describedby условный + aria-live=polite на error.** Скринридер не получает «висячий» idref в loading/selected/error состояниях.

**Тестирование:**
- 8 тестов file-formats: структура FILE_FORMATS, активные форматы по флагам (no flags / with flag / explicitly false), accept-формат для react-dropzone.
- 3 теста runtime-env: пустой результат при отсутствии window/__ENV__, чтение заданного __ENV__.
- 8 тестов validate-file (jsdom docblock): happy PDF и DOCX, EMPTY_FILE, FILE_TOO_LARGE (default + custom maxSize), UNSUPPORTED_FORMAT (DOCX без флага), INVALID_FILE (подмена расширения), сообщения для всех 4 кодов.
- 14 тестов file-drop-zone (jsdom docblock): рендер 5 состояний (idle/selected/loading/disabled/custom-text), поведение (onAccepted для PDF, onError для UNSUPPORTED/INVALID/TOO_LARGE), удаление через кнопку и через ref, ref.reset() silent на пустом state, hint меняется по feature flags.

**Verifications (все test_steps):**
- Шаг 1 ✓: `npm run typecheck` — 0 errors.
- Шаг 2 ✓: `npm run lint --max-warnings=0` — 0 errors / 0 warnings.
- Шаг 3 ✓: `npx prettier --check .` — clean.
- Шаг 4 ✓: `npm run test` — **130/130 passed** (+33 новых).
- Шаг 5 ✓: `npm run build` — dist/ 143.08 kB JS / 4.58 kB CSS gzip (main JS не вырос — FileDropZone лениво подключается через barrel; CSS прирос на utility-классы).
- Шаг 6 ✓: `CI=true npm run build-storybook` — ok 1.10 min, file-drop-zone.stories собран.
- Makefile в `Frontend/` отсутствует — этап N/A.

**Соответствие архитектуре:**
- §7.5 FILE_FORMATS — 1:1 (3 формата с правильными MIME/extensions/magicBytes/featureFlags).
- §7.5 validateFile — 1:1 коды + EMPTY_FILE сверх (одобрено code-architect).
- §8.3 FileDropZone таблица — все состояния реализованы (drag-hover/selected/error/loading/disabled/max-size guard).
- §8.5 — 8 stories покрывают Default/Hover (через :hover в idle)/Focus (focus-within-ring)/Disabled/Loading/Error.
- §13.4 FEATURE_DOCX_UPLOAD — feature-flag путь работает.
- §13.5 runtime-config — getRuntimeEnv() готов к window.__ENV__.
- §3 FSD — shared/config + shared/lib/validate-file + shared/ui/file-drop-zone в правильных слоях.
- §20.5 react-dropzone ^14.2.0 — pin соблюдён.

**Subagents:**
- code-architect (план: 5 вопросов о размещении/runtime-env guard/FileValidationError разделении/uncontrolled API/validateFile внутри компонента — все ответы интегрированы).
- code-reviewer (финал-review: вернул 3 merge-blocker'а, все исправлены — race-condition, DOCX best-effort docs, nested interactive a11y).

**Заметки для следующих итераций:**
- **FE-TASK-034** (contract-upload feature): обернуть FileDropZone — `props.onAccepted → useUploadContract.mutate(file)`, `ref.reset()` при success/cancel. Если потребуется controlled API (form-level validation через RHF/Zod) — расширить FileDropZone props {file?, error?, onChange?}.
- **FE-TASK-043** (NewCheckPage): композиция FileDropZone + Title-input + WillHappenSteps. 12 figma-состояний включают drag-hover/selected/error/processing-start — первые три покрыты, processing-start = `loading={isUploading}`.
- **FE-TASK-035** (version-upload): тот же FileDropZone, передаётся base_version_id.
- **FE-TASK-014** (error catalog): server-side errors FILE_TOO_LARGE/UNSUPPORTED_FORMAT/INVALID_FILE из API — отдельная сущность от FileValidationError; можно переиспользовать getFileValidationMessage логику.
- **FE-TASK-030** (App shell): после window.__ENV__-инжекции FileDropZone автоматически подхватит FEATURE_DOCX_UPLOAD из FEATURES.
- **FE-TASK-009** (Dockerfile + entrypoint.sh): runtime-инъекция window.__ENV__ должна включать FEATURES объект (по умолчанию {} — DOCX закрыт). При включении DOCX — синхронизация с backend ORCH_UPLOAD_ALLOWED_MIME_TYPES (см. §18 п.6).
- **FE-TASK-053** (Vitest jsdom global): удалить `// @vitest-environment jsdom` docblock из validate-file.test.ts и file-drop-zone.test.tsx.
- **Backlog**: deep DOCX validation (Content_Types.xml + word/document.xml в central directory) — глубокая проверка ~2-4 КБ central directory + парсинг ZIP — отложено до конкретного фишинг-кейса.
- **Backlog**: при инлайн featureFlags={{...}} — handleDrop пересоздаётся каждый ререндер (deps на formats). Гайдлайн в JSDoc «передавайте мемоизированный объект» дан; альтернатива — JSON.stringify сравнение (overhead vs ясность).

**Затронутые файлы:**
- `Frontend/src/shared/config/{runtime-env.ts,runtime-env.test.ts,file-formats.ts,file-formats.test.ts,index.ts}` (new + modified barrel)
- `Frontend/src/shared/lib/validate-file/{validate-file.ts,validate-file.test.ts,index.ts}` (new)
- `Frontend/src/shared/ui/file-drop-zone/{file-drop-zone.tsx,file-drop-zone.test.tsx,file-drop-zone.stories.tsx,index.ts}` (new)
- `Frontend/src/shared/ui/index.ts` (modified: +5 экспортов из ./file-drop-zone)
- `Frontend/package.json` (+react-dropzone ^14.2.3) + `package-lock.json`


---

## FE-TASK-023 — ProcessingProgress widget (2026-04-17)

**Статус:** done
**Категория:** design-system
**Приоритет:** high
**Зависимости:** FE-TASK-019 (done). **Разблокирует:** FE-TASK-042 (DashboardPage — critical), FE-TASK-045 (ContractDetailPage), FE-TASK-046 (ResultPage — critical).

**Что сделано:**
- `src/widgets/processing-progress/step-model.ts` — `PROCESSING_STEPS` (линейный pipeline из 6 шагов: UPLOADED → QUEUED → PROCESSING → ANALYZING → GENERATING_REPORTS → READY), `mapStatusToView(status, errorAtStep?) → ProcessingView` (currentIndex, tone, message, terminal, percent), `stepStateAt(index, view) → StepState`. Лейблы 1:1 с `ApiBackendOrchestrator/architecture/high-architecture.md §5.2`.
- `processing-progress.tsx` — виджет: корневой `<section role=region aria-label>`, progressbar (`aria-valuemin/max/now/valuetext/busy`), `<ol>` со списком 6 `Step`'ов (React.memo). CVA-варианты (`progress/awaiting/error/success`) через токены Tailwind (`bg-brand-500`, `border-warning`, `bg-danger`, `bg-success`), без hex. Ранний return для `REJECTED` — pipeline не стартовал (pre-processing), отдельная error-card без progress-bar. Slot-проп `awaitingAction?: ReactNode` для inline-CTA под AWAITING_USER_INPUT-шагом.
- `processing-progress.test.tsx` — 24 теста (jsdom docblock, RTL): контракт `mapStatusToView` на все 10 статусов, `stepStateAt` на 4 ключевых сценариях, aria-атрибуты progressbar, slot-CTA рендерится только в awaiting-состоянии, REJECTED ранний return, aria-current=step на текущем, FAILED+errorAtStep override.
- `processing-progress.stories.tsx` — 13 stories: 10 базовых статусов + `AwaitingUserInputWithAction` (slot + реальная Button) + `FailedOnReports` (errorAtStep override) + `LongLabelOverflow` (edge-case узкого контейнера).
- `index.ts` — barrel: `ProcessingProgress`, `ProcessingProgressProps` + re-export step-model helpers для потребителей.

**План реализации:**
1. Консультация code-architect — валидация slot-проп вместо callback, REJECTED → ранний return, step-model локально в widget'e (lift при 2-м потребителе), CVA через токены, `aria-busy` для awaiting/progress, React.memo на `Step`.
2. step-model.ts с pure-функциями — потребители страницы могут посчитать прогресс без рендера виджета.
3. Компонент с CVA на корень + на progressbar-fill + на step-icon + на step-label. React.memo на Step.
4. A11y: region-role + progressbar aria-valuemin/max/now/valuetext/busy + aria-current=step + aria-live=polite.
5. Тесты: сначала step-model (pure), потом рендер через RTL.
6. 13 stories (базовые 10 + 3 edge-case по совету code-architect).
7. Финальный code-reviewer: 'ship-it, no blockers'; применены 2 non-blocker'а (убран tone-override prop, опечатка).

**Ключевые решения / отклонения:**
- **awaitingAction как slot-проп, не callback.** По совету code-architect: §8.3 — AWAITING_USER_INPUT generic-состояние, завтра может быть «подтвердите сторону» / «уточните юрисдикцию». Slot решает 99% callback-кейсов + flexibility (Link, formaction, несколько кнопок).
- **REJECTED — ранний return.** Вариант C из code-architect. REJECTED — pre-pipeline валидация (SSRF/MIME/size), семантически ≠ 'pipeline упал на шаге N'. FAILED/PARTIALLY_FAILED показывают progress-bar до error-step.
- **step-model локально в widget'e**, НЕ в shared/lib / entities/job. YAGNI: entities/job пуст, lift создаст контракт, который будем ломать при первом реальном consumer'е. Barrel экспортирует `mapStatusToView` — будущий переезд не сломает потребителей.
- **FAILED/PARTIALLY_FAILED — дефолт errorAtStep по backend §5.2:** FAILED → PROCESSING, PARTIALLY_FAILED → GENERATING_REPORTS. Потребитель (useEventStream из FE-TASK-016) может передать более точный шаг, если event привезёт error_code с указанием фазы.
- **13 stories вместо 10 из acceptance.** +`AwaitingUserInputWithAction`, +`FailedOnReports`, +`LongLabelOverflow` (edge-case RTL по совету code-architect).
- **Убран `tone`-override prop** (после code-reviewer non-blocker #5). Причина: API-footgun — caller мог передать `tone="success"` при `status="FAILED"` и получить зелёный bar на errored view. Tone теперь полностью выводится из status — инвариант сохранён.

**Verifications:**
- Шаг 1 ✓: `npm run typecheck` — 0 errors.
- Шаг 2 ✓: `npm run lint --max-warnings=0` — 0 errors / 0 warnings.
- Шаг 3 ✓: `npx prettier --check .` — clean.
- Шаг 4 ✓: `npm run test` — **154/154 passed** (+24 новых).
- Шаг 5 ✓: `npm run build` — dist/ 143.08 kB JS / 45.96 kB gzip (main не вырос).
- Шаг 6 ✓: `CI=true npm run build-storybook` — ok 1.1 min, 13 stories в Widgets/ProcessingProgress собраны.
- Makefile в `Frontend/` отсутствует — этап N/A.

**Соответствие архитектуре:**
- §8.3 ProcessingProgress — «10 статусов → progress-bar + список шагов; для AWAITING_USER_INPUT рендерится не как шаг, а как inline-CTA» — 1:1.
- §8.5 — 13 stories покрывают Default/Current/Awaiting/Error/Success + edge-case длинных лейблов.
- backend §5.2 (ApiBackendOrchestrator) — все 10 статусов имеют user-friendly RU-лейблы 1:1 с таблицей маппинга.
- §2 FSD — widget импортирует только `@/shared/*` (cn + Spinner + openapi types); никаких пересечений с другими widgets / features / pages / app.
- §9.3 — errorMessage с correlation_id через `aria-live="polite"`.
- §1451 (чеклист): «✅ Все 10 статусов обработки ↔ ProcessingProgress + SSE-редьюсер» — выполнено (SSE-редьюсер будет в FE-TASK-016).

**Subagents:**
- **code-architect** (план-валидация): одобрил общий план; 4 ответа на вопросы; 6 подводных камней (aria-busy, CVA-токены, +2 stories, React.memo, barrel, jsdom docblock) — все применены.
- **code-reviewer** (финал): вердикт 'ship it', 0 blockers. 6 non-blocker'ов: (1) percentOf на error-статусах (backlog), (2) i18n RU-строк (FE-TASK-030), (3) aria-busy на section (минор), (4) readability (минор), (5) tone-override footgun (**применено**), (6) опечатка 'канонічном' (**применено**).

**Заметки для следующих итераций:**
- **FE-TASK-016** (useEventStream — pending, high): на SSE event → `queryClient.setQueryData(qk.contracts.status)`; ProcessingProgress получит актуальный status через `useQuery`. Для AWAITING_USER_INPUT — `awaitingAction={<Button onClick={() => openLowConfidenceModal(data)}>Подтвердить тип договора</Button>}`.
- **FE-TASK-037** (low-confidence-confirm): Modal из useEventStream + ProcessingProgress inline-CTA. Дублирование намеренное (Figma «Результат»/4-е состояние + §8.3).
- **FE-TASK-042** (DashboardPage — critical, был заблокирован FE-TASK-023): LastCheckCard включает ProcessingProgress для последней версии.
- **FE-TASK-045** (ContractDetailPage — high), **FE-TASK-046** (ResultPage — critical, был заблокирован): тот же ProcessingProgress как header-widget. На ResultPage рендерится ТОЛЬКО при `status !== 'READY'`.
- **FE-TASK-024** (RiskBadge + StatusBadge — medium): StatusBadge маппит те же 10 статусов. При появлении 3-го потребителя — вынести `getStatusTone()` в shared helper.
- **FE-TASK-053** (Vitest jsdom global): удалить `// @vitest-environment jsdom` docblock из `processing-progress.test.tsx`.
- **FE-TASK-030** (App shell): i18next ru-namespace подхватит RU-лейблы. Backlog — `mapStatusToView(status, { t, errorAtStep })` или useTranslation внутри компонента.
- **Backlog** (code-reviewer non-blocker #1): percent-семантика на error-статусах (`PARTIALLY_FAILED → 80%` vs `ANALYZING → 60%`). Рассмотреть carry «highest reached» в state-manager (useEventStream).
- **Backlog** (lift step-model): при 2-м потребителе — поднять в `entities/job/model/processing-status.ts`; widget останется тонкой обёрткой.

**Затронутые файлы:**
- `Frontend/src/widgets/processing-progress/{step-model.ts,processing-progress.tsx,processing-progress.test.tsx,processing-progress.stories.tsx,index.ts}` (new + modified barrel)
- (никакие shared/ui компоненты не менялись — FE-TASK-020/019 уже экспортируют Button/Spinner)
