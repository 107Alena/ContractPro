// Smoke-тесты OrchestratorError: поля, alias code, cause-пропагация, guard.
import { describe, expect, it } from 'vitest';

import { isOrchestratorError, OrchestratorError } from './orchestrator-error';

describe('OrchestratorError', () => {
  it('проставляет все переданные поля', () => {
    const err = new OrchestratorError({
      error_code: 'FILE_TOO_LARGE',
      message: 'Файл больше 20 МБ',
      suggestion: 'Разделите документ',
      details: { max_size_bytes: 20_971_520 } as unknown as OrchestratorError['details'],
      correlationId: 'corr-1',
      status: 413,
    });
    expect(err.error_code).toBe('FILE_TOO_LARGE');
    expect(err.message).toBe('Файл больше 20 МБ');
    expect(err.suggestion).toBe('Разделите документ');
    expect(err.correlationId).toBe('corr-1');
    expect(err.status).toBe(413);
    expect(err.name).toBe('OrchestratorError');
  });

  it('getter code возвращает error_code (§20.4 alias)', () => {
    const err = new OrchestratorError({ error_code: 'VALIDATION_ERROR', message: 'x' });
    expect(err.code).toBe('VALIDATION_ERROR');
    expect(err.code).toBe(err.error_code);
  });

  it('cause пробрасывается в Error constructor (ES2022)', () => {
    const inner = new Error('low-level');
    const err = new OrchestratorError({ error_code: 'INTERNAL_ERROR', message: 'x', cause: inner });
    expect(err.cause).toBe(inner);
  });

  it('опциональные поля undefined если не переданы', () => {
    const err = new OrchestratorError({ error_code: 'INTERNAL_ERROR', message: 'x' });
    expect(err.suggestion).toBeUndefined();
    expect(err.details).toBeUndefined();
    expect(err.correlationId).toBeUndefined();
    expect(err.status).toBeUndefined();
  });
});

describe('isOrchestratorError', () => {
  it('true для instance', () => {
    expect(
      isOrchestratorError(new OrchestratorError({ error_code: 'INTERNAL_ERROR', message: 'x' })),
    ).toBe(true);
  });

  it('false для обычного Error и примитивов', () => {
    expect(isOrchestratorError(new Error('x'))).toBe(false);
    expect(isOrchestratorError({ error_code: 'FILE_TOO_LARGE', message: 'x' })).toBe(false);
    expect(isOrchestratorError(null)).toBe(false);
    expect(isOrchestratorError(undefined)).toBe(false);
  });
});
