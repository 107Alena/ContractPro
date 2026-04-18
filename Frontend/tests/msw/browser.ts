// Browser-worker для dev-сервера и Storybook. Относительный baseURL '/api/v1'
// матчит реальный origin браузера (http://localhost:5173 в dev, фактический
// домен в preview/staging).
//
// Старт в dev: main.tsx → if (import.meta.env.DEV && VITE_MSW === 'true')
// await worker.start(). В Storybook — через msw-storybook-addon (preview.ts).

import { setupWorker } from 'msw/browser';

import { createHandlers } from './handlers';

export const WORKER_BASE_URL = '/api/v1';

export const worker = setupWorker(...createHandlers(WORKER_BASE_URL));
