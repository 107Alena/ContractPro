import { describe, expect, it } from 'vitest';

import { FILE_FORMATS, getActiveFormats, getDropzoneAccept, MAX_FILE_SIZE } from './file-formats';

describe('FILE_FORMATS', () => {
  it('содержит pdf без feature-flag, docx и doc — за FEATURE_DOCX_UPLOAD', () => {
    const ids = FILE_FORMATS.map((f) => f.id);
    expect(ids).toEqual(['pdf', 'docx', 'doc']);
    expect(FILE_FORMATS.find((f) => f.id === 'pdf')?.featureFlag).toBeUndefined();
    expect(FILE_FORMATS.find((f) => f.id === 'docx')?.featureFlag).toBe('FEATURE_DOCX_UPLOAD');
    expect(FILE_FORMATS.find((f) => f.id === 'doc')?.featureFlag).toBe('FEATURE_DOCX_UPLOAD');
  });

  it('PDF magicBytes = %PDF', () => {
    const pdf = FILE_FORMATS.find((f) => f.id === 'pdf');
    expect(pdf?.magicBytes[0]).toEqual([0x25, 0x50, 0x44, 0x46]);
  });

  it('MAX_FILE_SIZE = 20 МБ', () => {
    expect(MAX_FILE_SIZE).toBe(20 * 1024 * 1024);
  });
});

describe('getActiveFormats', () => {
  it('без флагов — только pdf (v1 default)', () => {
    expect(getActiveFormats({}).map((f) => f.id)).toEqual(['pdf']);
  });

  it('с FEATURE_DOCX_UPLOAD=true — pdf + docx + doc', () => {
    expect(getActiveFormats({ FEATURE_DOCX_UPLOAD: true }).map((f) => f.id)).toEqual([
      'pdf',
      'docx',
      'doc',
    ]);
  });

  it('с FEATURE_DOCX_UPLOAD=false — только pdf', () => {
    expect(getActiveFormats({ FEATURE_DOCX_UPLOAD: false }).map((f) => f.id)).toEqual(['pdf']);
  });
});

describe('getDropzoneAccept', () => {
  it('преобразует в react-dropzone accept-формат', () => {
    const accept = getDropzoneAccept(getActiveFormats({}));
    expect(accept).toEqual({ 'application/pdf': ['.pdf'] });
  });

  it('включает docx/doc при включённом флаге', () => {
    const accept = getDropzoneAccept(getActiveFormats({ FEATURE_DOCX_UPLOAD: true }));
    expect(Object.keys(accept).sort()).toEqual(
      [
        'application/msword',
        'application/pdf',
        'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      ].sort(),
    );
  });
});
