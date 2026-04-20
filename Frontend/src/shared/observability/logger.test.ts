// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { sessionStore } from '@/shared/auth/session-store';

// Полный module-mock @sentry/react: `Sentry.addBreadcrumb/captureMessage/
// captureException` в ESM-пакете объявлены non-configurable → vi.spyOn падает
// с "Cannot redefine property". Тот же паттерн используется в sentry.test.ts,
// который не пытается шпионить напрямую (там buildSentryConfig — pure builder).
vi.mock('@sentry/react', () => ({
  addBreadcrumb: vi.fn(),
  captureMessage: vi.fn(() => 'evt-id'),
  captureException: vi.fn(() => 'evt-id'),
}));

const Sentry = await import('@sentry/react');
const { __resetLoggerForTests, logger } = await import('./logger');

// Минимальный валидный UserProfile — поля проверяются в session-store.test.ts.
const user = {
  user_id: 'u-1',
  email: 'a@b.c',
  name: 'Alice',
  role: 'LAWYER' as const,
  organization_id: 'org-1',
  organization_name: 'Acme',
  permissions: { export_enabled: true },
};

describe('logger — dev (non-production) ветка', () => {
  beforeEach(() => {
    vi.stubEnv('MODE', 'development');
    vi.spyOn(console, 'debug').mockImplementation(() => undefined);
    vi.spyOn(console, 'info').mockImplementation(() => undefined);
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    sessionStore.getState().clear();
    vi.mocked(Sentry.addBreadcrumb).mockClear();
    vi.mocked(Sentry.captureMessage).mockClear();
    vi.mocked(Sentry.captureException).mockClear();
    __resetLoggerForTests();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('info → console.info с префиксом [contractpro]', () => {
    logger.info('hello', { foo: 'bar' });
    expect(console.info).toHaveBeenCalledTimes(1);
    const [prefix, message, ctx] = (console.info as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(prefix).toBe('[contractpro]');
    expect(message).toBe('hello');
    expect(ctx).toMatchObject({ foo: 'bar' });
  });

  it('enrichment: user в session → user_id/org_id в контексте', () => {
    sessionStore.getState().setUser(user);
    logger.warn('auth-ok');
    const [, , ctx] = (console.warn as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(ctx).toMatchObject({ user_id: 'u-1', org_id: 'org-1' });
  });

  it('enrichment: без user — поля user_id/org_id отсутствуют', () => {
    logger.warn('guest');
    const [, , ctx] = (console.warn as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(ctx).not.toHaveProperty('user_id');
    expect(ctx).not.toHaveProperty('org_id');
  });

  it('enrichment: route = window.location.pathname', () => {
    logger.info('route-check');
    const [, , ctx] = (console.info as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(ctx).toMatchObject({ route: '/' });
  });

  it('error → console.error включает err', () => {
    const err = new Error('boom');
    logger.error('failed', err, { correlation_id: 'c-1' });
    const [, message, ctx] = (console.error as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(message).toBe('failed');
    expect((ctx as { err: Error }).err).toBe(err);
    expect(ctx).toMatchObject({ correlation_id: 'c-1' });
  });
});

describe('logger — prod ветка', () => {
  beforeEach(() => {
    vi.stubEnv('MODE', 'production');
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    sessionStore.getState().clear();
    vi.mocked(Sentry.addBreadcrumb).mockClear();
    vi.mocked(Sentry.captureMessage).mockClear();
    vi.mocked(Sentry.captureException).mockClear();
    vi.mocked(Sentry.addBreadcrumb).mockImplementation(() => undefined);
    vi.mocked(Sentry.captureMessage).mockImplementation(() => 'evt-id');
    vi.mocked(Sentry.captureException).mockImplementation(() => 'evt-id');
    __resetLoggerForTests();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('debug/info → no-op (не замусоривает Breadcrumbs)', () => {
    logger.debug('d');
    logger.info('i');
    expect(Sentry.addBreadcrumb).not.toHaveBeenCalled();
    expect(Sentry.captureMessage).not.toHaveBeenCalled();
  });

  it('warn → Sentry.addBreadcrumb с level:warning + data', () => {
    sessionStore.getState().setUser(user);
    logger.warn('stale-cache', { cache_key: 'contracts' });
    expect(Sentry.addBreadcrumb).toHaveBeenCalledWith({
      category: 'log',
      level: 'warning',
      message: 'stale-cache',
      data: expect.objectContaining({
        cache_key: 'contracts',
        user_id: 'u-1',
        org_id: 'org-1',
      }),
    });
  });

  it('error → captureException + captureMessage', () => {
    const err = new Error('boom');
    logger.error('upload-failed', err, { correlation_id: 'c-1' });
    expect(Sentry.captureException).toHaveBeenCalledWith(
      err,
      expect.objectContaining({ extra: expect.objectContaining({ message: 'upload-failed' }) }),
    );
    expect(Sentry.captureMessage).toHaveBeenCalledWith(
      'upload-failed',
      expect.objectContaining({ level: 'error' }),
    );
  });

  it('error без err → только captureMessage, без captureException', () => {
    logger.error('no-err-case');
    expect(Sentry.captureException).not.toHaveBeenCalled();
    expect(Sentry.captureMessage).toHaveBeenCalledTimes(1);
  });

  it('recursion guard: вложенный logger.warn внутри Sentry-хука → fallback на console', () => {
    vi.mocked(Sentry.addBreadcrumb).mockImplementation(() => {
      logger.warn('inner');
    });
    logger.warn('outer');
    // Второй (inner) вызов — fallback на console.error, не рекурсирует в Sentry
    expect(console.error).toHaveBeenCalledWith('[contractpro] logger recursion:', 'inner');
  });

  it('ошибка в Sentry.captureMessage не ломает caller', () => {
    vi.mocked(Sentry.captureMessage).mockImplementation(() => {
      throw new Error('sentry-down');
    });
    expect(() => logger.error('x')).not.toThrow();
  });
});
