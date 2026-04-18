// Доменные типы фичи. Re-export `TypeConfirmationEvent` из shared/api/sse-events
// упрощает импорты потребителей: они работают только с public API фичи.
export type { TypeAlternative, TypeConfirmationEvent } from '@/shared/api';

export interface ConfirmTypeInput {
  contractId: string;
  versionId: string;
  /** Whitelist валидируется LIC; передача незнакомого значения → 400. */
  contractType: string;
  signal?: AbortSignal;
}
