// Browser-worker для dev-сервера и Storybook. Относительный baseURL '/api/v1'
// матчит реальный origin браузера (http://localhost:5173 в dev, фактический
// домен в preview/staging).
//
// Старт в dev: main.tsx → if (import.meta.env.DEV && VITE_MSW === 'true')
// await worker.start(). В Storybook — через msw-storybook-addon (preview.ts).
//
// E2E role override: Playwright через addInitScript кладёт window.__cpE2eRole__
// до старта приложения. Перед worker.start() читаем значение и применяем
// setE2EUserRole — GET /users/me вернёт соответствующий fixture.

import { setupWorker } from 'msw/browser';

import type { components } from '@/shared/api/openapi';

import { createHandlers } from './handlers';
import { setE2EUserRole } from './handlers/users';

type UserRole = components['schemas']['UserProfile']['role'];

declare global {
  interface Window {
    __cpE2eRole__?: UserRole;
  }
}

export const WORKER_BASE_URL = '/api/v1';

export const worker = setupWorker(...createHandlers(WORKER_BASE_URL));

/** Применяет e2e-role из window (если есть). Вызывать сразу до/после worker.start(). */
export function applyE2ERoleOverride(): void {
  const role = typeof window !== 'undefined' ? window.__cpE2eRole__ : undefined;
  setE2EUserRole(role ?? null);
}
