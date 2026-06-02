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
| 12 | — | `/settings` | `src/pages/settings/SettingsPage.tsx` | ⚙️ code orphan — tokens-only alignment |
| 13 | — | `/admin/policies` | `src/pages/admin-policies/AdminPoliciesPage.tsx` | ⚙️ code orphan — tokens-only alignment |
| 14 | — | `/admin/checklists` | `src/pages/admin-checklists/AdminChecklistsPage.tsx` | ⚙️ code orphan — tokens-only alignment |
| 15 | — | `/403` `/404` `/500` `/offline` | `src/pages/errors/*` | ⚙️ code orphan — tokens-only alignment |

**Сводка:** 9 страниц ↔ Figma матчатся, 1 — секция Landing, 1 Figma orphan (Audit, отложено), 4 code orphans (без макетов — обновляются только через design-system).

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

**Honesty-отклонения от макета:** (1) «до 50 МБ» → **«до 20 МБ»** (реальный лимит, openapi `макс. 20 МБ`); (2) подзаголовок без обещания paste-ввода + таб «Вставить текст» = placeholder (backend PDF-only); (3) **поля «Название договора» в Figma нет**, но `title` обязателен в `uploadContract` → авто-вывод из имени файла (scope-решение 4.6); (4) `PageTitle` 28px → `text-24` (нет токена 28).

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
