// Orchestration-модель статуса для dashboard-группировки (FE-TASK-042).
//
// Labels + tone — источник истины в `shared/lib/status-view` (FE-TASK-024),
// чтобы `entities/version/ui/StatusBadge` и orchestration `viewStatus()` не
// дублировали маппинг. Здесь добавляется только `bucket` — dashboard-специфичная
// группировка статусов (pending/in_progress/ready/failed/awaiting).
//
// Потребители: widgets/dashboard-* (6 виджетов) + pages/dashboard.
import type { UserProcessingStatus } from '@/shared/api';
import { statusMeta, type StatusTone } from '@/shared/lib/status-view';

export type StatusBucket = 'pending' | 'in_progress' | 'ready' | 'failed' | 'awaiting';

export interface StatusView {
  label: string;
  tone: StatusTone;
  bucket: StatusBucket;
}

const BUCKET_MAP: Record<UserProcessingStatus, StatusBucket> = {
  UPLOADED: 'pending',
  QUEUED: 'pending',
  PROCESSING: 'in_progress',
  ANALYZING: 'in_progress',
  AWAITING_USER_INPUT: 'awaiting',
  GENERATING_REPORTS: 'in_progress',
  READY: 'ready',
  PARTIALLY_FAILED: 'failed',
  FAILED: 'failed',
  REJECTED: 'failed',
};

export function viewStatus(status: UserProcessingStatus | undefined): StatusView {
  const { label, tone } = statusMeta(status);
  const bucket: StatusBucket = status ? BUCKET_MAP[status] : 'pending';
  return { label, tone, bucket };
}
