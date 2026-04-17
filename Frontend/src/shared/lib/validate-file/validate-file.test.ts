// @vitest-environment jsdom
// jsdom: для File.slice().arrayBuffer() — node env не включает File API.
import { describe, expect, it } from 'vitest';

import { MAX_FILE_SIZE } from '@/shared/config/file-formats';

import { FileValidationError, getFileValidationMessage, validateFile } from './validate-file';

function makeFile(bytes: number[], type: string, name = 'doc'): File {
  return new File([new Uint8Array(bytes)], name, { type });
}

const PDF_HEADER = [0x25, 0x50, 0x44, 0x46]; // %PDF
const DOCX_HEADER = [0x50, 0x4b, 0x03, 0x04]; // PK\x03\x04
const RANDOM_HEADER = [0xde, 0xad, 0xbe, 0xef];

describe('validateFile — happy path', () => {
  it('пропускает PDF с корректным magic-bytes', async () => {
    const file = makeFile([...PDF_HEADER, 0, 0, 0, 0, 0, 0, 0, 0], 'application/pdf', 'a.pdf');
    await expect(validateFile(file)).resolves.toBeUndefined();
  });

  it('пропускает DOCX при FEATURE_DOCX_UPLOAD=true', async () => {
    const file = makeFile(
      [...DOCX_HEADER, 0, 0, 0, 0, 0, 0, 0, 0],
      'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      'a.docx',
    );
    await expect(
      validateFile(file, { flags: { FEATURE_DOCX_UPLOAD: true } }),
    ).resolves.toBeUndefined();
  });
});

describe('validateFile — ошибки', () => {
  it('EMPTY_FILE для нулевого размера', async () => {
    const file = new File([], 'empty.pdf', { type: 'application/pdf' });
    await expect(validateFile(file)).rejects.toMatchObject({
      name: 'FileValidationError',
      code: 'EMPTY_FILE',
    });
  });

  it('FILE_TOO_LARGE при превышении дефолтного лимита', async () => {
    const big = makeFile([...PDF_HEADER, 0, 0, 0, 0, 0, 0, 0, 0], 'application/pdf', 'big.pdf');
    Object.defineProperty(big, 'size', { value: MAX_FILE_SIZE + 1 });
    await expect(validateFile(big)).rejects.toMatchObject({
      code: 'FILE_TOO_LARGE',
      details: { maxSize: MAX_FILE_SIZE, size: MAX_FILE_SIZE + 1 },
    });
  });

  it('FILE_TOO_LARGE при кастомном maxSize', async () => {
    const file = makeFile([...PDF_HEADER, 0, 0, 0, 0, 0, 0, 0, 0], 'application/pdf', 'a.pdf');
    Object.defineProperty(file, 'size', { value: 11 });
    await expect(validateFile(file, { maxSize: 10 })).rejects.toMatchObject({
      code: 'FILE_TOO_LARGE',
      details: { maxSize: 10 },
    });
  });

  it('UNSUPPORTED_FORMAT для DOCX без флага', async () => {
    const file = makeFile(
      [...DOCX_HEADER, 0, 0, 0, 0, 0, 0, 0, 0],
      'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      'a.docx',
    );
    await expect(validateFile(file, { flags: {} })).rejects.toMatchObject({
      code: 'UNSUPPORTED_FORMAT',
      details: { allowed: ['PDF'] },
    });
  });

  it('INVALID_FILE при подмене расширения (MIME=pdf, magic-bytes другие)', async () => {
    const file = makeFile(
      [...RANDOM_HEADER, 0, 0, 0, 0, 0, 0, 0, 0],
      'application/pdf',
      'fake.pdf',
    );
    await expect(validateFile(file)).rejects.toMatchObject({
      code: 'INVALID_FILE',
    });
  });
});

describe('getFileValidationMessage', () => {
  it('возвращает сообщение для каждого кода (RU, NFR-5.2)', () => {
    expect(getFileValidationMessage(new FileValidationError('EMPTY_FILE'))).toMatch(/Файл пустой/);
    expect(
      getFileValidationMessage(
        new FileValidationError('FILE_TOO_LARGE', { maxSize: 20 * 1024 * 1024 }),
      ),
    ).toMatch(/больше 20 МБ/);
    expect(
      getFileValidationMessage(
        new FileValidationError('UNSUPPORTED_FORMAT', { allowed: ['PDF', 'DOCX'] }),
      ),
    ).toMatch(/Поддерживается только PDF, DOCX/);
    expect(getFileValidationMessage(new FileValidationError('INVALID_FILE'))).toMatch(
      /не соответствует/,
    );
  });
});
