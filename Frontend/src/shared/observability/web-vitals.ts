// web-vitals → Sentry pipeline (§14.4 high-architecture, FE-TASK-052).
//
// Метрики: `LCP`, `INP`, `CLS`, `TTFB`. Кажый callback'ом web-vitals шлёт
// `Sentry.addBreadcrumb` (контекст для последующих events) + одноразовый
// `Sentry.captureMessage('web-vital.{METRIC}', tags:{metric, rating})` —
// материализуется как Sentry issue, группируется по названию метрики.
//
// Почему не Sentry Performance: `tracesSampleRate: 0` (§14.2 комментарий
// `sentry.ts:37-40`) — distributed tracing покрывается OpenTelemetry.
// Отдельный span-transaction для web-vitals создавал бы двойную
// instrumentation.  Sentry v8 метрики-product свёрнут в 2024–2025 (Custom
// Metrics deprecation) — не закладываемся на него.
//
// Порядок init: `initWebVitals()` — ПОСЛЕ `initSentry` (иначе captureMessage
// no-op) и ДО первого paint (LCP/TTFB фиксируются через `PerformanceObserver`,
// который должен быть установлен к моменту paint/navigation). В `main.tsx`
// вызывается сразу после `initOtel()` (до `createRoot`).
//
// StrictMode: вызов — из `main.tsx`, вне React-дерева; double-mount невозможен.
// Дополнительно — module-level `initialized`-guard (как в `initOtel`).
import * as Sentry from '@sentry/react';

export interface InitWebVitalsResult {
  enabled: boolean;
}

interface WebVitalMetric {
  name: 'LCP' | 'INP' | 'CLS' | 'TTFB' | 'FCP';
  value: number;
  rating: 'good' | 'needs-improvement' | 'poor';
  delta: number;
  id: string;
  navigationType?: string;
}

function reportMetric(metric: WebVitalMetric): void {
  const data = {
    metric: metric.name,
    value: metric.value,
    rating: metric.rating,
    delta: metric.delta,
    id: metric.id,
    navigationType: metric.navigationType,
  };
  try {
    Sentry.addBreadcrumb({
      category: 'web-vital',
      level: metric.rating === 'poor' ? 'warning' : 'info',
      message: `${metric.name}=${metric.value.toFixed(2)}`,
      data,
    });
    Sentry.captureMessage(`web-vital.${metric.name}`, {
      level: 'info',
      tags: { metric: metric.name, rating: metric.rating },
      extra: data,
    });
  } catch {
    // Sentry недоступен (init пропущен) — молча роняем, не ломаем UX.
  }
}

let initialized = false;

/**
 * Регистрирует web-vitals callback'и (LCP/INP/CLS/TTFB).
 * Lazy-import — изолирует зависимость в отдельный chunk (tree-shake friendly)
 * и не тянет `web-vitals` в critical-path main-бандла.
 *
 * При повторном вызове — no-op (StrictMode-safe + защита от двойного
 * bootstrap'а).
 */
export function initWebVitals(): InitWebVitalsResult {
  if (initialized) return { enabled: true };
  if (typeof window === 'undefined') return { enabled: false };
  initialized = true;

  void import('web-vitals').then(({ onLCP, onINP, onCLS, onTTFB }) => {
    onLCP(reportMetric);
    onINP(reportMetric);
    onCLS(reportMetric);
    onTTFB(reportMetric);
  });

  return { enabled: true };
}

/** Для тестов: сброс idempotency guard'а. */
export function __resetWebVitalsForTests(): void {
  initialized = false;
}

/** Экспорт reporter'а для unit-тестов (обход lazy-import). */
export const __reportMetricForTests = reportMetric;
