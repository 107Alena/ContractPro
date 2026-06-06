# Figma ↔ Frontend mapping

**Source:** Figma file `Lxhk7jQyXL3iuoTpiOHxcb` (https://www.figma.com/design/Lxhk7jQyXL3iuoTpiOHxcb).
Все верхнеуровневые фреймы расположены на единственной странице `0:1 Page 1`.

Эта таблица — единый источник истины при сверке верстки с макетами. Обновляйте при изменении структуры Figma или появлении новых страниц.

---

## Page-level mapping

| # | Figma main frame | Route | Code page | Status |
|---|---|---|---|---|
| 1 | `4:2` Landing (1440×7410) + `443:2` Mobile Landing (390×8439) | `/` | `src/pages/landing/LandingPage.tsx` | ✅ matched |
| 2 | `49:2` Auth Desktop (1440×900) + `61:2` Auth Mobile (390×844) | `/login` | `src/pages/auth/LoginPage.tsx` | ✅ matched |
| 3 | `84:2` Dashboard (1440×2090) | `/dashboard` | `src/pages/dashboard/DashboardPage.tsx` | ✅ matched |
| 4 | `193:2` Документы и история (1440×1763) | `/contracts` | `src/pages/contracts-list/ContractsListPage.tsx` | ✅ matched |
| 5 | `112:2` Новая проверка (1440×2244) | `/contracts/new` | `src/pages/new-check/NewCheckPage.tsx` | ✅ matched |
| 6 | `306:2` Карточка документа (1440×3656) | `/contracts/:id` | `src/pages/contract-detail/ContractDetailPage.tsx` | ✅ matched |
| 7 | `150:2` Результат проверки (1440×1744) | `/contracts/:id/versions/:vid/result` | `src/pages/result/ResultPage.tsx` | ✅ matched |
| 8 | `169:2` Сравнение версий (1440×3906) | `/contracts/:id/compare` | `src/pages/comparison/ComparisonPage.tsx` | ✅ matched |
| 9 | `223:2` Отчеты (1440×2255) | `/reports` | `src/pages/reports/ReportsPage.tsx` | ✅ matched |
| 10 | `354:2` Pricing Page (1440×1080) | `/` (внутри Landing) | секция Landing → `widgets/pricing-section` | 🔗 mapped as section |
| 11 | `245:2` Журнал действий и аудит (1440×856) | — | — | ⚠️ Figma orphan — out of current scope |
| 12 | — | `/settings` | `src/pages/settings/SettingsPage.tsx` | ✅ code orphan — tokens aligned (этап 4.11) |
| 13 | — | `/admin/policies` | `src/pages/admin-policies/AdminPoliciesPage.tsx` | ✅ code orphan — tokens aligned (этап 4.11) |
| 14 | — | `/admin/checklists` | `src/pages/admin-checklists/AdminChecklistsPage.tsx` | ✅ code orphan — tokens aligned (этап 4.11) |
| 15 | — | `/403` `/404` `/500` `/offline` | `src/pages/errors/*` | ✅ code orphan — tokens aligned (этап 4.11) |

**Сводка:** 9 страниц ↔ Figma матчатся, 1 — секция Landing, 1 Figma orphan (Audit, отложено), 4 code orphans (без макетов — выровнены под design-system токены, этап 4.11).

> **Статус эпика:** alignment 4.1–5.x + Stage 6 (финальная верификация) завершены — см. [`high-architecture.md` §19.1](./high-architecture.md). Stage 6: typecheck/lint/unit(1367)/storybook зелёные, Playwright e2e 10/10, аудит honesty/навигации/docs чист. Chromatic full baseline — вручную. Опциональный follow-up: косметическая чистка recipe-drift на ранних экранах (§19.1).

**Этап 4.11 (token cascade, без figma):** orphan-страницы приведены к flat-card токен-рецепту — card-поверхности `border-border-subtle`+`shadow-none`+`rounded-lg`, лейблы без uppercase, заголовки на токен-шкалу (`text-24`/`text-16`). Структуру/вёрстку не меняли; Settings корень `<main>`→`<div>` (внутри AppLayout — устранено дублирование landmark). Сохранены валидные стандартные `text-sm`/`text-xs`; `shared/ui/empty-state` не трогали (shared-примитив).

### App Shell (глобальный хром)

Sidebar (`85:2`) и AppHeader (`86:3`) — дети фреймов страниц, общий хром всех
защищённых экранов (рендерится `AppLayout`, не входит в постраничные этапы 4.1–4.13).
Выравнивание — отдельный этап **4.4 App Shell**.

| Figma | Код | Статус |
|---|---|---|
| `85:2` Sidebar | `widgets/sidebar-navigation` | ✅ aligned (4.4.1): логотип-плашка, WorkspaceSwitcher (org из `useMe`), группы МЕНЮ/СИСТЕМА, active/inactive nav-стили, UserProfile + logout внизу (перенос из топбара). «Сравнение версий» опущено (нет standalone-роута); «Организация» реализована гранулярно как Политики/Чек-листы под RBAC. |
| `86:3` AppHeader | `widgets/topbar` | restyle (4.4.2): border-subtle, rounded-8 кнопки, h-60; user-menu убран (профиль в сайдбаре); глобальный поиск/help не добавлялись (нет бэкенда). |

---

## State frames per screen

Все state-фрагменты — отдельные 480×N (или 440×N) фреймы рядом с основным экраном на той же канвасе. При сверке используются как референс для empty/loading/error/role-restricted состояний соответствующих widget'ов.

### Landing — `4:2`
Без отдельных state-фреймов (одностраничный лендинг). Содержит секцию Pricing, эквивалентную фрейму `354:2`.

### Auth Desktop — `49:2`
| Figma | Name |
|---|---|
| `58:3` | State / Error (479×485) |
| `59:2` | State / Loading (440×321) |
| `59:15` | State / Success (440×303) |
| `59:24` | State / Default (440×321) |

### Auth Mobile — `61:2`
Без state-фреймов (mobile responsive того же flow).

### Dashboard — `84:2`
| Figma | Name |
|---|---|
| `97:2` | State / Empty (520×472) |
| `98:2` | State / Processing (520×412) |
| `99:2` | State / Low-Confidence (520×465) |
| `100:2` | State / Active (520×326) |

**Реализация (упрощение dashboard):** по продуктовому решению экран сокращён до WelcomeBlock (один CTA «Новая проверка договора», размер `lg`, без иконки-лупы) → двухколоночный layout (`Недавние проверки` слева | `Сводка` + `Организация` справа) → TrustFooter.

**Honesty-отклонения от макета 84:2:** относительно Figma убраны блоки **«Что важно сейчас»** (`CurrentActions`), **«Последняя проверка»** (`LastCheckCard`), **«Статус обработки»** (`ProcessingStatus`), **«Ключевые риски за последнее время»** (`KeyRisksCards`) и **«Быстрый старт»** (`QuickStart`), а также вторичные CTA WelcomeBlock «Загрузить договор» / «Вставить текст». `CurrentActions` остаётся в коде — переиспользуется на ContractsList; виджеты `dashboard-last-check`/`dashboard-processing-status`/`dashboard-key-risks`/`dashboard-quick-start` удалены (orphan).

**Карточка «Сводка» (`BusinessSummary`):** две метрики — `проверено` = `total` из `GET /contracts` (реальный all-time счётчик) и `в работе` = `inProgressCount` из `GET /contracts/stats` (`ContractStats.by_processing_status`: сумма pending+in_progress статусов; см. `entities/contract/use-contract-stats`). Эндпоинт `/contracts/stats` добавлен в OpenAPI Orchestrator (контракт), `openapi.d.ts` регенерирован. **Backend на этапе Путь C: замокан в MSW** (`tests/msw/fixtures/contracts.ts → contractStats`), реальная реализация — ORCH-TASK-057 + DM-TASK-059 (Путь A); при переключении на реальный backend Frontend менять не нужно. Если stats недоступны (загрузка/ошибка) → `в работе` = `—` (data-honesty, без выдуманных чисел).

### New Check — `112:2`
| Figma | Name |
|---|---|
| `134:3` | State / Default — Upload (480×349) |
| `134:12` | State / Drag-and-Drop Hover (480×222) |
| `135:3` | State / PDF Selected (480×392) |
| `135:22` | State / Paste Text (480×496) |
| `136:3` | State / Processing Start (480×350) |
| `136:34` | State / Low-Confidence Type (480×671) |
| `137:3` | State / Validation Error (480×492) |
| `137:26` | State / Unsupported Format (480×482) |

**Реализация (этап 4.6):** полная структура Figma — PageIntro + FormatHint → full-width WorkspaceCard (табы «Загрузить PDF / Вставить текст» + drop-зона / FileCard) → TwoColumnInfo (`WillHappenSteps` 117:3 + `WhatWeCheck` 117:35, 14 чипов) → `Tips` (118:2, виджет `new-check-tips`) → `TrustFooter` (переиспользован `widgets/dashboard-trust-footer`). Карточки — Card-примитив с flat-border treatment (`border-border-subtle` + `shadow-none`), радиусы `rounded-lg`(14)/`rounded-xl`(16). Хлебные крошки не в странице — глобальный `widgets/breadcrumbs` в AppLayout.

**Honesty-отклонения от макета:** (1) «до 50 МБ» → **«до 20 МБ»** (реальный лимит, openapi `макс. 20 МБ`); (2) подзаголовок без обещания paste-ввода + таб «Вставить текст» = placeholder (backend PDF-only); (3) **поля «Название договора» в Figma нет**, но `title` обязателен в `uploadContract` → авто-вывод из имени файла (scope-решение 4.6); (4) `PageTitle` 28px → `text-24` (нет токена 28); (5) `Tips` сокращён до **3 советов** (продуктовое решение): убран совет про приложения, из совета о PDF убрана фраза «и не защищён паролем»; сетка `lg:grid-cols-4` → `lg:grid-cols-3`.

### Result — `150:2`
| Figma | Name |
|---|---|
| `159:3` | State / High-Risk Summary (480×326) |
| `159:19` | State / Low-Risk Summary (480×440) |
| `159:37` | State / No Policy Template (480×244) |
| `159:44` | State / Export Share (480×417) |
| `160:3` | State / Feedback (480×407) |
| `160:18` | State / Warning Incomplete (480×394) |
| `160:36` | State / Version Comparison (480×381) |
| `160:57` | State / Default Completed (480×355) |

**Реализация (этап 4.8):** полная структура Figma — Page Intro (h1 «Результат проверки договора» + описание при READY) → тонкая Document Meta Card (153:20) → risk-overview панель 154:2 (RiskProfileCard | MandatoryConditions, gated) → Key Risks 156:2 (карточки с левым акцент-бордером по уровню + интегрированные рекомендации) → TwoColumnBottom 157:2 (Краткое резюме + key-параметры | Deviations + NextActions + Feedback) → LegalDisclaimer → TrustFooter (reuse). Все result-карточки — flat-border (`border-border-subtle` + `shadow-none`, rounded-xl), заголовки `text-16/18 semibold fg` (без uppercase). Данные реальные (`useResults` — risks/recommendations/summary/key_parameters/risk_profile).

**Структурные решения / отклонения:** (1) **рекомендации интегрированы в карточки риска** по `recommendation.risk_id` (Figma 150:2 не имеет отдельной секции «Рекомендации»; standalone `RecommendationsList` убран с ResultPage, но сохранён для ContractDetail). Несколько рекомендаций на один `risk_id` группируются и показываются все; orphan-рекомендации без совпадающего `risk_id` не отображаются. (2) Действие «Отметить как просмотренное» из Figma опущено — нет backend-персистентности. (3) summary-prose в Figma SummaryPanel (154:7) — risk-ориентированный текст, у нас `results.summary` нейтральный (бизнес-резюме) → размещён в нижней «Краткое резюме». (4) PageTitle 26px → `text-24`. (5) shared-виджеты restyle выровняли и ContractDetail (4.7). (6) Хлебные крошки глобальные (не в странице).

**Решения из adversarial-review (этап 4.8.5):** (a) **NextActions** гейтится под `canViewRisks` — вердикт строится из `risk_profile`/`risks`, которые backend стрипает для BUSINESS_USER, иначе показывался бы ложный «готов к подписанию» при скрытых high-рисках. (b) **MandatoryConditionsChecklist** на ResultPage не имеет источника данных (поля `mandatory_conditions` нет в `AnalysisResults`) → honest empty-copy «пока недоступна» (не «появятся после анализа», т.к. анализ на READY уже завершён). (c) «Подробнее» (drawer) рендерится только при `risk.id` (drawer открывается по id; без id был бы silent no-op). (d) risk-счётчики RiskProfileCard получили цветовой тинт по уровню (Figma 154:2). (e) a11y: «Подробнее» — постоянное подчёркивание + `text-brand-600`, `aria-controls`/`aria-expanded` на «Показать формулировку»; информативный текст `fg-subtle`→`fg-muted`. (f) **Отложено в a11y-бэклог:** контраст shared-`Badge` (semantic-tint, текст 12px на тинте 2.7–4.2:1 < AA 4.5:1) — затрагивает все экраны/бейджи и Chromatic-baseline, чинится отдельной token-задачей, не в рамках 4.8. (g) **Блоки убраны со страницы по продуктовому решению:** «Обязательные условия» (MandatoryConditions — нет API-источника) и «Отклонения от политики» (DeviationsFromPolicy). Компоненты сохранены в коде; risk-overview теперь = только RiskProfileCard.

### Comparison — `169:2`
| Figma | Name |
|---|---|
| `186:3` | State / Default Comparison (480×147) |
| `186:17` | State / Improved Risk Profile (480×194) |
| `186:29` | State / Worsened Risk Profile (480×194) |
| `187:3` | State / No Significant Changes (480×212) |
| `187:21` | State / Section-Focused Diff (480×268) |
| `187:33` | State / Share Export (480×292) |
| `188:3` | State / No Previous Version (640×271) |
| `188:16` | State / Partial Analysis Warning (480×228) |
| `188:26` | State / Filter By Risk (480×221) |

**Реализация (этап 4.9):** страница уже функционально полная (FE-TASK-047, 9 состояний). Этап = (1) **подключение реальной риск-дельты** и (2) **flat-card выравнивание**. Структура: PageHeader (заголовок «Сравнение версий договора» + описание) → VersionMetaHeader (base|target) → ComparisonVerdictCard → ChangeCounters (метрики) → RiskProfileDelta → KeyDiffsBySection → секция «Что изменилось» (TabsFilters-чипы + ChangesTable) → RisksGroups (resolved/introduced/unchanged) → секция «Сравнение текста» (lazy DiffViewer). Все карточки — flat-border (`border-border-subtle` + `shadow-none`, `rounded-xl`), заголовки `text-16/18 semibold` без uppercase. Корень — `<div>` (AppLayout даёт `<main>`).

**Риск-дельта (FE-TASK-048, подключена):** `useRisks(base)` + `useRisks(target)` → `risk-aggregation`: `risk_profile` → snapshot для verdict/дельты; матчинг рисков по `id` (fallback `clause_ref`/`description`) → resolved/introduced/unchanged. Gated на `risks.view` + готовый diff; нет артефакта (версия не READY / нет прав) → профиль undefined, группы пустые, виджеты показывают плейсхолдеры (без краша). MSW: delta v1/v2 имеют разные риски → наглядная дельта в dev:e2e (`/contracts/<delta>/compare?base=<deltaV1>&target=<deltaV2>`).

**Honesty-отклонения от макета 169:2:** (1) **«Рекомендации после сравнения»** (182:2) и **«Отклонения от политики / дельта политик»** (BottomRow) **опущены** — LIC отдаёт рекомендации/отклонения по версии, а не сравнительный артефакт (источника нет). (2) Метрики SummaryPanel вида **«3 из 6 условий улучшены»** опущены (нет дельты обязательных условий). (3) **VersionCards** показывают минимум (версия/автор/дата из `VersionMetadata`) — без filename/PDF-иконки/типа договора (нет в diff/version API). (4) Действия шапки Скачать/Поделиться/Повторная проверка → пока только функциональная «Пересчитать» (export/share на сравнении не подключены). (5) `<main>` дедуп.

**Stage 5 (functional alignment, выполнено):** все 4 compare-CTA карточки договора (comparison-entry / last-check / summary-card / quick-start) пресетят пару prev→current (`?base=<prev>&target=<current>`, две последние READY-версии по `version_number`, `buildComparePreset`) → `/compare` открывается populated, а не на «Версии не выбраны». ContractsList «Сравнить» — guard `current_version_number >= 2` (пресет невозможен: version-UUID нет в `ContractSummary`). ComparisonPage empty-states получили ссылку «← Вернуться на карточку договора». Reports detail-panel «Открыть результаты» → полная result-страница версии (versionId резолвится через `useContract`).

### Contracts List (Документы) — `193:2`
| Figma | Name |
|---|---|
| `205:2` | State / Default List (480×497) |
| `206:2` | State / Empty (480×512) |
| `207:2` | State / Processing Items (480×659) |
| `208:2` | State / Filtered by High Risk (480×519) |
| `209:2` | State / Filtered by Status (480×361) |
| `211:2` | State / Document Selected Expanded (480×565) |
| `212:2` | State / No Results (480×360) |
| `213:2` | State / Share Export Panel (480×1095) |
| `215:2` | State / Limited Access Role Restriction (480×1173) |

**Реализация (FE-TASK-058, ORCH-TASK-056):** колонки **«Тип»/«Риск»** в `DocumentsTable` и dashboard `RecentChecksTable` рендерят реальные `contract_type`/`risk_level` из `ContractSummary` (RU-лейблы типа — `entities/contract/model/contract-type.ts`, обратные к нормализации event-catalog §1.3; риск — `RiskBadge`). «—» остаётся ТОЛЬКО при `null` (договор не проанализирован). Stat **«высокий риск»** в `ContractsMetricsStrip` считается из `risk_level` текущей страницы (page-scoped, как «в обработке»/«требуют внимания»; добавлена видимая подпись охвата + sr-only). **Серверные фильтры** (`status`/`risk_level`/`contract_type[]`/группы статуса обработки/период `7д/30д/90д`) и **серверная сортировка** (toolbar `SortControl` → API `sort`/`order`; client header-сортировка в `DocumentsTable` убрана — per-page reorder противоречил бы глобальному порядку) уходят на бэкенд (раньше значения только писались в URL).

**Honesty-отклонения 193:2/200:2:** (1) чип «Требует внимания» из макета НЕ реализован как отдельная группа: его семантика — `AWAITING_USER_INPUT`, а бэкенд отклоняет этот статус в `processing_status` (orchestrator-managed → 400). Группы статуса: «Завершено»/«В обработке»/«С ошибкой» (последняя = PARTIALLY_FAILED/FAILED/REJECTED, зеркалит bucket-семантику status-view). (2) Контраст shared-`RiskBadge` (12px текст на тинте) — общий a11y-долг (см. §8.2 (f) high-architecture), не закрывается этой задачей; затрагивает новые поверхности (таблицы Документы/Дашборд). (3) период `7д/30д/90д` при мокнутом clock и апрельских фикстурах может давать пусто на коротких диапазонах (как в reports) — задокументированный pre-existing нюанс dev:e2e.

### Reports — `223:2`
| Figma | Name |
|---|---|
| `233:3` | State / Default Reports (480×515) |
| `233:31` | State / Report Detail Selected (480×530) |
| `233:50` | State / Export Modal (480×723) |
| `234:3` | State / Share Link Generated (480×395) |
| `234:20` | State / Share Settings (480×823) |
| `234:44` | State / Expired Link (480×399) |
| `235:3` | State / No Reports Yet (480×365) |
| `235:12` | State / Access Restricted (480×713) |
| `235:32` | State / Feedback Submitted (480×179) |
| `235:38` | State / Partial Analysis Warning (480×705) |
| `239:3` | Tablet Preview — Summary + Share Toolbar (768×534) |

**Реализация (этап 4.10, Опция B):** страница уже функционально полная (FE-TASK-048). Этап = (1) **flat-card выравнивание** существующих секций + (2) **новые честные секции**. Структура: PageHeader (заголовок «Отчёты» `text-24` + описание из figma) → ExpiredLinkBanner (при `?share=expired`) → ReportsMetrics (4 KPI) → секция поиск+фильтры → row [ReportsTable (+PaginationControls) | ReportDetailPanel с риск-профилем] → ShareableMaterials (4 static-карточки 231:2) → FeedbackBlock (reuse, по выбранному отчёту, 231:25) → TrustFooter (reuse, 232:2) → ExportShareModal. Карточки — flat-border (`border-border-subtle` + `shadow-none`, `rounded-xl`), заголовки `text-16/18 semibold` без uppercase, числа `text-24`. Корень — `<div>` (AppLayout даёт `<main>`).

**Резолв version-UUID + риск-профиль (FE-TASK-048):** реестр отдаёт `ContractSummary` без version-UUID (только `current_version_number`). При выборе строки page тянет `useContract(id)` → `current_version.version_id` (UUID); им питаются: риск-профиль detail-panel (`useRisks` gated на `risks.view` → `toReportRiskProfile`, паттерн 4.9), reuse `FeedbackBlock` и **починка латентного export-бага** (раньше в ExportShareModal уходил `String(номер версии)` вместо UUID). Уровень риска — только бэковый `overall_level`; из счётчиков НЕ синтезируем (иначе ложный вердикт; зеркалит RiskProfileCard). Нет артефакта/прав → скелет/честный плейсхолдер/секция скрыта (без краша).

**Honesty-отклонения от макета 223:2:** (1) **Summary-Strip 6 stats → 4 честных KPI** (ReportsMetrics): «доступно для шаринга / активных ссылок / истекают скоро» опущены — нет share-агрегатов в API. (2) **Activity-Log** (история действий, 231:26) опущен — нет audit API (v1.1). (3) **Shared-Link статус/срок/число открытий** (230:26) опущены — нет share-state API. (4) Колонки таблицы **«тип результата»/«риск»**: данные (`contract_type`/`risk_level`) появились в `ContractSummary` (ORCH-TASK-056/FE-TASK-058), но в таблицу **отчётов** не добавлены — scope FE-TASK-058 = список «Документы» + дашборд; для Reports вынесено в follow-up. (5) Detail-panel **Summary-Section** и **Materials-checklist** (230:23/230:31) опущены — вне scope этапа (B = только риск-профиль). (6) **Header Page-Intro actions** (Скачать/Поделиться/Новая проверка) опущены — пер-отчётные действия в detail-panel (прецедент минимальных хедеров). (7) **List-Header «Все отчёты»+count** опущен — диапазон показывает PaginationControls. (8) **Toolbar sort-dropdown / view-toggle / риск-чипы** опущены — нет данных/функционала. (9) Feedback гейтится выбором отчёта (макет-карточка generic; honesty требует конкретный contract+version). (10) date-period `7d/30d/90d` при мокнутом clock (июнь) и фикстурах (апрель) даёт пусто — задокументированный pre-existing follow-up (`tests/e2e/reports.spec.ts`).

### Contract Detail — `306:2`
| Figma | Name |
|---|---|
| `318:4` | State / Default Document (480×285) |
| `318:31` | State / High-Risk Document (480×334) |
| `318:57` | State / Low-Risk Document (480×334) |
| `320:3` | State / Processing New Version (480×327) |
| `320:21` | State / Partial Analysis Warning (480×345) |
| `321:3` | State / No Policy Template (480×305) |
| `321:12` | State / Share Export (480×553) |
| `321:40` | State / Comparison Available (480×256) |
| `322:3` | State / Role-Restricted (480×457) |
| `322:30` | State / No Previous Version (480×344) |
| `325:3` | Tablet Preview — Summary + Version History (768×601) |

**Реализация (этап 4.7):** полная структура Figma — Page Intro (заголовок + действия) → Document Meta Card (PDF-иконка, doc-info, status-badge, meta-grid 6 полей) → Main Content Row: левая колонка (Summary 311:4 + Latest Check 313:2 + KeyRisks + Recommendations + VersionsTimeline + ChecksHistory[#check-history] + ComparisonEntry 315:2 + ReportsShared + DeviationsChecklist + Документ-и-навигация) + правая колонка 320px (QuickStart «Быстрые действия» 312:3 + AccessNote 312:53) → TrustFooter (reuse). Page-local карточки — Card flat-border (`border-border-subtle` + `shadow-none`).

**Honesty-отклонения от макета:** (1) `ContractDetails` содержит только title/status/current_version/даты — **тип договора, политика, уровень риска, summary, риски, рекомендации, отклонения недоступны** (пер-версионные AnalysisResults, FE-TASK-046/048) → «—»/skeleton/«появится после анализа»; risk-бейдж/risk-чип/risk-счётчики-значения опущены или «—». (2) **Stats-card (12 проверок/8 рисков) и Activity-лента — бэкенда нет вообще** → не реализованы (не выдумываем). (3) Comparison-вердикт («12 различий», «стало лучше») требует /compare → ComparisonEntry = приглашение без фейка. (4) Sections-nav (10 разделов) — структура недоступна → опущена. (5) Подзаголовок со сторонами/типом не рендерится (нет в API). (6) PageTitle 28px → `text-24`. (7) shared-виджеты (RisksList/RecommendationsList/VersionsTimeline/ChecksHistory) сохраняют прежний стиль — выровняются на 4.8.

### Pricing — `354:2` (секция Landing)
| Figma | Name |
|---|---|
| `358:3` | State / DEFAULT STATE (440×576) |
| `358:34` | State / HOVER — LIGHT CARD (440×576) |
| `358:65` | State / HOVER — PRO CARD (440×596) |
| `358:101` | State / SELECTED / ACTIVE CTA (440×416) |

### Mobile Landing — `443:2`
| Figma | Name |
|---|---|
| `458:2` | State / Mobile Menu — Opened (390×844) |
| `459:2` | State / Sticky CTA (390×844) |
| `460:2` | State / FAQ — Expanded (390×716) |
| `461:2` | State / Features — Scroll Focus (390×705) |
| `462:2` | State / Trust & Security (390×523) |
| `462:36` | State / Bottom CTA — Spotlight (390×449) |
| `462:53` | State / Pricing — Pro Spotlight (390×763) |
| `462:110` | State / Footer (390×473) |

### Audit Log — `245:2` (вне scope текущего alignment)
| Figma | Name |
|---|---|
| `255:2` | State / Default Audit (480×372) |
| `255:53` | State / Filtered by User (480×350) |
| `255:96` | State / Filtered by Action (480×381) |
| `255:142` | State / Selected Event Detail (480×386) |
| `256:2` | State / Suspicious Activity (480×476) |
| `256:31` | State / No Results (480×340) |
| `256:43` | State / Limited Access (480×411) |
| `258:2` | State / Export Audit (480×512) |
| `258:37` | State / Retention Archived (480×394) |
| `258:55` | State / Warning Incomplete Data (480×455) |
| `259:2` | Tablet Preview — Summary + Filters (768×370) |

---

## Constraints discovered (важно для дальнейших этапов)

1. **Figma Variables не используются** в файле. `get_variable_defs` на главных фреймах возвращает `{}`. Tokens (`src/app/styles/tokens.css`) снимаются вручную через `get_design_context` по representative-фреймам. Полная процедура re-extraction'а описана в [`adr/009-token-pipeline.md`](./adr/009-token-pipeline.md); текущая таблица токенов — §8.2 в [`high-architecture.md`](./high-architecture.md).

2. **Собственной design-system библиотеки ContractPro в Figma нет.** Подключены только community-киты (Material 3, Apple iOS/macOS/watchOS/visionOS, Simple Design System). Кнопки/инпуты в файле — обычные auto-layout frames с именами `Button/Login`, `Card/Risk` и т.д. Mapping `Figma frame → код` ведётся по соглашению об именах.

3. **Code Connect недоступен** (требует Developer seat в Organization/Enterprise; у нас Pro tier). Связь Figma component ↔ code component поддерживается вручную через эту таблицу.

---

## Workflow

- 1 коммит = 1 этап работ. Прямые коммиты в `master`.
- Chromatic запускаем вручную после каждого этапа, accept/reject в web UI.
- `npm run typecheck && npm run lint && npm run test` должны быть зелёными перед коммитом.
