// FE-TASK-053 — Vitest 1.6 + Testing Library setup + coverage.
// Конфигурация соответствует §10.2-10.4 high-architecture:
//   unit+integration через RTL; coverage thresholds применяются только к
//   shared/* и entities/* (lines/statements ≥ 80%, branches ≥ 75%).
// Остальные слои (features/widgets/pages/app/processes) — без минимального
// порога: инвариант §10.4 говорит именно про foundational-код, а UI-покрытие
// дополнительно проверяется Storybook + Chromatic (visual regression) и
// Playwright (e2e) в отдельных задачах FE-TASK-055.
import path from 'node:path';

import { defineConfig } from 'vitest/config';

export default defineConfig({
  resolve: {
    alias: { '@': path.resolve(__dirname, 'src') },
  },
  test: {
    // jsdom-по-умолчанию — тесты RTL составляют большинство (widgets, pages,
    // features). Pure-node тесты (schemas, mappers, stores без persist) тоже
    // корректно прогоняются в jsdom; оверхед ~50-100 мс/файл приемлем.
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    // URL нужен jsdom-окружению: без валидного origin localStorage и
    // sessionStorage отдают SecurityError (opaque origin) и ломают
    // persist-middleware Zustand.
    environmentOptions: {
      jsdom: {
        url: 'http://localhost/',
      },
    },
    // Полифил Storage + регистрация @testing-library/jest-dom matchers
    // (toBeInTheDocument, toHaveAttribute и т.д.).
    setupFiles: ['./src/test-setup.ts'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html'],
      reportsDirectory: './coverage',
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.d.ts',
        'src/**/*.{test,spec}.{ts,tsx}',
        'src/**/*.stories.{ts,tsx}',
        'src/**/__tests__/**',
        'src/**/__mocks__/**',
        'src/**/index.ts',
        'src/main.tsx',
        'src/vite-env.d.ts',
        'src/test-setup.ts',
      ],
      // Per-glob thresholds — только для foundational-слоёв. Pragma §10.4.
      thresholds: {
        'src/shared/**/*.{ts,tsx}': {
          lines: 80,
          statements: 80,
          branches: 75,
          functions: 80,
        },
        'src/entities/**/*.{ts,tsx}': {
          lines: 80,
          statements: 80,
          branches: 75,
          functions: 80,
        },
      },
    },
  },
});
