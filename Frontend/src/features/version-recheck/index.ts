// Barrel: публичный API feature version-recheck (§6.1, §16.2 high-architecture).
//
// Импортировать ТОЛЬКО этот путь (FSD-граница). Потребители — ContractDetailPage
// (FE-TASK-045) и ResultPage (FE-TASK-046, кнопка "Проверить заново" в FAILED).
export type { RecheckVersionOptions } from './api/recheck-version';
export { recheckVersion, recheckVersionEndpoint } from './api/recheck-version';
export type {
  RecheckVersionInput,
  RecheckVersionResponse,
  UserProcessingStatus,
} from './model/types';
export type {
  UseRecheckVersionOptions,
  UseRecheckVersionResult,
} from './model/use-recheck-version';
export { useRecheckVersion } from './model/use-recheck-version';
