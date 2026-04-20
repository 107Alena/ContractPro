import './app/styles/index.css';
import './shared/i18n/config';

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './app/App';
import { initAuthFlow } from './processes/auth-flow';
import { initOtel, initSentry } from './shared/observability';

/**
 * Bootstrap (§10.3 unified MSW для dev/test/Storybook/e2e, FE-TASK-055).
 *
 * MSW-worker запускается ТОЛЬКО когда одновременно выполнены условия:
 *   - `import.meta.env.DEV` (гарантирует, что в prod build `vite build`
 *     ветка мертва, и rollup удаляет `await import(...)` вместе с графом
 *     `tests/msw/*` — `dist/assets/*.js` не должен содержать msw-кода);
 *   - `VITE_ENABLE_MSW === 'true'` — явный opt-in через `.env.e2e` или
 *     локальный `.env.development.local` для hand-on разработки без backend.
 *
 * Ordering (§5.1, §5.3, §14.3): worker.start → initSentry → initOtel → initAuthFlow → render.
 * worker.start обязан завершиться ДО createRoot: React Router data-loaders
 * стартуют синхронно с mount'ом, и первый fetch должен попасть под mock.
 * initOtel обязан запуститься ДО первого fetch/XHR — автоматическая
 * инструментация патчит `window.fetch` / `XMLHttpRequest` (§14.3, FE-TASK-051).
 */
async function bootstrap(): Promise<void> {
  if (import.meta.env.DEV && import.meta.env.VITE_ENABLE_MSW === 'true') {
    const { worker, applyE2ERoleOverride } = await import('../tests/msw/browser');
    await worker.start({
      // `bypass` — незамоканные запросы идут в сеть; для e2e это, например,
      // Vite HMR-клиент и /config.js (runtime-env, FE-TASK-009). Строгий
      // `error` ломает dev-сервер, `warn` засоряет консоль предсказуемым шумом.
      onUnhandledRequest: 'bypass',
      serviceWorker: { url: '/mockServiceWorker.js' },
    });
    // E2E role override из Playwright addInitScript (tests/e2e/fixtures/auth-state.ts).
    applyE2ERoleOverride();
  }

  initSentry();
  initOtel();
  initAuthFlow();

  const container = document.getElementById('root');
  if (!container) {
    throw new Error('Root container #root not found in index.html');
  }

  createRoot(container).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}

void bootstrap();
