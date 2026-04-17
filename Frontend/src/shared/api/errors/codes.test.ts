// Тест инвариантов реестра кодов ошибок: полнота, уникальность, соответствие §7.3.
import { describe, expect, it } from 'vitest';

import { ERROR_UX } from './catalog';
import { CLIENT_ERROR_CODES, type ErrorCode, isKnownErrorCode, SERVER_ERROR_CODES } from './codes';

describe('SERVER_ERROR_CODES', () => {
  it('содержит ровно 22 кода (§7.3)', () => {
    expect(SERVER_ERROR_CODES).toHaveLength(22);
  });

  it('все коды уникальны', () => {
    expect(new Set(SERVER_ERROR_CODES).size).toBe(SERVER_ERROR_CODES.length);
  });

  it.each([
    'AUTH_TOKEN_MISSING',
    'AUTH_TOKEN_EXPIRED',
    'VALIDATION_ERROR',
    'RATE_LIMIT_EXCEEDED',
    'INTERNAL_ERROR',
  ] as const)('включает %s', (code) => {
    expect(SERVER_ERROR_CODES).toContain(code);
  });
});

describe('CLIENT_ERROR_CODES', () => {
  it('содержит 4 sentinel-кода (§7.2)', () => {
    expect(Object.keys(CLIENT_ERROR_CODES)).toHaveLength(4);
  });

  it('keys совпадают с values (self-referential enum)', () => {
    for (const [key, value] of Object.entries(CLIENT_ERROR_CODES)) {
      expect(key).toBe(value);
    }
  });
});

describe('ERROR_UX catalog completeness', () => {
  it('покрывает все 26 кодов (22 серверных + 4 клиентских)', () => {
    const expected = [...SERVER_ERROR_CODES, ...(Object.values(CLIENT_ERROR_CODES) as ErrorCode[])];
    for (const code of expected) {
      expect(ERROR_UX[code], `ERROR_UX missing: ${code}`).toBeDefined();
      expect(ERROR_UX[code].title, `title empty for ${code}`).not.toBe('');
    }
  });

  it('все action-значения в whitelist: retry | login | none | undefined', () => {
    const allowed = new Set(['retry', 'login', 'none', undefined]);
    for (const entry of Object.values(ERROR_UX)) {
      expect(allowed.has(entry.action)).toBe(true);
    }
  });
});

describe('isKnownErrorCode', () => {
  it('true для всех 26 известных кодов', () => {
    for (const code of [
      ...SERVER_ERROR_CODES,
      ...(Object.values(CLIENT_ERROR_CODES) as ErrorCode[]),
    ]) {
      expect(isKnownErrorCode(code)).toBe(true);
    }
  });

  it('false для произвольной строки', () => {
    expect(isKnownErrorCode('SOMETHING_NEW')).toBe(false);
    expect(isKnownErrorCode('')).toBe(false);
    expect(isKnownErrorCode('validation_error')).toBe(false); // case-sensitive
  });
});
