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
        // §6.3 / §11.2 high-architecture — granular code-splitting:
        // - chunks/admin    — все admin-страницы (FE-TASK-001/002), один общий chunk
        // - vendor/react    — React + ReactDOM + scheduler
        // - vendor/router   — react-router и data-router
        // - vendor/query    — TanStack Query + devtools
        // - vendor/i18n     — i18next + react-i18next
        // - vendor/sentry   — @sentry/react (release-tagged source-maps в FE-TASK-050)
        // TODO(FE-TASK-038/039): chunks/diff-viewer (diff-match-patch) и chunks/pdf-preview
        //   (pdfjs-dist) добавятся одновременно с реализацией соответствующих widgets/features.
        manualChunks: (id: string) => {
          if (id.includes('/src/pages/admin-')) return 'chunks/admin';
          if (id.includes('node_modules')) {
            if (id.includes('react-dom') || id.includes('/react/') || id.includes('scheduler'))
              return 'vendor/react';
            if (id.includes('react-router')) return 'vendor/router';
            if (id.includes('@tanstack')) return 'vendor/query';
            if (id.includes('i18next')) return 'vendor/i18n';
            if (id.includes('@sentry')) return 'vendor/sentry';
          }
          return undefined;
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
  },
});
