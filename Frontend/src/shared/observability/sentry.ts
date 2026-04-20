import * as Sentry from '@sentry/react';

import { getRuntimeEnv, type RuntimeEnv } from '@/shared/config';

import { scrubSentryEvent } from './scrubber';

export interface InitSentryResult {
  enabled: boolean;
}

/**
 * Git SHA, инжектируемый на build-time через Vite `define` в
 * `vite.config.ts` (`__GIT_SHA__`). Если переменная пустая (локальный
 * `vite build` без `VITE_GIT_SHA` и без git-контекста), release в
 * конфигурацию не попадает — события сгруппируются в дефолтный release.
 */
declare const __GIT_SHA__: string;

type SentryInitConfig = Parameters<typeof Sentry.init>[0];

/**
 * Чистый билдер Sentry-конфига — выделен отдельной функцией для тестирования
 * без мока `Sentry.init` (свойство non-configurable при выбранной версии SDK).
 * Возвращает null, если DSN отсутствует — вызвавший должен пропустить init.
 *
 * Privacy:
 *   - `sendDefaultPii: false` — Sentry SDK не собирает IP / user-agent по умолчанию.
 *   - Session Replay: `maskAllText`, `blockAllMedia`, `maskAllInputs`; network body
 *     capture отключён (`networkDetailAllowUrls: []`, `networkCaptureBodies: false`).
 *   - `beforeSend` → `scrubSentryEvent`: удаляет Authorization/JWT/Bearer/password
 *     из headers, body, breadcrumbs, extra, URL-query. Обёрнут в try/catch: если
 *     scrubber упадёт на кривом event, возвращаем `null` — событие отбрасывается.
 *   - Для replay network events отдельного beforeSend-хука в SDK v8 нет; URL'ы
 *     защищены дисциплиной приложения (токены в Authorization header, не query)
 *     плюс `networkDetailAllowUrls: []`, исключающий перехват body/headers.
 *
 * Tracing:
 *   - `tracesSampleRate: 0` — distributed tracing покрывается OpenTelemetry SDK
 *     (см. §14.3, FE-TASK-051). Sentry Performance остаётся отключённым, чтобы
 *     избежать двойной instrumentation fetch и конфликта traceparent.
 */
export function buildSentryConfig(
  env: RuntimeEnv,
  gitSha: string,
  mode: string,
): SentryInitConfig | null {
  const { SENTRY_DSN } = env;
  if (!SENTRY_DSN) return null;

  const release = gitSha ? `contractpro-frontend@${gitSha}` : undefined;

  return {
    dsn: SENTRY_DSN,
    environment: env.SENTRY_ENVIRONMENT ?? mode,
    ...(release ? { release } : {}),
    sendDefaultPii: false,
    tracesSampleRate: 0,
    replaysSessionSampleRate: 0.1,
    replaysOnErrorSampleRate: 1.0,
    integrations: [
      Sentry.replayIntegration({
        maskAllText: true,
        blockAllMedia: true,
        maskAllInputs: true,
        networkDetailAllowUrls: [],
        networkCaptureBodies: false,
      }),
    ],
    beforeSend: (event) => {
      try {
        return scrubSentryEvent(event);
      } catch {
        // При падении scrubber'а дропаем событие, не отправляем сырое.
        return null;
      }
    },
  };
}

/**
 * Инициализирует Sentry, если в runtime-env задан DSN.
 * При пустом DSN — no-op (типично для локального dev без Sentry-проекта).
 * §14.2 high-architecture.
 *
 * Слои конфигурации:
 *   - DSN + environment → runtime (`window.__ENV__`): один образ для dev/staging/prod.
 *   - release (git SHA) → build-time (Vite `define __GIT_SHA__`): должен совпасть
 *     с source-maps, залитыми в release через `sentry-cli sourcemaps upload`.
 *
 * Privacy:
 *   - Session Replay: `maskAllText: true`, `blockAllMedia: true`,
 *     `networkDetailAllowUrls: []`, `networkCaptureBodies: false` —
 *     replay не захватывает тексты/тела запросов (scrubber не применяется
 *     к replay-каналу).
 *   - `beforeSend` → `scrubSentryEvent` — удаляет Authorization/token/password
 *     из заголовков, body, breadcrumbs, URLs.
 */
export function initSentry(): InitSentryResult {
  const gitSha = typeof __GIT_SHA__ !== 'undefined' ? __GIT_SHA__ : '';
  const config = buildSentryConfig(getRuntimeEnv(), gitSha, import.meta.env.MODE);
  if (!config) return { enabled: false };
  Sentry.init(config);
  return { enabled: true };
}

export { Sentry };
