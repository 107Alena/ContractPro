import { describe, expect, it } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { mapConfirmTypeError } from './map-confirm-type-error';

function err(code: string, message = 'msg', status = 400): OrchestratorError {
  return new OrchestratorError({ error_code: code, message, status });
}

describe('mapConfirmTypeError', () => {
  it('VERSION_NOT_AWAITING_INPUT (409) → kind=stale (закрыть модалку + warning toast)', () => {
    const action = mapConfirmTypeError(err('VERSION_NOT_AWAITING_INPUT', 'too late', 409));
    expect(action).toEqual({ kind: 'stale' });
  });

  it('VALIDATION_ERROR (400) → kind=invalid-type с текстом ошибки', () => {
    const action = mapConfirmTypeError(err('VALIDATION_ERROR', 'Не из whitelist', 400));
    expect(action).toEqual({ kind: 'invalid-type', message: 'Не из whitelist' });
  });

  it('VALIDATION_ERROR без message → fallback "Недопустимый тип договора."', () => {
    const action = mapConfirmTypeError(err('VALIDATION_ERROR', '', 400));
    expect(action).toEqual({ kind: 'invalid-type', message: 'Недопустимый тип договора.' });
  });

  it('REQUEST_ABORTED → kind=aborted (без UX)', () => {
    expect(mapConfirmTypeError(err('REQUEST_ABORTED', 'cancelled'))).toEqual({ kind: 'aborted' });
  });

  it.each([
    ['INTERNAL_ERROR', 500],
    ['AUTH_TOKEN_EXPIRED', 401],
    ['PERMISSION_DENIED', 403],
    ['NOT_FOUND', 404],
  ])('%s (HTTP %d) → kind=unknown (page-level toast.error fallback)', (code, status) => {
    expect(mapConfirmTypeError(err(code, 'msg', status))).toEqual({ kind: 'unknown' });
  });
});
