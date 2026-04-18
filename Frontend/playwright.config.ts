// Playwright config (§10.1-§10.4, §13.3 CI e2e job, FE-TASK-055).
//
// Стратегия: e2e гоняем SPA (Vite dev-сервер на 5173) с MSW-worker'ом
// в браузере — единый набор mocks, что и для Storybook/Vitest (§10.3).
// Отдельный mode `e2e` грузит `.env.e2e` → `VITE_ENABLE_MSW=true` →
// `src/main.tsx` запускает worker до `createRoot()`. Этим отвязываемся
// от реального Orchestrator (:8080) в CI.
//
// webServer.reuseExistingServer: в локальной разработке можно держать
// `npm run dev` в соседнем терминале — Playwright не поднимет второй.
// В CI (`CI=true`) — всегда свежий сервер, чтобы не унаследовать состояние.

import { defineConfig, devices } from '@playwright/test';

const PORT = 5173;
const BASE_URL = `http://localhost:${PORT}`;
const isCI = Boolean(process.env.CI);

export default defineConfig({
  testDir: './tests/e2e',
  // .spec.ts — только e2e-спецы, не пересекается с vitest `.test.ts`.
  testMatch: /.*\.spec\.ts$/,

  // §10.4: блокирующие a11y-нарушения — fail CI. Retries в CI допустимы
  // только для flakiness, не для скрытия регрессий.
  fullyParallel: true,
  forbidOnly: isCI,
  retries: isCI ? 2 : 0,
  // Локально — один worker'ов столько, сколько ядер; в CI — явное ограничение
  // для детерминизма логов и Docker-runner'ов с переменной CPU-квотой.
  workers: isCI ? 2 : undefined,

  reporter: isCI ? [['list'], ['html', { open: 'never' }]] : [['list'], ['html']],

  timeout: 30_000,
  expect: { timeout: 5_000 },

  use: {
    baseURL: BASE_URL,
    // Детерминированная временная зона (jsdom и Node дают разные тосты по
    // `Intl.DateTimeFormat().resolvedOptions().timeZone` — а snapshot-тесты
    // на дату/время станут flaky). Europe/Moscow — основной рынок v1.
    timezoneId: 'Europe/Moscow',
    locale: 'ru-RU',
    trace: isCI ? 'retain-on-failure' : 'on-first-retry',
    screenshot: 'only-on-failure',
    video: isCI ? 'retain-on-failure' : 'off',
    // Блокируем реальные исходящие запросы в CI (всё должно идти через MSW).
    // В dev-режиме можно оставить actionTimeout побольше для отладки.
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  webServer: {
    // `vite --mode e2e` → dotenv подхватывает Frontend/.env.e2e
    // (`VITE_ENABLE_MSW=true`). --strictPort, чтобы не мигрировать на 5174
    // при занятом порту (CI должен падать, а не рандомно проходить).
    command: 'npm run dev:e2e',
    url: BASE_URL,
    reuseExistingServer: !isCI,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
