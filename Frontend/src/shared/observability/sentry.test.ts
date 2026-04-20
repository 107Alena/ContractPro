import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import * as runtimeEnv from '@/shared/config/runtime-env';

import { buildSentryConfig, initSentry } from './sentry';

describe('initSentry', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('возвращает { enabled: false } если SENTRY_DSN пуст (no-op для локального dev)', () => {
    vi.spyOn(runtimeEnv, 'getRuntimeEnv').mockReturnValue({});
    const result = initSentry();
    expect(result.enabled).toBe(false);
  });

  it('возвращает { enabled: true } когда SENTRY_DSN задан', () => {
    vi.spyOn(runtimeEnv, 'getRuntimeEnv').mockReturnValue({
      SENTRY_DSN: 'https://public@sentry.example.com/1',
    });
    const result = initSentry();
    expect(result.enabled).toBe(true);
  });

  it('отсутствующий DSN трактуется как no-op', () => {
    vi.spyOn(runtimeEnv, 'getRuntimeEnv').mockReturnValue({ API_BASE_URL: '/api/v1' });
    expect(initSentry().enabled).toBe(false);
  });

  it('пустая строка DSN трактуется как отсутствующая (no-op)', () => {
    vi.spyOn(runtimeEnv, 'getRuntimeEnv').mockReturnValue({ SENTRY_DSN: '' });
    expect(initSentry().enabled).toBe(false);
  });
});

describe('buildSentryConfig', () => {
  it('без DSN возвращает null (пропускаем init)', () => {
    expect(buildSentryConfig({}, 'abc123', 'development')).toBeNull();
    expect(buildSentryConfig({ SENTRY_DSN: '' }, 'abc123', 'development')).toBeNull();
  });

  it('с DSN возвращает полный конфиг: sample rates, integrations, scrubber', () => {
    const config = buildSentryConfig(
      { SENTRY_DSN: 'https://public@sentry.example.com/1' },
      'abc1234',
      'production',
    );
    expect(config).not.toBeNull();
    expect(config?.dsn).toBe('https://public@sentry.example.com/1');
    expect(config?.environment).toBe('production');
    expect(config?.release).toBe('contractpro-frontend@abc1234');
    // tracing покрывается OpenTelemetry (§14.3, FE-TASK-051) — Sentry Performance выключен
    expect(config?.tracesSampleRate).toBe(0);
    expect(config?.replaysSessionSampleRate).toBe(0.1);
    expect(config?.replaysOnErrorSampleRate).toBe(1.0);
    expect(config?.sendDefaultPii).toBe(false);
    expect(Array.isArray(config?.integrations)).toBe(true);
    expect(config?.integrations?.length).toBeGreaterThan(0);
    expect(typeof config?.beforeSend).toBe('function');
  });

  it('Session Replay integration зарегистрирована с privacy-настройками', () => {
    const config = buildSentryConfig(
      { SENTRY_DSN: 'https://public@sentry.example.com/1' },
      '',
      'production',
    );
    const integrations = config?.integrations as Array<{ name: string }> | undefined;
    expect(integrations).toBeDefined();
    const replay = integrations?.find((i) => i.name === 'Replay');
    expect(replay).toBeDefined();
  });

  it('beforeSend возвращает null если scrubber падает (не утекает сырое событие)', () => {
    const config = buildSentryConfig(
      { SENTRY_DSN: 'https://public@sentry.example.com/1' },
      '',
      'production',
    );
    const beforeSend = config?.beforeSend;
    // Передаём объект с getter'ом, бросающим Error, чтобы scrubber упал
    const bad: Record<string, unknown> = { type: undefined };
    Object.defineProperty(bad, 'request', {
      get() {
        throw new Error('boom');
      },
      enumerable: true,
    });
    const result = beforeSend!(bad as unknown as Parameters<NonNullable<typeof beforeSend>>[0], {});
    expect(result).toBeNull();
  });

  it('runtime SENTRY_ENVIRONMENT переопределяет mode', () => {
    const config = buildSentryConfig(
      { SENTRY_DSN: 'https://public@sentry.example.com/1', SENTRY_ENVIRONMENT: 'staging' },
      'sha',
      'production',
    );
    expect(config?.environment).toBe('staging');
  });

  it('пустой gitSha — release не выставляется (fallback на дефолт Sentry)', () => {
    const config = buildSentryConfig(
      { SENTRY_DSN: 'https://public@sentry.example.com/1' },
      '',
      'development',
    );
    expect(config?.release).toBeUndefined();
  });

  it('beforeSend прогоняет event через scrubber (Authorization не утекает)', () => {
    const config = buildSentryConfig(
      { SENTRY_DSN: 'https://public@sentry.example.com/1' },
      '',
      'production',
    );
    const beforeSend = config?.beforeSend;
    expect(beforeSend).toBeDefined();
    const result = beforeSend!(
      {
        type: undefined,
        request: { headers: { Authorization: 'Bearer token.here.value' } },
      } as Parameters<NonNullable<typeof beforeSend>>[0],
      {},
    );
    expect(result).not.toBeNull();
    const headers = (result as { request: { headers: Record<string, string> } }).request.headers;
    expect(headers.Authorization).toBe('[Filtered]');
  });
});
