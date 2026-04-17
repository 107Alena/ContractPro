// Barrel: публичный API каталога ошибок (§7.2-§7.3, §20.4, §20.4a).
export type {
  ApplyValidationResult,
  FieldValuesLike,
  TranslateFn,
  UseFormSetErrorLike,
  ValidationErrorDetails,
  ValidationFieldError,
} from './apply-validation';
export { applyValidationErrors, isValidationError } from './apply-validation';
export { ERROR_UX, type ErrorUXEntry } from './catalog';
export {
  CLIENT_ERROR_CODES,
  type ClientErrorCode,
  type ErrorAction,
  type ErrorCode,
  isKnownErrorCode,
  SERVER_ERROR_CODES,
  type ServerErrorCode,
} from './codes';
export { toUserMessage, type UserMessage } from './handler';
export {
  type ErrorDetails,
  type ErrorResponse,
  isOrchestratorError,
  OrchestratorError,
  type OrchestratorErrorOptions,
} from './orchestrator-error';
