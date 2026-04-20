import path from 'node:path';

import react from '@vitejs/plugin-react-swc';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    target: 'es2022',
    rollupOptions: {
      output: {
        // Отдельное имя для entry-chunk'а — отличает main от lazy-страниц
        // `src/pages/*/index.tsx`, которые Rollup именует `index-*.js`. Нужно для
        // size-limit (§13.3, FE-TASK-010): стабильный glob `assets/main-*.js`
        // позволяет bound'ить только main-bundle (≤200 КБ gzip).
        entryFileNames: 'assets/main-[hash].js',
        // §6.3 / §11.2 high-architecture — granular code-splitting:
        // - chunks/admin        — все admin-страницы (FE-TASK-001/002), один общий chunk
        // - chunks/diff-viewer  — widgets/diff-viewer + diff-match-patch (FE-TASK-047),
        //                         budget ≤150 КБ gzip; нужен только для /compare.
        // - chunks/pdf-preview  — widgets/pdf-navigator (stub в FE-TASK-045; pdfjs-dist
        //                         присоединится позже, см. §6.3 «~500 КБ»). Стабильное
        //                         имя chunk'a нужно для size-limit-бюджета и e2e (AC
        //                         «lazy-loaded chunk»).
        // - vendor/react        — React + ReactDOM + scheduler
        // - vendor/router       — react-router и data-router
        // - vendor/query        — TanStack Query + devtools
        // - vendor/i18n         — i18next + react-i18next
        // - vendor/sentry       — @sentry/react (release-tagged source-maps в FE-TASK-050)
        // - vendor/ui-utils     — class-variance-authority, clsx, tailwind-merge:
        //                         shared между ~всеми UI-компонентами; без явного
        //                         правила Rollup дублирует их в каждом lazy chunk
        //                         (замерено в FE-TASK-001: +7 КБ gzip в chunks/admin).
        manualChunks: (id: string) => {
          if (id.includes('/src/pages/admin-')) return 'chunks/admin';
          if (id.includes('/src/widgets/diff-viewer/')) return 'chunks/diff-viewer';
          if (id.includes('/src/widgets/pdf-navigator/')) return 'chunks/pdf-preview';
          if (id.includes('node_modules')) {
            if (id.includes('diff-match-patch')) return 'chunks/diff-viewer';
            if (id.includes('pdfjs-dist')) return 'chunks/pdf-preview';
            if (id.includes('react-dom') || id.includes('/react/') || id.includes('scheduler'))
              return 'vendor/react';
            if (id.includes('react-router')) return 'vendor/router';
            if (id.includes('@tanstack')) return 'vendor/query';
            if (id.includes('i18next')) return 'vendor/i18n';
            if (id.includes('@sentry')) return 'vendor/sentry';
            if (
              id.includes('class-variance-authority') ||
              id.includes('tailwind-merge') ||
              /[\\/]clsx[\\/]/.test(id)
            )
              return 'vendor/ui-utils';
          }
          return undefined;
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    // Dev-proxy зеркалит §13.2 high-architecture (production nginx) + ADR-6 (same-origin
    // deployment в backend). Браузер обращается к относительному /api/v1/* на :5173 —
    // CORS не активируется, traceparent (§14.3) идёт без preflight.
    //
    // Regex-ключи (longest-match) гарантируют, что точный путь SSE-стрима матчится раньше
    // общего /api/* (см. §7.7 — EventSource на /api/v1/events/stream).
    proxy: {
      '^/api/v1/events/stream(?:\\?.*)?$': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: false,
        // proxy_read_timeout 24h аналог из §13.2 — long-lived SSE.
        proxyTimeout: 24 * 60 * 60 * 1000,
        timeout: 24 * 60 * 60 * 1000,
        configure: (proxy) => {
          proxy.on('proxyRes', (proxyRes) => {
            // Защитный слой: Go-orchestrator на net/http+Flusher по умолчанию НЕ ставит
            // Content-Length для text/event-stream, но если он его выставит — http-proxy
            // не переключится в chunked transfer и буферизация съест real-time стрим
            // (§13.2:proxy_buffering off).
            delete proxyRes.headers['content-length'];
          });
        },
      },
      '^/api/': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: false,
      },
    },
  },
});
