// Barrel: публичный API фичи contract-upload (§6.1 / §16.2 high-architecture).
//
// Импортировать ТОЛЬКО этот путь (FSD-граница: `features/*` не раскрывает
// внутренние слои наружу). Обе страницы-потребителя — NewCheckPage
// (FE-TASK-042) и VersionUploadDialog (через feature/version-upload, FE-TASK-039) —
// используют `useUploadContract`.
export type { UploadContractOptions } from './api/upload-contract';
export { UPLOAD_CONTRACT_ENDPOINT,uploadContract } from './api/upload-contract';
export { isFileFieldError, mapUploadFileError, type UploadFieldError } from './lib/map-upload-error';
export type {
  UploadContractInput,
  UploadContractResponse,
  UploadFormValues,
  UploadProgress,
  UserProcessingStatus,
} from './model/types';
export { UPLOAD_FORM_FIELDS } from './model/types';
export type {
  UseUploadContractOptions,
  UseUploadContractResult,
} from './model/use-upload-contract';
export { useUploadContract } from './model/use-upload-contract';
