// Source of truth для разрешённых форматов загрузки на frontend (§7.5).
// Хардкод 'application/pdf' — только в этой таблице. Добавление нового формата =
// одна строка ниже + при необходимости feature-flag.
//
// Синхронизируется вручную с backend `ORCH_UPLOAD_ALLOWED_MIME_TYPES`
// (security.md §6 + configuration.md). При расхождении: frontend отвергает
// раньше, backend всё равно перепроверит. До endpoint /config (§18 п.6) —
// контроль через PR-чек-лист.
import { type FeatureFlag, type FeatureFlags, getFeatureFlags } from './runtime-env';

export type FileFormatId = 'pdf' | 'docx' | 'doc';

export interface FileFormat {
  readonly id: FileFormatId;
  readonly mime: string;
  readonly extensions: readonly string[];
  readonly magicBytes: readonly (readonly number[])[];
  readonly label: string;
  readonly featureFlag?: FeatureFlag;
}

export const MAX_FILE_SIZE = 20 * 1024 * 1024;

export const FILE_FORMATS: readonly FileFormat[] = [
  {
    id: 'pdf',
    mime: 'application/pdf',
    extensions: ['.pdf'],
    magicBytes: [[0x25, 0x50, 0x44, 0x46]], // %PDF
    label: 'PDF',
  },
  {
    id: 'docx',
    mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    extensions: ['.docx'],
    // ZIP-сигнатуры: PK\x03\x04 (LFH) и PK\x05\x06 (EOCD для пустых архивов).
    magicBytes: [
      [0x50, 0x4b, 0x03, 0x04],
      [0x50, 0x4b, 0x05, 0x06],
    ],
    label: 'DOCX',
    featureFlag: 'FEATURE_DOCX_UPLOAD',
  },
  {
    id: 'doc',
    mime: 'application/msword',
    extensions: ['.doc'],
    // OLE Compound Document
    magicBytes: [[0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1]],
    label: 'DOC',
    featureFlag: 'FEATURE_DOCX_UPLOAD',
  },
];

export function getActiveFormats(flags: FeatureFlags = getFeatureFlags()): FileFormat[] {
  return FILE_FORMATS.filter((f) => !f.featureFlag || flags[f.featureFlag] === true);
}

// Готовый объект для react-dropzone `accept`-атрибута: { mime: ['.ext1', '.ext2'] }.
export function getDropzoneAccept(formats: readonly FileFormat[]): Record<string, string[]> {
  return Object.fromEntries(formats.map((f) => [f.mime, [...f.extensions]]));
}
