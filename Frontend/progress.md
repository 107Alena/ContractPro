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
