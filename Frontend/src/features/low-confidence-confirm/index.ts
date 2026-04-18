// Public API фичи. App-shell монтирует Provider, страницы могут вызывать
// useConfirmType напрямую для action-bar (не используется в v1, экспортируется
// на случай ContractDetailPage manual-trigger по статусу AWAITING_USER_INPUT).
export { mapConfirmTypeError } from './lib/map-confirm-type-error';
export { useCurrentTypeConfirmation, useLowConfidenceStore } from './model/low-confidence-store';
export type { ConfirmTypeInput, TypeAlternative, TypeConfirmationEvent } from './model/types';
export type { UseConfirmTypeOptions, UseConfirmTypeResult } from './model/use-confirm-type';
export { useConfirmType } from './model/use-confirm-type';
export { useLowConfidenceBridge } from './model/use-low-confidence-bridge';
export type { LowConfidenceConfirmModalProps } from './ui/LowConfidenceConfirmModal';
export { LowConfidenceConfirmModal } from './ui/LowConfidenceConfirmModal';
export { LowConfidenceConfirmProvider } from './ui/LowConfidenceConfirmProvider';
