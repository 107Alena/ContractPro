// Кастомные RUM-события (§14.4 high-architecture, FE-TASK-052).
//
// Event names (стабильный enum — часть telemetry-контракта):
//   - `contract.upload.started`    — старт загрузки договора
//   - `contract.upload.completed`  — успешное 202 → SSE
//   - `sse.reconnect`              — очередной reconnect SSE (с retry counter'ом)
//   - `auth.refresh.failed`        — POST /auth/refresh завершился 401/сетевой ошибкой
//
// Транспорт: `Sentry.addBreadcrumb({category:'rum', ...})` + best-effort
// `trace.getActiveSpan()?.addEvent(...)` — если OTel-span активен (например,
// внутри fetch-handler'а). scrubber (§14.2) сработает через Sentry `beforeSend`
// перед отправкой; caller не должен класть в `attrs` raw-токены.
//
// Tracing: распределённого trace для SSE-reconnect и auth.refresh нет
// (EventSource не инструментирован §14.3), поэтому отдельный `span.addEvent`
// там будет no-op — это acceptable.
import { trace } from '@opentelemetry/api';
import * as Sentry from '@sentry/react';

export type RumEventName =
  | 'contract.upload.started'
  | 'contract.upload.completed'
  | 'sse.reconnect'
  | 'auth.refresh.failed';

export type RumEventAttrs = Record<string, string | number | boolean | undefined>;

function stripUndefined(attrs: RumEventAttrs): Record<string, string | number | boolean> {
  const out: Record<string, string | number | boolean> = {};
  for (const [k, v] of Object.entries(attrs)) {
    if (v !== undefined) out[k] = v;
  }
  return out;
}

/**
 * Испустить RUM-событие. Никогда не бросает — telemetry не ломает UX.
 *
 * Почему breadcrumb, а не `captureMessage`: RUM-события — высокочастотные
 * (несколько в минуту на активной сессии), в Sentry issues их материализовать
 * не нужно. Breadcrumb'ы оседают в следующей Error/Message-event'е как
 * контекст — то что нужно для диагностики «что делал пользователь перед багом».
 */
export function emitRumEvent(name: RumEventName, attrs: RumEventAttrs = {}): void {
  const cleaned = stripUndefined(attrs);
  try {
    Sentry.addBreadcrumb({
      category: 'rum',
      type: 'info',
      level: 'info',
      message: name,
      data: { name, ...cleaned },
    });
  } catch {
    // ignore — Sentry не инициализирован (dev без DSN) или beforeSend упал.
  }
  try {
    const span = trace.getActiveSpan();
    if (span) {
      span.addEvent(name, cleaned);
    }
  } catch {
    // OTel не инициализирован / trace.disable() — тихо игнорируем.
  }
}
