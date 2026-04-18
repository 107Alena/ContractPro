// ContractPro Frontend — placeholder runtime-config (§13.5 high-architecture).
//
// Этот файл копируется Vite как есть в dist/config.js. В production nginx
// `docker/entrypoint.sh` перезаписывает его на старте контейнера реальными
// значениями из ENV. В dev-режиме (vite dev server) файл отдаётся как есть и
// оставляет window.__ENV__ пустым — shared/config/runtime-env.ts корректно
// возвращает {} и все фича-флаги считаются выключенными.
window.__ENV__ = {
  API_BASE_URL: '/api/v1',
  SENTRY_DSN: '',
  OTEL_ENDPOINT: '',
};
