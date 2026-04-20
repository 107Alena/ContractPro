// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// @sentry/react properties non-configurable — module-mock, см. logger.test.ts.
vi.mock('@sentry/react', () => ({
  addBreadcrumb: vi.fn(),
}));

vi.mock('@opentelemetry/api', () => ({
  trace: {
    getActiveSpan: vi.fn(() => undefined),
  },
}));

const Sentry = await import('@sentry/react');
const { trace } = await import('@opentelemetry/api');
const { emitRumEvent } = await import('./rum-events');

describe('emitRumEvent', () => {
  beforeEach(() => {
    vi.mocked(Sentry.addBreadcrumb).mockClear();
    vi.mocked(Sentry.addBreadcrumb).mockImplementation(() => undefined);
    vi.mocked(trace.getActiveSpan).mockReturnValue(undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('отправляет Sentry breadcrumb c category:rum + name в data', () => {
    emitRumEvent('contract.upload.started', { size_bytes: 1024, mime_type: 'application/pdf' });
    expect(Sentry.addBreadcrumb).toHaveBeenCalledWith({
      category: 'rum',
      type: 'info',
      level: 'info',
      message: 'contract.upload.started',
      data: {
        name: 'contract.upload.started',
        size_bytes: 1024,
        mime_type: 'application/pdf',
      },
    });
  });

  it('strip undefined — поля не попадают в breadcrumb.data', () => {
    emitRumEvent('contract.upload.started', { size_bytes: 10, mime_type: undefined });
    const call = vi.mocked(Sentry.addBreadcrumb).mock.calls[0]![0] as {
      data: Record<string, unknown>;
    };
    expect(call.data).not.toHaveProperty('mime_type');
    expect(call.data).toMatchObject({ name: 'contract.upload.started', size_bytes: 10 });
  });

  it('не бросает, если Sentry.addBreadcrumb падает', () => {
    vi.mocked(Sentry.addBreadcrumb).mockImplementation(() => {
      throw new Error('sentry-down');
    });
    expect(() => emitRumEvent('sse.reconnect', { retry: 3, delay_ms: 8000 })).not.toThrow();
  });

  it('добавляет span.addEvent если активный OTel-span есть', () => {
    const addEvent = vi.fn();
    vi.mocked(trace.getActiveSpan).mockReturnValue({
      addEvent,
      setAttribute: vi.fn(),
      setAttributes: vi.fn(),
      setStatus: vi.fn(),
      updateName: vi.fn(),
      end: vi.fn(),
      isRecording: () => true,
      recordException: vi.fn(),
      spanContext: () => ({ traceId: '', spanId: '', traceFlags: 0 }),
    } as unknown as ReturnType<typeof trace.getActiveSpan>);

    emitRumEvent('auth.refresh.failed', { reason: 'AUTH_REFRESH_FAILED' });
    expect(addEvent).toHaveBeenCalledWith('auth.refresh.failed', {
      reason: 'AUTH_REFRESH_FAILED',
    });
  });

  it('игнорирует отсутствие активного span (OTel не инициализирован)', () => {
    vi.mocked(trace.getActiveSpan).mockReturnValue(undefined);
    expect(() => emitRumEvent('sse.reconnect', { retry: 1, delay_ms: 2000 })).not.toThrow();
    expect(Sentry.addBreadcrumb).toHaveBeenCalledTimes(1);
  });
});
