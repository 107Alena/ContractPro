import { afterEach, describe, expect, it } from 'vitest';

import { getFeatureFlags, getRuntimeEnv } from './runtime-env';

describe('runtime-env', () => {
  const original = typeof window !== 'undefined' ? window.__ENV__ : undefined;

  afterEach(() => {
    if (typeof window === 'undefined') return;
    if (original === undefined) {
      delete window.__ENV__;
    } else {
      window.__ENV__ = original;
    }
  });

  it('возвращает {} при отсутствии window (node env)', () => {
    expect(getRuntimeEnv()).toEqual({});
    expect(getFeatureFlags()).toEqual({});
  });

  it('возвращает {} при отсутствии window.__ENV__', () => {
    if (typeof window === 'undefined') return;
    delete window.__ENV__;
    expect(getRuntimeEnv()).toEqual({});
    expect(getFeatureFlags()).toEqual({});
  });

  it('читает заданный __ENV__ и FEATURES', () => {
    if (typeof window === 'undefined') return;
    window.__ENV__ = {
      API_BASE_URL: '/api/v1',
      FEATURES: { FEATURE_DOCX_UPLOAD: true },
    };
    expect(getRuntimeEnv().API_BASE_URL).toBe('/api/v1');
    expect(getFeatureFlags()).toEqual({ FEATURE_DOCX_UPLOAD: true });
  });
});
