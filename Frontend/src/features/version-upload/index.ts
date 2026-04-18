// Barrel: публичный API feature version-upload (§6.1, §16.2 high-architecture).
//
// Импортировать ТОЛЬКО этот путь (FSD-граница). Потребители — ContractDetailPage
// (FE-TASK-045) и VersionUploadDialog в widgets.
export type { UploadVersionOptions } from './api/upload-version';
export { uploadVersion, uploadVersionEndpoint } from './api/upload-version';
export {
  isUploadVersionFileFieldError,
  mapUploadVersionError,
  type UploadVersionFieldError,
} from './lib/map-upload-error';
export type {
  UploadVersionFormValues,
  UploadVersionInput,
  UploadVersionProgress,
  UploadVersionResponse,
  UserProcessingStatus,
} from './model/types';
export { UPLOAD_VERSION_FORM_FIELDS } from './model/types';
export type {
  UseUploadVersionOptions,
  UseUploadVersionResult,
} from './model/use-upload-version';
export { useUploadVersion } from './model/use-upload-version';
