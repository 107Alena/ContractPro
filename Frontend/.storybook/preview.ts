import type { Preview } from '@storybook/react';
// FE-TASK-054 — msw-storybook-addon подключает MSW service worker в превью.
// initialize() вызывается в top-level до первого рендера (канонический
// паттерн addon'а). mswLoader авто-прокидывает `parameters.msw.handlers` из
// каждой story в server. По умолчанию story-specific handlers не заданы —
// worker отвечает из глобального набора (tests/msw/browser.ts).
import { initialize, mswLoader } from 'msw-storybook-addon';

import { worker } from '../tests/msw/browser';

import '../src/app/styles/index.css';

// Публичный URL воркера совпадает со сгенерированным `npx msw init public/`.
// `onUnhandledRequest: 'bypass'` — запросы не к /api/v1 (например, Chromatic
// CDN, addon-HMR) не блокируются.
initialize({
  onUnhandledRequest: 'bypass',
  serviceWorker: { url: './mockServiceWorker.js' },
});

// Единый набор handlers с browser-worker. Story может переопределить через
// `parameters.msw.handlers` — mswLoader вызовет worker.use(...).
void worker;

const preview: Preview = {
  loaders: [mswLoader],
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    backgrounds: {
      default: 'surface',
      values: [
        { name: 'surface', value: 'var(--color-bg)' },
        { name: 'muted', value: 'var(--color-bg-muted)' },
      ],
    },
    a11y: {
      // WCAG 2.1 AA — axe-core теги. Блокирующие нарушения фейлят Chromatic-gate
      // и play-функции через @storybook/addon-interactions.
      config: {
        rules: [
          { id: 'color-contrast', enabled: true },
          { id: 'label', enabled: true },
          { id: 'button-name', enabled: true },
          { id: 'aria-valid-attr', enabled: true },
          { id: 'aria-required-attr', enabled: true },
        ],
      },
      options: {
        runOnly: {
          type: 'tag',
          values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'],
        },
      },
    },
    options: {
      storySort: {
        order: ['Welcome', 'Shared', 'Entities', 'Features', 'Widgets', 'Pages'],
      },
    },
  },
};

export default preview;
