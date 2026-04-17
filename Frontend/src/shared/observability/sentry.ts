import * as Sentry from '@sentry/react';

import { getRuntimeEnv } from '@/shared/config';

export interface InitSentryResult {
  enabled: boolean;
}

/**
 * Инициализирует Sentry, если в runtime-env задан DSN.
 * При пустом DSN — no-op (типично для локального dev без Sentry-проекта).
 * §14.2 high-architecture.
 */
export function initSentry(): InitSentryResult {
  const { SENTRY_DSN } = getRuntimeEnv();
  if (!SENTRY_DSN) {
    return { enabled: false };
  }

  Sentry.init({
    dsn: SENTRY_DSN,
    environment: import.meta.env.MODE,
    tracesSampleRate: 0.1,
    replaysSessionSampleRate: 0,
    replaysOnErrorSampleRate: 0,
  });

  return { enabled: true };
}

export { Sentry };
