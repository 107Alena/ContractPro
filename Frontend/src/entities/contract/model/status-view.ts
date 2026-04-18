// Маппинг UserProcessingStatus → user-label + badge-tone + grouping-bucket.
//
// Доменный справочник на уровне entities/contract — используется на dashboard
// (FE-TASK-042) и будет переиспользован в ContractsListPage/ContractDetailPage.
// StatusBadge (FE-TASK-024) оборачивает эту модель в готовый shared-примитив —
// при появлении FE-TASK-024 потребители переключатся на <StatusBadge/>, модель
// останется как источник истины (лейблы и tone не дублируются).
//
// Источник лейблов: ApiBackendOrchestrator/architecture/high-architecture.md §5.2
// (синхронизирован с widgets/processing-progress/step-model.ts).
import type { UserProcessingStatus } from '@/shared/api';
import type { BadgeProps } from '@/shared/ui';

export type StatusBucket = 'pending' | 'in_progress' | 'ready' | 'failed' | 'awaiting';

export interface StatusView {
  label: string;
  tone: NonNullable<BadgeProps['variant']>;
  bucket: StatusBucket;
}

const MAPPING: Record<UserProcessingStatus, StatusView> = {
  UPLOADED: { label: 'Загружен', tone: 'neutral', bucket: 'pending' },
  QUEUED: { label: 'В очереди', tone: 'neutral', bucket: 'pending' },
  PROCESSING: { label: 'Извлечение текста', tone: 'brand', bucket: 'in_progress' },
  ANALYZING: { label: 'Юр. анализ', tone: 'brand', bucket: 'in_progress' },
  AWAITING_USER_INPUT: { label: 'Требует подтверждения', tone: 'warning', bucket: 'awaiting' },
  GENERATING_REPORTS: { label: 'Формирование отчётов', tone: 'brand', bucket: 'in_progress' },
  READY: { label: 'Готово', tone: 'success', bucket: 'ready' },
  PARTIALLY_FAILED: { label: 'Частично готово', tone: 'warning', bucket: 'failed' },
  FAILED: { label: 'Ошибка', tone: 'danger', bucket: 'failed' },
  REJECTED: { label: 'Отклонён', tone: 'danger', bucket: 'failed' },
};

export function viewStatus(status: UserProcessingStatus | undefined): StatusView {
  if (!status) return { label: 'Неизвестно', tone: 'neutral', bucket: 'pending' };
  return MAPPING[status];
}
