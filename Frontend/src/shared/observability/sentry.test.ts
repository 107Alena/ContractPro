import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import * as runtimeEnv from '@/shared/config/runtime-env';

import { initSentry } from './sentry';

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
