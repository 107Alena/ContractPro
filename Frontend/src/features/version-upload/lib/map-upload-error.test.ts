import { describe, expect, it } from 'vitest';

import { OrchestratorError } from '@/shared/api/errors';

import {
  isUploadVersionFileFieldError,
  mapUploadVersionError,
} from './map-upload-error';

function orch(code: string, message = 'msg', status?: number): OrchestratorError {
  return new OrchestratorError(
    status !== undefined
      ? { error_code: code, message, status }
      : { error_code: code, message },
  );
}

describe('mapUploadVersionError', () => {
  it.each([
    ['FILE_TOO_LARGE', 'Файл больше 20 МБ'],
    ['UNSUPPORTED_FORMAT', 'Поддерживается только PDF'],
    ['INVALID_FILE', 'Файл повреждён'],
  ])('%s → field=file, message из err', (code, msg) => {
    const mapped = mapUploadVersionError(orch(code, msg));
    expect(mapped).toEqual({ field: 'file', code, message: msg });
  });

  it('пустой message → fallback на error_code', () => {
    const mapped = mapUploadVersionError(orch('INVALID_FILE', '   '));
    expect(mapped).toEqual({ field: 'file', code: 'INVALID_FILE', message: 'INVALID_FILE' });
  });

  it.each([
    'VALIDATION_ERROR',
    'DOCUMENT_NOT_FOUND',
    'VERSION_STILL_PROCESSING',
    'INTERNAL_ERROR',
    'NETWORK_ERROR',
  ])('не-file код %s → null', (code) => {
    expect(mapUploadVersionError(orch(code))).toBeNull();
  });

  it('не-OrchestratorError → null', () => {
    expect(mapUploadVersionError(new Error('boom'))).toBeNull();
    expect(mapUploadVersionError(null)).toBeNull();
    expect(mapUploadVersionError({ error_code: 'FILE_TOO_LARGE' })).toBeNull();
  });
});

describe('isUploadVersionFileFieldError', () => {
  it('true для file-кодов', () => {
    expect(isUploadVersionFileFieldError(orch('FILE_TOO_LARGE'))).toBe(true);
    expect(isUploadVersionFileFieldError(orch('UNSUPPORTED_FORMAT'))).toBe(true);
    expect(isUploadVersionFileFieldError(orch('INVALID_FILE'))).toBe(true);
  });

  it('false для прочих', () => {
    expect(isUploadVersionFileFieldError(orch('VALIDATION_ERROR'))).toBe(false);
    expect(isUploadVersionFileFieldError(new Error('boom'))).toBe(false);
  });
});
