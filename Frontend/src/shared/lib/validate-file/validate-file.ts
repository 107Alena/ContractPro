// Валидация файла до отправки на backend (§7.5).
// Магические байты отвергают подмену расширения; сравнение MIME — по таблице
// активных форматов (с учётом FEATURE_DOCX_UPLOAD).
//
// ⚠️ Best-effort на frontend. По первым 16 байтам отличаем PDF от других
// форматов и любого ZIP (DOCX). DOCX — это OOXML-контейнер ZIP с теми же
// PK\x03\x04 в шапке, что и .jar/.apk/.xlsx/.zip — глубокая проверка
// (Content_Types.xml + word/document.xml в central directory) не делается.
// Бэкенд (DocumentProcessing) обязан перепроверить (security.md §6).
import { type FileFormat, getActiveFormats, MAX_FILE_SIZE } from '@/shared/config/file-formats';
import { type FeatureFlags } from '@/shared/config/runtime-env';

export type FileValidationCode =
  | 'EMPTY_FILE'
  | 'FILE_TOO_LARGE'
  | 'UNSUPPORTED_FORMAT'
  | 'INVALID_FILE';

export interface FileValidationDetails {
  /** Список меток разрешённых форматов (для UNSUPPORTED_FORMAT). */
  allowed?: string[];
  /** Лимит в байтах (для FILE_TOO_LARGE). */
  maxSize?: number;
  /** Размер переданного файла. */
  size?: number;
}

export class FileValidationError extends Error {
  readonly code: FileValidationCode;
  readonly details: FileValidationDetails;

  constructor(code: FileValidationCode, details: FileValidationDetails = {}) {
    super(code);
    this.name = 'FileValidationError';
    this.code = code;
    this.details = details;
  }
}

const MAGIC_BYTES_HEAD_BYTES = 16;

// Читаем первые `bytes` байт файла. FileReader используется как самый
// портативный путь: Blob.arrayBuffer() появился поздно в Safari (14.1),
// а в jsdom 24 не реализован для Blob, полученного из File.slice().
function readHead(file: File, bytes: number): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result;
      resolve(result instanceof ArrayBuffer ? new Uint8Array(result) : new Uint8Array(0));
    };
    reader.onerror = () =>
      reject(reader.error ?? new Error('FileReader: не удалось прочитать файл'));
    reader.readAsArrayBuffer(file.slice(0, bytes));
  });
}

export interface ValidateFileOptions {
  maxSize?: number;
  flags?: FeatureFlags;
  formats?: readonly FileFormat[];
}

export async function validateFile(file: File, options: ValidateFileOptions = {}): Promise<void> {
  const maxSize = options.maxSize ?? MAX_FILE_SIZE;
  const formats = options.formats ?? getActiveFormats(options.flags);

  if (file.size === 0) {
    throw new FileValidationError('EMPTY_FILE', { size: 0 });
  }
  if (file.size > maxSize) {
    throw new FileValidationError('FILE_TOO_LARGE', { maxSize, size: file.size });
  }

  const byMime = formats.find((f) => f.mime === file.type);
  if (!byMime) {
    throw new FileValidationError('UNSUPPORTED_FORMAT', {
      allowed: formats.map((f) => f.label),
    });
  }

  const head = await readHead(file, MAGIC_BYTES_HEAD_BYTES);
  const matched = byMime.magicBytes.some(
    (sig) => head.length >= sig.length && sig.every((b, i) => head[i] === b),
  );
  if (!matched) {
    // Несоответствие magic-bytes — подмена расширения.
    throw new FileValidationError('INVALID_FILE', {
      allowed: formats.map((f) => f.label),
    });
  }
}

// Человеко-читаемые сообщения об ошибках валидации (NFR-5.2 — RU).
export function getFileValidationMessage(err: FileValidationError): string {
  switch (err.code) {
    case 'EMPTY_FILE':
      return 'Файл пустой. Выберите другой документ.';
    case 'FILE_TOO_LARGE': {
      const limitMb = Math.round((err.details.maxSize ?? MAX_FILE_SIZE) / 1024 / 1024);
      return `Файл больше ${limitMb} МБ. Загрузите документ меньшего размера.`;
    }
    case 'UNSUPPORTED_FORMAT': {
      const allowed = err.details.allowed?.join(', ') ?? 'PDF';
      return `Поддерживается только ${allowed}.`;
    }
    case 'INVALID_FILE':
      return 'Не удалось распознать файл. Возможно, расширение не соответствует содержимому.';
  }
}
