# ADR-FE-09: Token pipeline — синхронизация Figma ↔ `tokens.css`

| Поле | Значение |
|---|---|
| Статус | **Accepted** |
| Дата | 2026-05-27 |
| Автор | Frontend team (alignment Этап 2) |
| Зависит от | Figma file `Lxhk7jQyXL3iuoTpiOHxcb` (Pro tier, без Code Connect), Figma MCP плагин для извлечения через `get_design_context` |
| Связано с | §8.2 high-architecture.md (полная таблица токенов), [`../figma-mapping.md`](../figma-mapping.md), ADR-FE-04 (UI stack), ADR-FE-08 (Figma как обязательный артефакт UI-PR) |

## Контекст

ContractPro использует Figma как единый источник истины для дизайна (см. ADR-FE-08). При этом:

1. **Figma Variables в файле не настроены.** `get_variable_defs` на главных фреймах возвращает `{}`. Tokens сняты вручную при инициализации проекта (Этап 0 верстки), что привело к drift'у (`#666b78` vs реальный `#4d5261` для `fg-muted`, `#d9dee5` vs `#d9dbe0` для border, и др.).
2. **Code Connect недоступен** на Pro tier (требует Developer seat в Organization/Enterprise). Связь Figma-component ↔ React-component поддерживается через [`../figma-mapping.md`](../figma-mapping.md) вручную.
3. **Собственной design-system Component-библиотеки в Figma нет** — кнопки/инпуты разбросаны по экранам как auto-layout frames с именами вида `Button/Login`, `Card/Risk`. Mapping ведётся по соглашению об именах, не через component-instance matching.

Без формализованного пайплайна drift накапливается между релизами: вёрстка отстаёт от макетов, ревью теряет смысл, новые токены вводятся ad-hoc.

## Решение

Принимаем **полу-автоматический pipeline извлечения через MCP-tooling** при отсутствии Figma Variables:

1. **Single source files в коде:**
   - `src/app/styles/tokens.css` — определение CSS-переменных, единственный канонический список.
   - `tailwind.config.ts` — маппинг переменных в утилитарные классы Tailwind. Hex-значения **не дублируются** — только `var(--token-name)`.
   - §8.2 в `high-architecture.md` — человекочитаемая таблица токенов с назначением, не дублирует значения для истории (только текущее состояние).

2. **Процедура re-extraction** (при Figma-alignment или подозрении на drift):
   1. Через MCP `mcp__plugin_figma_figma__get_design_context` извлечь representative-фреймы: Button/Login, Button/PrimaryCTA, Badge, Headline, Risk Summary (`159:3`, `159:19`), Dashboard Active (`100:2`). Полный список фреймов — [`../figma-mapping.md`](../figma-mapping.md).
   2. Сделать diff с `tokens.css` — выписать каждое drift-значение и каждый новый токен.
   3. Обновить `tokens.css` (источник правды) и `tailwind.config.ts` (экспорт утилит) в одном коммите.
   4. Обновить §8.2 в `high-architecture.md` (таблица токенов).
   5. Прогнать `npm run typecheck && npm run lint && npm run test` — должны быть зелёные.
   6. Запустить `npm run chromatic` вручную; визуальные изменения принимает дизайнер/разработчик в web UI Chromatic. Это становится новым baseline.

3. **Naming convention токенов:**
   - Цвета: `--color-{role}` (`fg`, `bg`, `border`, `success`, `risk-high` и т.д.), производные тинты — `--color-{role}-bg`/`-bg-soft`.
   - Spacing: `--space-N` где `N = px / 4`; для значений не делящихся на 4 — `--space-N-M` (`1-5` = 6px, `2-5` = 10px, `3-5` = 14px).
   - Typography: `--text-{px}` (явный px в имени) до тех пор, пока не появится семантическая шкала.
   - Radii: семантические имена `sm/md/lg/xl/pill` без привязки к px.
   - Shadow: семантические `sm/md/lg/card`.

4. **Discipline-правила:**
   - В коде запрещены inline hex-значения, кроме случаев, когда значение явно one-off (например, `rounded-[8px]` на единственной кнопке «Скачать отчёт»). Для one-off — оставлять `// figma node-id` комментарий.
   - При добавлении нового токена — синхронно: `tokens.css` → `tailwind.config.ts` → §8.2 → коммит `style(fe):` или `feat(fe):`.
   - При drift-fix существующего токена — коммит `style(fe): align <token> with figma`.

5. **Будущее развитие:** при росте сложности (>3-х re-extraction'ов в квартал, либо переход на Enterprise tier Figma) — мигрировать на Figma Variables + автоматическую генерацию `tokens.css` через `figma-tokens-cli` или аналог. Pipeline в этом ADR пересматривается тогда же.

## Альтернативы

| Альтернатива | Плюсы | Минусы | Вердикт |
|---|---|---|---|
| **Status quo** (без формализации) | Минимум усилий | Drift растёт незаметно; вёрстка ↔ макеты расходятся; ревью теряет ценность | Rejected: уже привело к 5 drift'ам колоров и отсутствию typography scale |
| **Figma Variables → авто-генерация** | Полная автоматизация; типизация; нет ручного diff | Требует переделки Figma-файла под Variables; Code Connect ускорит, но недоступен на Pro tier | Accepted-later: вернуться при Enterprise tier либо при росте сложности |
| **Style Dictionary / Theo CLI** | Cross-platform экспорт (iOS/Android/Web) | Cross-platform не нужен — только Web; overhead настройки | Rejected: над-инжиниринг для нашего scope |
| **CSS-in-JS (Emotion/styled-components)** | Динамическая темизация | Runtime overhead; противоречит ADR-FE-04 (Tailwind) | Rejected by ADR-FE-04 |

## Trade-offs

| Аспект | Влияние |
|---|---|
| Скорость разработки | Замедление при добавлении токена (3 файла + §8.2), но drift не накапливается |
| UX-консистентность | Высокая — все компоненты читают через CSS-переменную, drift-fix распространяется автоматически |
| Сложность для нового разработчика | Минимальная — taxonomy и naming описаны в этом ADR + §8.2 |
| Backwards compatibility | При drift-fix существующего токена визуал меняется автоматически — обязательно Chromatic-acceptance |
| Производительность | Нулевое влияние — CSS-переменные нативны, без runtime cost |
| Тестирование | Snapshot-тесты ловят drift автоматически; Chromatic — визуальные регрессии |

## План перехода

Pipeline уже применён в коммите `8fa8adb style(fe): align design tokens with figma (этап 2)`:
- 5 drift-fixes (fg-muted, border, risk-high/medium/low)
- 9 новых colors (fg-subtle, fg-disabled, border-subtle, divider, processing, risk-{high,medium,low}-bg тинты)
- 2 radii (xl, pill)
- 4 spacing (1-5, 2-5, 3-5, 7)
- 8 typography sizes (11..60)
- 1 shadow (card)

Будущие re-extraction'ы — по той же процедуре. Промежуточные «правки одного hex'a» допустимы тем же `style(fe):` коммитом без полного re-extraction'а, если правка точечная.

## Метрики успеха

- **Drift-free через 1 квартал после re-extraction** — повторная выборка `get_design_context` не показывает расхождений по выбранным representative-фреймам.
- **Zero inline hex** в `src/` (кроме явно помеченных one-off) — проверяется grep'ом в CI или вручную при ревью UI-PR.
- **§8.2 + tokens.css не расходятся** — при ревью UI-PR обязан проверять reviewer.

## Связанные документы

- [`../high-architecture.md`](../high-architecture.md) §8.2 — текущая таблица токенов
- [`../figma-mapping.md`](../figma-mapping.md) — каталог Figma фреймов и mapping в код
- [`../../src/app/styles/tokens.css`](../../src/app/styles/tokens.css) — источник правды
- [`../../tailwind.config.ts`](../../tailwind.config.ts) — экспорт в утилиты
- ADR-FE-04 (UI stack) — обоснование Tailwind + CSS Variables
- ADR-FE-08 (Figma как обязательный артефакт UI-PR) — disciplinary supplement
