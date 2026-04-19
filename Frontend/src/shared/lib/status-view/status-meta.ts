// Источник истины для отображения `UserProcessingStatus` (10 значений из
// ApiBackendOrchestrator/architecture/api-specification.yaml → §5.2
// high-architecture.md).
//
// Ссылки на high-architecture.md (Frontend/architecture/):
// - §8.3 — RiskBadge/StatusBadge спецификация.
// - §17.4 — widgets, которые мапят processing_status в UI.
//
// Лежит в shared/lib, потому что UserProcessingStatus — OpenAPI-тип (shared/api),
// а {label, tone} — stateless reference data, не доменная логика. Потребители:
// - entities/version/ui/status-badge — display primitive (FE-TASK-024).
// - entities/contract/model/status-view — orchestration (добавляет bucket для
//   dashboard-группировки, FE-TASK-042).
// Размещение в shared устраняет cross-entity импорт, запрещённый
// eslint-plugin-boundaries (Frontend/eslint.config.js:136-137).
import type { UserProcessingStatus } from '@/shared/api';
import type { BadgeProps } from '@/shared/ui';

export type StatusTone = NonNullable<BadgeProps['variant']>;

export interface StatusMeta {
  /** Пользовательский лейбл (ru, NFR-5.2). */
  label: string;
  /** Визуальный tone для shared/ui Badge (success/warning/danger/neutral/brand). */
  tone: StatusTone;
}

export const STATUS_META: Record<UserProcessingStatus, StatusMeta> = {
  UPLOADED: { label: 'Загружен', tone: 'neutral' },
  QUEUED: { label: 'В очереди', tone: 'neutral' },
  PROCESSING: { label: 'Извлечение текста', tone: 'brand' },
  ANALYZING: { label: 'Юр. анализ', tone: 'brand' },
  AWAITING_USER_INPUT: { label: 'Требует подтверждения', tone: 'warning' },
  GENERATING_REPORTS: { label: 'Формирование отчётов', tone: 'brand' },
  READY: { label: 'Готово', tone: 'success' },
  PARTIALLY_FAILED: { label: 'Частично готово', tone: 'warning' },
  FAILED: { label: 'Ошибка', tone: 'danger' },
  REJECTED: { label: 'Отклонён', tone: 'danger' },
};

export const UNKNOWN_STATUS_META: StatusMeta = { label: 'Неизвестно', tone: 'neutral' };

export function statusMeta(status: UserProcessingStatus | undefined | null): StatusMeta {
  if (!status) return UNKNOWN_STATUS_META;
  return STATUS_META[status] ?? UNKNOWN_STATUS_META;
}
