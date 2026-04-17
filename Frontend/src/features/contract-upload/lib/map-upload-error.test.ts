import { describe, expect, it } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { isFileFieldError, mapUploadFileError } from './map-upload-error';

function orch(code: string, message = 'msg', status = 400): OrchestratorError {
  return new OrchestratorError({ error_code: code, message, status });
}

describe('mapUploadFileError', () => {
  it.each([
    ['FILE_TOO_LARGE', 'Файл больше 20 МБ'],
    ['UNSUPPORTED_FORMAT', 'Только PDF'],
    ['INVALID_FILE', 'Файл повреждён'],
  ])('%s → { field: "file", code, message }', (code, msg) => {
    const mapped = mapUploadFileError(orch(code, msg));
    expect(mapped).toEqual({ field: 'file', code, message: msg });
  });

  it('fallback на error_code, если message пустой', () => {
    const mapped = mapUploadFileError(orch('FILE_TOO_LARGE', '   '));
    expect(mapped?.message).toBe('FILE_TOO_LARGE');
  });

  it('не-whitelist код → null', () => {
    expect(mapUploadFileError(orch('AUTH_TOKEN_EXPIRED'))).toBeNull();
    expect(mapUploadFileError(orch('VALIDATION_ERROR'))).toBeNull();
    expect(mapUploadFileError(orch('INTERNAL_ERROR'))).toBeNull();
  });

  it('не-OrchestratorError → null', () => {
    expect(mapUploadFileError(new Error('x'))).toBeNull();
    expect(mapUploadFileError(null)).toBeNull();
    expect(mapUploadFileError(undefined)).toBeNull();
    expect(mapUploadFileError({ error_code: 'FILE_TOO_LARGE' })).toBeNull();
  });
});

describe('isFileFieldError', () => {
  it('true для whitelist', () => {
    expect(isFileFieldError(orch('FILE_TOO_LARGE'))).toBe(true);
    expect(isFileFieldError(orch('UNSUPPORTED_FORMAT'))).toBe(true);
    expect(isFileFieldError(orch('INVALID_FILE'))).toBe(true);
  });

  it('false для прочих кодов и не-Orchestrator', () => {
    expect(isFileFieldError(orch('VALIDATION_ERROR'))).toBe(false);
    expect(isFileFieldError(orch('NETWORK_ERROR'))).toBe(false);
    expect(isFileFieldError(new Error('x'))).toBe(false);
    expect(isFileFieldError(null)).toBe(false);
  });
});
