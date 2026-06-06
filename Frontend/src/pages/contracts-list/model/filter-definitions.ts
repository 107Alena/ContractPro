// Декларативный список фильтров для страницы «Документы» (§17.3/§17.4, Figma 200:2).
//
// Определения — page-scoped: другие потребители `useFilterParams` описывают
// свои фильтры у себя (reports/audit). Когда появится второй потребитель —
// поднимем в entities/contract/model/filters.ts (YAGNI).
//
// OpenAPI-контракт `/contracts` (ORCH-TASK-056) поддерживает server-side фильтры:
// status (DocumentStatus), search, risk_level (single), contract_type[] (multi),
// processing_status[] (multi), date_from/date_to, sort/order. Все значения опций
// берутся из канонических enum-источников (один источник истины) — стейл-значений
// (RENT/CONSULTING и т.п.) больше нет. Маппинг значений в ListParams — в
// use-contracts-list-query.ts.
import { CONTRACT_TYPE_LABELS, CONTRACT_TYPES } from '@/entities/contract';
import { RISK_LEVEL_META, RISK_LEVELS, type RiskLevel } from '@/entities/risk';
import type { FilterDefinition, FilterOption } from '@/features/filters';
import type { ContractType, UserProcessingStatus } from '@/shared/api';

export const DOCUMENT_STATUS_OPTIONS = [
  { value: 'ACTIVE', label: 'Активные' },
  { value: 'ARCHIVED', label: 'В архиве' },
  { value: 'DELETED', label: 'Удалённые' },
] as const;

// Типы договоров (классификация LIC) — value = EN enum, label = RU. Опции
// строятся из CONTRACT_TYPES + CONTRACT_TYPE_LABELS (один источник истины с
// колонкой «Тип»), поэтому набор значений всегда валиден для бэкенда.
export const CONTRACT_TYPE_OPTIONS: readonly FilterOption<ContractType>[] = CONTRACT_TYPES.map(
  (type) => ({ value: type, label: CONTRACT_TYPE_LABELS[type] }),
);

// Уровень риска (single) — лейблы из RISK_LEVEL_META (совпадают с RiskBadge,
// чтобы «Высокий риск» в фильтре == бейдж в строке).
export const RISK_LEVEL_OPTIONS: readonly FilterOption<RiskLevel>[] = RISK_LEVELS.map((level) => ({
  value: level,
  label: RISK_LEVEL_META[level].label,
}));

// Пользовательские группы статуса обработки (фильтр Figma 200:2).
//
// Отклонение от AC FE-TASK-058 п.4 «Завершено/В обработке/Требует внимания/Ошибка»:
// группа «Требует внимания» НЕ реализована — её единственный статус
// AWAITING_USER_INPUT orchestrator-managed и отклоняется бэкендом в processing_status
// (→ 400 VALIDATION_ERROR, openapi listContracts), т.е. не может быть server-фильтром.
// Поэтому группы: «Завершено» / «В обработке» / «С ошибкой».
//
// Соответствие с BUCKET_MAP (entities/contract/model/status-view): «С ошибкой» = bucket
// `failed`; «Завершено» = bucket `ready`. «В обработке» намеренно ШИРЕ bucket
// `in_progress` — включает ещё bucket `pending` (UPLOADED/QUEUED), т.к. для пользователя
// «в работе» = всё в полёте. Это сознательное расхождение с page-scoped счётчиком
// «в обработке» в ContractsMetricsStrip (тот = строго bucket `in_progress`); счётчик и
// фильтр — разные срезы, не единый источник.
export type ProcessingGroup = 'done' | 'in_progress' | 'error';

export const PROCESSING_GROUP_OPTIONS = [
  { value: 'done', label: 'Завершено' },
  { value: 'in_progress', label: 'В обработке' },
  { value: 'error', label: 'С ошибкой' },
] as const satisfies readonly FilterOption<ProcessingGroup>[];

// Статусы, допустимые для server-фильтрации (всё, кроме AWAITING_USER_INPUT).
export type ServerFilterableStatus = Exclude<UserProcessingStatus, 'AWAITING_USER_INPUT'>;

// Развёртка группы → набор UserProcessingStatus (OR на бэкенде). `satisfies`
// с ServerFilterableStatus гарантирует на этапе компиляции, что AWAITING_USER_INPUT
// сюда не попадёт ни в одну группу.
export const PROCESSING_GROUP_TO_STATUSES = {
  done: ['READY'],
  in_progress: ['UPLOADED', 'QUEUED', 'PROCESSING', 'ANALYZING', 'GENERATING_REPORTS'],
  error: ['PARTIALLY_FAILED', 'FAILED', 'REJECTED'],
} as const satisfies Record<ProcessingGroup, readonly ServerFilterableStatus[]>;

export const DATE_RANGE_OPTIONS = [
  { value: '7d', label: 'За 7 дней' },
  { value: '30d', label: 'За 30 дней' },
  { value: '90d', label: 'За 90 дней' },
  { value: 'all', label: 'Всё время' },
] as const;

/**
 * Определения фильтров для `useFilterParams`.
 * `pinned: true` — чипы рендерятся в основной полосе; `pinned: false` — в
 * модалке «Ещё фильтры» (§8.3). Закреплены компактные оси (Состояние, Риск —
 * 3+3 чипа); тип договора (12), статус обработки (3) и период — в модалке,
 * чтобы не разрастать полосу фильтров.
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
    key: 'risk',
    label: 'Риск',
    kind: 'single',
    options: RISK_LEVEL_OPTIONS,
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
    key: 'processing',
    label: 'Статус обработки',
    kind: 'multi',
    options: PROCESSING_GROUP_OPTIONS,
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
