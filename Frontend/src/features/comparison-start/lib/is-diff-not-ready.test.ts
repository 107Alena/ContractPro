// Unit-тесты lib/is-diff-not-ready.ts: тип-guard для 404 DIFF_NOT_FOUND.
import { describe, expect, it } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { isDiffNotReadyError } from './is-diff-not-ready';

function orch(code: string, status?: number): OrchestratorError {
  return new OrchestratorError(
    status !== undefined
      ? { error_code: code, message: 'm', status }
      : { error_code: code, message: 'm' },
  );
}

describe('isDiffNotReadyError', () => {
  it('OrchestratorError с error_code=DIFF_NOT_FOUND → true', () => {
    expect(isDiffNotReadyError(orch('DIFF_NOT_FOUND', 404))).toBe(true);
  });

  it('OrchestratorError с любым другим кодом → false', () => {
    expect(isDiffNotReadyError(orch('VERSION_NOT_FOUND', 404))).toBe(false);
    expect(isDiffNotReadyError(orch('VERSION_STILL_PROCESSING', 409))).toBe(false);
    expect(isDiffNotReadyError(orch('INTERNAL_ERROR', 500))).toBe(false);
  });

  it('произвольный Error → false', () => {
    expect(isDiffNotReadyError(new Error('DIFF_NOT_FOUND'))).toBe(false);
  });

  it('не-ошибка → false', () => {
    expect(isDiffNotReadyError(null)).toBe(false);
    expect(isDiffNotReadyError(undefined)).toBe(false);
    expect(isDiffNotReadyError('DIFF_NOT_FOUND')).toBe(false);
    expect(isDiffNotReadyError({ error_code: 'DIFF_NOT_FOUND' })).toBe(false);
  });
});
