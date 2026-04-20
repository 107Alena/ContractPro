// Structured logger wrapper (§14.1 high-architecture, FE-TASK-052).
//
// Методы `debug | info | warn | error` обогащают каждое событие
// `{ correlation_id?, user_id?, org_id?, route?, release? }`:
//   - `correlation_id` — передаётся вызывающим через `ctx.correlation_id`
//     (обычно из `err.correlation_id`); logger САМ не резолвит — в браузере
//     нет AsyncLocalStorage, X-Correlation-Id живёт в axios-interceptor'е
//     (§7.2) и недоступен синхронно на стороне произвольного callsite.
//   - `user_id`, `org_id` — лениво из `sessionStore.getState().user`
//     (события до авторизации сохраняют актуальные значения после login).
//   - `route` — `window.location.pathname` в момент вызова; в SPA
//     каждый вызов уже актуален, history-listener не нужен.
//   - `release` — build-time `__GIT_SHA__` (vite `define`, vite.config.ts).
//
// Transport:
//   - dev (`import.meta.env.MODE !== 'production'`) — `console.debug/info/warn/error`
//     с префиксом `[contractpro]`, без scrubber'а (acceptable risk: caller
//     не должен передавать raw-токены в ctx; документируется контрактом).
//   - prod — `warn` → `Sentry.addBreadcrumb({level:'warning', category:'log', ...})`;
//     `error` → `Sentry.captureException(err)` + `Sentry.captureMessage(msg, ...)` —
//     оба идут через `beforeSend` scrubber (§14.2, FE-TASK-050), PII фильтруется.
//     `debug`/`info` в prod — no-op (не замусоривает Breadcrumbs-кольцо).
//
// Recursion guard: module-level `inFlight` исключает бесконечный цикл,
// если logger вызывается из `beforeSend`-scrubber'а или Sentry-integration'ов.
import * as Sentry from '@sentry/react';

import { sessionStore } from '@/shared/auth/session-store';

export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

export interface LogContext {
  correlation_id?: string;
  [key: string]: unknown;
}

export interface Logger {
  debug(message: string, ctx?: LogContext): void;
  info(message: string, ctx?: LogContext): void;
  warn(message: string, ctx?: LogContext): void;
  error(message: string, err?: unknown, ctx?: LogContext): void;
}

export interface EnrichedContext extends LogContext {
  user_id?: string;
  org_id?: string;
  route?: string;
  release?: string;
}

declare const __GIT_SHA__: string;

function safeGitSha(): string | undefined {
  try {
    return typeof __GIT_SHA__ !== 'undefined' && __GIT_SHA__ !== '' ? __GIT_SHA__ : undefined;
  } catch {
    return undefined;
  }
}

function safeRoute(): string | undefined {
  if (typeof window === 'undefined') return undefined;
  try {
    return window.location.pathname;
  } catch {
    return undefined;
  }
}

function enrich(ctx: LogContext | undefined): EnrichedContext {
  const base: EnrichedContext = { ...ctx };
  const user = sessionStore.getState().user;
  if (user) {
    base.user_id = user.user_id;
    base.org_id = user.organization_id;
  }
  const route = safeRoute();
  if (route) base.route = route;
  const release = safeGitSha();
  if (release) base.release = release;
  return base;
}

function isProduction(): boolean {
  // import.meta.env доступен в Vite; fallback — проверка process.env для
  // Node-окружений (тесты), которые мокают MODE через vi.stubEnv.
  try {
    return import.meta.env.MODE === 'production';
  } catch {
    return false;
  }
}

let inFlight = false;

function withGuard(fn: () => void, fallbackMessage: string): void {
  if (inFlight) {
    // Фолбэк — сырый console.error, минуя Sentry и logger: защита
    // от Sentry-beforeSend → logger → Sentry рекурсии.
    try {
      console.error('[contractpro] logger recursion:', fallbackMessage);
    } catch {
      // ignore
    }
    return;
  }
  inFlight = true;
  try {
    fn();
  } catch {
    // ignore — не даём transport-ошибке убить caller'а
  } finally {
    inFlight = false;
  }
}

function consoleOut(level: LogLevel, message: string, ctx: EnrichedContext): void {
  const fn = console[level] ?? console.log;
  try {
    fn.call(console, '[contractpro]', message, ctx);
  } catch {
    // ignore
  }
}

function prodWarn(message: string, ctx: EnrichedContext): void {
  Sentry.addBreadcrumb({
    category: 'log',
    level: 'warning',
    message,
    data: ctx,
  });
}

function prodError(message: string, err: unknown, ctx: EnrichedContext): void {
  if (err !== undefined) {
    Sentry.captureException(err, { extra: { message, ...ctx } });
  }
  Sentry.captureMessage(message, {
    level: 'error',
    extra: ctx,
  });
}

export const logger: Logger = {
  debug(message, ctx) {
    if (isProduction()) return;
    withGuard(() => {
      consoleOut('debug', message, enrich(ctx));
    }, message);
  },
  info(message, ctx) {
    if (isProduction()) return;
    withGuard(() => {
      consoleOut('info', message, enrich(ctx));
    }, message);
  },
  warn(message, ctx) {
    withGuard(() => {
      const enriched = enrich(ctx);
      if (isProduction()) {
        prodWarn(message, enriched);
      } else {
        consoleOut('warn', message, enriched);
      }
    }, message);
  },
  error(message, err, ctx) {
    withGuard(() => {
      const enriched = enrich(ctx);
      if (isProduction()) {
        prodError(message, err, enriched);
      } else {
        consoleOut('error', message, { ...enriched, ...(err !== undefined ? { err } : {}) });
      }
    }, message);
  },
};

/** Для тестов: сброс recursion-guard между кейсами. */
export function __resetLoggerForTests(): void {
  inFlight = false;
}
