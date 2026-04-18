// A11y baseline e2e (FE-TASK-055 acceptance #3 test-step): axe-проверка
// на LoginPage не должна находить critical-нарушений (блокирующие, §10.4).
//
// Serious-нарушения (напр., color-contrast в brand-палитре `#F55E12` на
// PromoSidebar/CTA «Войти») НЕ проверяются здесь: это известный design-debt,
// отдельный a11y-ratchet-рефактор должен закрыть их в дизайн-системе.
// Фикстура `a11y` по умолчанию берёт `impacts: ['critical']` — см.
// tests/e2e/fixtures/a11y.ts (docstring). Когда палитра будет исправлена,
// ужесточим до `impacts: ['critical', 'serious']` в одной строке.
//
// Сценарии login happy / wrong-password / validation retry — отложены на
// deferred_follow_ups FE-TASK-029 (см. tasks.json), реализуются
// последующими итерациями поверх этой инфраструктуры.

import { expect, test } from './fixtures';

test.describe('Login page a11y', () => {
  test('/login проходит axe WCAG 2.1 AA без critical-нарушений', async ({ page, a11y }) => {
    await page.goto('/login');

    // Убеждаемся, что React смонтировал страницу — иначе axe проверит пустой root.
    await expect(page.getByTestId('page-login')).toBeVisible();
    await expect(page.getByRole('heading', { name: /Вход в/i })).toBeVisible();

    await a11y.check();
  });
});
