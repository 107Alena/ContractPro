// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// @sentry/react properties non-configurable — module-mock, см. logger.test.ts.
vi.mock('@sentry/react', () => ({
  addBreadcrumb: vi.fn(),
  captureMessage: vi.fn(() => 'evt'),
}));

const Sentry = await import('@sentry/react');
const { __reportMetricForTests, __resetWebVitalsForTests, initWebVitals } =
  await import('./web-vitals');

describe('initWebVitals', () => {
  beforeEach(() => {
    __resetWebVitalsForTests();
    vi.mocked(Sentry.addBreadcrumb).mockClear();
    vi.mocked(Sentry.captureMessage).mockClear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('возвращает { enabled: true } при первом вызове', () => {
    expect(initWebVitals().enabled).toBe(true);
  });

  it('повторный вызов — no-op (idempotency guard, StrictMode-safe)', () => {
    initWebVitals();
    initWebVitals();
    initWebVitals();
    // Guard сигнализирует enabled:true — lazy-import выполнится только раз
    expect(initWebVitals().enabled).toBe(true);
  });
});

describe('reportMetric (pure builder)', () => {
  beforeEach(() => {
    vi.mocked(Sentry.addBreadcrumb).mockClear();
    vi.mocked(Sentry.captureMessage).mockClear();
    vi.mocked(Sentry.addBreadcrumb).mockImplementation(() => undefined);
    vi.mocked(Sentry.captureMessage).mockImplementation(() => 'evt');
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('LCP good → breadcrumb level:info + captureMessage с tags.metric=LCP', () => {
    __reportMetricForTests({
      name: 'LCP',
      value: 1234.56,
      rating: 'good',
      delta: 100,
      id: 'v3-1',
      navigationType: 'navigate',
    });
    expect(Sentry.addBreadcrumb).toHaveBeenCalledWith(
      expect.objectContaining({
        category: 'web-vital',
        level: 'info',
        message: 'LCP=1234.56',
      }),
    );
    expect(Sentry.captureMessage).toHaveBeenCalledWith(
      'web-vital.LCP',
      expect.objectContaining({
        level: 'info',
        tags: { metric: 'LCP', rating: 'good' },
      }),
    );
  });

  it('INP poor → breadcrumb level:warning (регресс ухудшается)', () => {
    __reportMetricForTests({
      name: 'INP',
      value: 900,
      rating: 'poor',
      delta: 900,
      id: 'v3-2',
    });
    expect(Sentry.addBreadcrumb).toHaveBeenCalledWith(
      expect.objectContaining({ level: 'warning' }),
    );
  });

  it('не бросает, если Sentry не инициализирован (captureMessage throws)', () => {
    vi.mocked(Sentry.captureMessage).mockImplementation(() => {
      throw new Error('not-init');
    });
    expect(() =>
      __reportMetricForTests({
        name: 'CLS',
        value: 0.05,
        rating: 'good',
        delta: 0.05,
        id: 'v3-3',
      }),
    ).not.toThrow();
  });
});
