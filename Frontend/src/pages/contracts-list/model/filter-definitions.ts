// Декларативный список фильтров для страницы «Документы» (§17.3/§17.4).
//
// Определения — page-scoped: другие потребители `useFilterParams` описывают
// свои фильтры у себя (reports/audit). Когда появится второй потребитель —
// поднимем в entities/contract/model/filters.ts (YAGNI).
//
// OpenAPI-контракт `/contracts` на момент FE-TASK-044 поддерживает только
// `status` (DocumentStatus). Фильтры «тип договора» и «дата» оставлены в UI
// для совместимости с дизайном Figma — значения сохраняются в URL, но НЕ
// уходят в бэкенд до появления серверной поддержки.
// TODO(orchestrator): дополнить `/contracts` поддержкой `type` и `date_from`/
// `date_to` и подключить в use-contracts-list-query.ts.
import type { FilterDefinition } from '@/features/filters';

export const DOCUMENT_STATUS_OPTIONS = [
  { value: 'ACTIVE', label: 'Активные' },
  { value: 'ARCHIVED', label: 'В архиве' },
  { value: 'DELETED', label: 'Удалённые' },
] as const;

// Типы договоров (классификация LIC). Канонический список живёт в backend-
// классификаторе; пока показываем наиболее частые варианты из требований ТЗ.
export const CONTRACT_TYPE_OPTIONS = [
  { value: 'SERVICES', label: 'Услуги' },
  { value: 'SUPPLY', label: 'Поставка' },
  { value: 'RENT', label: 'Аренда' },
  { value: 'NDA', label: 'NDA' },
  { value: 'CONSULTING', label: 'Консалтинг' },
  { value: 'OTHER', label: 'Прочие' },
] as const;

export const DATE_RANGE_OPTIONS = [
  { value: '7d', label: 'За 7 дней' },
  { value: '30d', label: 'За 30 дней' },
  { value: '90d', label: 'За 90 дней' },
  { value: 'all', label: 'Всё время' },
] as const;

/**
 * Определения фильтров для `useFilterParams`.
 * `pinned: true` — чипы рендерятся в основной полосе; `pinned: false` — в
 * модалке «Ещё фильтры» (§8.3).
 */
export const CONTRACTS_FILTER_DEFINITIONS: readonly FilterDefinition[] = [
  {
    key: 'status',
    label: 'Состояние',
    kind: 'single',
    options: DOCUMENT_STATUS_OPTIONS,
    defaultValue: '',
    pinned: true,
  },
  {
    key: 'type',
    label: 'Тип договора',
    kind: 'multi',
    options: CONTRACT_TYPE_OPTIONS,
    defaultValue: [],
    pinned: false,
  },
  {
    key: 'date',
    label: 'Период',
    kind: 'single',
    options: DATE_RANGE_OPTIONS,
    defaultValue: 'all',
    pinned: false,
  },
];
