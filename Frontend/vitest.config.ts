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
  },
});
