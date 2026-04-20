import type { ErrorEvent } from '@sentry/react';

const SENSITIVE_KEY_RE =
  /^(authorization|proxy-authorization|authentication|bearer|cookie|set-cookie|password|pass|passwd|token|id[_-]?token|access[_-]?token|refresh[_-]?token|api[_-]?key|x-api-key|x-access-token|x-auth-token|secret|x-csrf-token)$/i;

// Bearer token: `Bearer <любые непробельные>` — покрывает base64url с +/=
// и непрозрачные OAuth-токены (не только JWT).
const BEARER_RE = /Bearer\s+\S+/gi;
const JWT_RE = /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g;
// Разделитель `=` или fragment `#access_token=...` (Implicit flow).
const QUERY_TOKEN_KEYS =
  'access_token|refresh_token|id_token|token|api_key|apikey|password|code|assertion|signature|sig|session';
const QUERY_TOKEN_RE = new RegExp(`((?:^|[?&#])(?:${QUERY_TOKEN_KEYS})=)[^&#\\s]+`, 'gi');

const REDACTED = '[Filtered]';
const MAX_DEPTH = 8;

/**
 * Redact чувствительной подстроки в строке: Bearer-токены, JWT,
 * query-параметры вида `token=...`. Используется из `scrubSentryEvent`
 * и напрямую в `beforeSendReplay` (Replay URL network events).
 */
export function redactString(input: string): string {
  return input
    .replace(BEARER_RE, `Bearer ${REDACTED}`)
    .replace(JWT_RE, REDACTED)
    .replace(QUERY_TOKEN_RE, `$1${REDACTED}`);
}

function redactValue(value: unknown, depth: number, seen: WeakSet<object>): unknown {
  if (value == null) return value;
  if (typeof value === 'string') return redactString(value);
  if (typeof value !== 'object') return value;
  if (depth >= MAX_DEPTH) return value;
  if (seen.has(value as object)) return value;
  seen.add(value as object);

  if (Array.isArray(value)) {
    return value.map((v) => redactValue(v, depth + 1, seen));
  }

  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
    if (SENSITIVE_KEY_RE.test(k)) {
      out[k] = REDACTED;
      continue;
    }
    out[k] = redactValue(v, depth + 1, seen);
  }
  return out;
}

/**
 * §14.2 privacy scrubber. Запускается в `beforeSend` Sentry SDK:
 * удаляет чувствительные заголовки (Authorization, Cookie, X-API-Key, ...),
 * JWT- и Bearer-токены в строковых значениях (URL, message, breadcrumbs),
 * query-параметры с именами token/password/api_key.
 *
 * Пройдёт по structured путям event.request / breadcrumbs / extra / contexts,
 * регекс по string-листьям — вторая линия защиты от утечек в message/URL.
 */
export function scrubSentryEvent(event: ErrorEvent): ErrorEvent {
  const seen = new WeakSet<object>();
  const mut = event as unknown as Record<string, unknown>;

  if (event.request) {
    mut.request = redactValue(event.request, 0, seen);
  }
  if (event.breadcrumbs) {
    mut.breadcrumbs = redactValue(event.breadcrumbs, 0, seen);
  }
  if (event.contexts) {
    mut.contexts = redactValue(event.contexts, 0, seen);
  }
  if (event.extra) {
    mut.extra = redactValue(event.extra, 0, seen);
  }
  if (event.tags) {
    mut.tags = redactValue(event.tags, 0, seen);
  }
  if (typeof event.message === 'string') {
    event.message = redactString(event.message);
  }
  if (event.exception?.values) {
    for (const exc of event.exception.values) {
      if (typeof exc.value === 'string') {
        exc.value = redactString(exc.value);
      }
    }
  }
  return event;
}
