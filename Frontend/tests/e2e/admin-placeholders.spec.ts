// Admin placeholders e2e (FE-TASK-001):
//   (1) ORG_ADMIN на /admin/policies видит EmptyState;
//   (2) BUSINESS_USER на /admin/policies → редирект /403;
//   (3) пункт меню «Администрирование» виден только ORG_ADMIN.
//
// Роль выбирается MSW-хендлером GET /users/me через `window.__cpE2eRole__`:
// seedAuthenticatedSession(page, { role }) кладёт значение в addInitScript,
// main.tsx после worker.start() вызывает applyE2ERoleOverride().
// Cookie-подход не годится: MSW Service Worker не получает Cookie-заголовок
// в Request (ограничение Service Worker Fetch API).
//
// Важно: useSession — in-memory store, он сбрасывается при полной перезагрузке
// страницы (page.goto). Поэтому: логинимся через UI (наполняет store), затем
// навигируем SPA'шно через history.pushState + popstate — React Router
// перерисовывает маршрут без перезагрузки.

import type { Page } from '@playwright/test';

import { expect, seedAuthenticatedSession, test } from './fixtures';

async function loginAs(page: Page, role: 'ORG_ADMIN' | 'BUSINESS_USER' | 'LAWYER'): Promise<void> {
  await seedAuthenticatedSession(page, { role });
  await page.goto('/login');
  await page.getByLabel('Email').fill('user@contractpro.local');
  await page.getByLabel('Пароль').fill('correct-horse-battery-staple');
  await page.getByRole('button', { name: 'Войти' }).click();
  await page.waitForURL(/\/dashboard$/);
}

async function spaNavigate(page: Page, path: string): Promise<void> {
  await page.evaluate((to: string) => {
    window.history.pushState({}, '', to);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, path);
}

test.describe('Admin placeholders @smoke', () => {
  test('(1) ORG_ADMIN видит EmptyState на /admin/policies', async ({ page }) => {
    await loginAs(page, 'ORG_ADMIN');
    await spaNavigate(page, '/admin/policies');

    const pageRoot = page.getByTestId('page-admin-policies');
    await expect(pageRoot).toBeVisible();
    await expect(pageRoot.getByRole('heading', { name: 'Раздел в разработке' })).toBeVisible();
    await expect(
      pageRoot.getByText('Управление политиками организации появится в версии 1.0.1'),
    ).toBeVisible();
    expect(new URL(page.url()).pathname).toBe('/admin/policies');
  });

  test('(2) BUSINESS_USER на /admin/policies → редирект /403', async ({ page }) => {
    await loginAs(page, 'BUSINESS_USER');
    await spaNavigate(page, '/admin/policies');

    await page.waitForURL(/\/403(\?.*)?$/);
    expect(new URL(page.url()).pathname).toBe('/403');
    await expect(page.getByTestId('page-admin-policies')).toHaveCount(0);
  });

  test('(3a) ORG_ADMIN видит пункт «Администрирование» в sidebar', async ({ page }) => {
    await loginAs(page, 'ORG_ADMIN');
    const sidebar = page.getByTestId('sidebar-desktop');
    await expect(sidebar).toBeVisible();
    await expect(sidebar.getByRole('group', { name: 'Администрирование' })).toBeVisible();
    await expect(sidebar.getByTestId('nav-admin-policies')).toBeVisible();
    await expect(sidebar.getByTestId('nav-admin-checklists')).toBeVisible();
  });

  test('(3b) LAWYER и BUSINESS_USER не видят «Администрирование» в sidebar', async ({ page }) => {
    await loginAs(page, 'LAWYER');
    const lawyerSidebar = page.getByTestId('sidebar-desktop');
    await expect(lawyerSidebar).toBeVisible();
    await expect(lawyerSidebar.getByRole('group', { name: 'Администрирование' })).toHaveCount(0);
    await expect(lawyerSidebar.getByTestId('nav-admin-policies')).toHaveCount(0);
    await expect(lawyerSidebar.getByTestId('nav-admin-checklists')).toHaveCount(0);
  });
});
