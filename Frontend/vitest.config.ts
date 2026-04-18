// Минимальный Vitest-config для FE-TASK-026.
// Покрывает только unit-тесты уровня shared/* (pure JS/TS, без React).
// FE-TASK-053 расширит: jsdom environment, setup-файл (@testing-library/jest-dom),
// coverage thresholds (lines ≥ 80%, branches ≥ 75% для shared/*, entities/*),
// test-скрипт переключится на watch + добавится test:ci с --coverage.
import path from 'node:path';

import { defineConfig } from 'vitest/config';

export default defineConfig({
  resolve: {
    alias: { '@': path.resolve(__dirname, 'src') },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    // jsdom-тестам нужна полноценная URL origin, иначе localStorage отдаёт
    // SecurityError (opaque origin) — ломает persist-middleware Zustand в
    // layout-store и любые тесты, использующие localStorage напрямую.
    environmentOptions: {
      jsdom: {
        url: 'http://localhost/',
      },
    },
    // Полифил Storage в jsdom (issue с vitest 1.6.1 + jsdom 24.1.3 — прототипы
    // localStorage/sessionStorage теряются после populateGlobal).
    // Подключён в setupFiles, т.к. ориентирован только на jsdom-тесты; в
    // node-окружении `window === undefined`, полифил safely no-op.
    setupFiles: ['./src/test-setup.ts'],
  },
});
