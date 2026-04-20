// Фильтры страницы «Отчёты» (FE-TASK-048, §17.3 + §17.4).
//
// Фильтр `status` (DocumentStatus) уходит на сервер (GET /contracts ?status=...).
// Фильтр `period` — клиентский, применяется поверх текущей страницы (7/30/90 дней
// по updated_at). Серверной поддержки периода пока нет (см. TODO в
// contracts-list/filter-definitions.ts).
//
// Фильтр `processing_status` для отчётов ЖЁСТКО прибит в use-reports-list-query.ts:
// показываем только READY + PARTIALLY_FAILED — то есть контракты, по которым
// отчёт уже сформирован (полностью или с предупреждениями).
import type { FilterDefinition } from '@/features/filters';

export const REPORT_STATE_OPTIONS = [
  { value: 'READY', label: 'Готовые' },
  { value: 'PARTIALLY_FAILED', label: 'С предупреждениями' },
] as const;

export const REPORT_PERIOD_OPTIONS = [
  { value: '7d', label: 'За 7 дней' },
  { value: '30d', label: 'За 30 дней' },
  { value: '90d', label: 'За 90 дней' },
  { value: 'all', label: 'Всё время' },
] as const;

export const REPORTS_FILTER_DEFINITIONS: readonly FilterDefinition[] = [
  {
    key: 'state',
    label: 'Состояние',
    kind: 'single',
    options: REPORT_STATE_OPTIONS,
    defaultValue: 'READY',
    pinned: true,
  },
  {
    key: 'period',
    label: 'Период',
    kind: 'single',
    options: REPORT_PERIOD_OPTIONS,
    defaultValue: 'all',
    pinned: true,
  },
];

export type ReportStateFilter = (typeof REPORT_STATE_OPTIONS)[number]['value'];
export type ReportPeriodFilter = (typeof REPORT_PERIOD_OPTIONS)[number]['value'];
