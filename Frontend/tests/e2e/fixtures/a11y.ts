// Accessibility-фикстура (§10.4 «a11y: axe-playwright прогоняет каждый e2e-сценарий;
// блокирующие нарушения — fail CI»; FE-TASK-055 acceptance #4).
//
// Расширяем стандартный `test` от @playwright/test фикстурой `a11y`, которая
// инжектит axe-core и предоставляет единый helper `check()` — не забываем
// прогнать его в каждом сценарии.
//
// Импакт-политика: `includedImpacts: ['critical']` по умолчанию.
// Это соответствует трактовке «блокирующие» в §10.4 = только critical;
// serious-нарушения (например, color-contrast в корпоративной палитре
// `#F55E12`) трекаются отдельно и не должны валить CI от сборки к сборке
// до явного a11y-ratchet-refactor. Сценарии могут ужесточить политику —
// `a11y.check({ impacts: ['critical', 'serious'] })` — когда страница
// прошла design-review на AA.

import { type Page, test as base } from '@playwright/test';
import type { RunOptions } from 'axe-core';
import { checkA11y, injectAxe } from 'axe-playwright';

type AxeImpact = 'minor' | 'moderate' | 'serious' | 'critical';

interface AxeCheckOptions {
  /** Селектор для скоупа axe (по умолчанию — весь body). */
  selector?: string;
  /** Список tag'ов axe — по умолчанию WCAG 2.1 A+AA (§10.4). */
  tags?: string[];
  /** Какие impact-уровни считаем блокирующими — по умолчанию ['critical']. */
  impacts?: AxeImpact[];
  /** Отключение конкретных правил — нужно только при обсуждённом дефекте. */
  disabledRules?: string[];
}

export interface A11yCheck {
  check: (options?: AxeCheckOptions) => Promise<void>;
  page: Page;
}

// WCAG 2.1 A+AA (§10.4). 'best-practice' — off: дизайн-система уже закрывает
// часть «советов», правила дают ложноположительные срабатывания.
const DEFAULT_WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];
const DEFAULT_IMPACTS: AxeImpact[] = ['critical'];

export const test = base.extend<{ a11y: A11yCheck }>({
  a11y: async ({ page }, use) => {
    await use({
      page,
      check: async ({ selector, tags, impacts, disabledRules } = {}) => {
        await injectAxe(page);
        const axeOptions: RunOptions = {
          runOnly: { type: 'tag', values: tags ?? DEFAULT_WCAG_TAGS },
          ...(disabledRules && disabledRules.length > 0
            ? {
                rules: Object.fromEntries(disabledRules.map((id) => [id, { enabled: false }])),
              }
            : {}),
        };
        await checkA11y(page, selector, {
          axeOptions,
          includedImpacts: impacts ?? DEFAULT_IMPACTS,
          detailedReport: true,
          detailedReportOptions: { html: true },
        });
      },
    });
  },
});

export { expect } from '@playwright/test';
