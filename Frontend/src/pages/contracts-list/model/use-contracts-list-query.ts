// Page-level glue: склейка поиска + фильтров + сортировки + пагинации в единый
// вызов useContracts (§17.3). Логика маппинга `filters.values` в `ListParams`
// изолирована здесь, чтобы page-компонент оставался декларативным.
//
// Server-side фильтры (ORCH-TASK-056): status, search, risk_level, contract_type[],
// processing_status[], date_from, sort/order. Все значения из URL проходят через
// narrowing-guards (отбрасывают невалидные + дают mutable-массивы под generated-тип);
// массивы сортируются для стабильности query-key (одинаковый логический набор →
// один кэш-ключ независимо от порядка кликов). Пустые значения НЕ кладутся в params
// (exactOptionalPropertyTypes + чистый кэш-ключ).
import { useEffect, useMemo, useRef } from 'react';

import {
  CONTRACT_TYPES,
  type ContractList,
  type ContractSummary,
  useContracts,
} from '@/entities/contract';
import { RISK_LEVELS, type RiskLevel } from '@/entities/risk';
import { useFilterParams } from '@/features/filters';
import { usePageParams } from '@/features/pagination';
import { useDebouncedSearchParam } from '@/features/search';
import { type ContractType, type DocumentStatus, type ListParams } from '@/shared/api';

import {
  CONTRACTS_FILTER_DEFINITIONS,
  PROCESSING_GROUP_TO_STATUSES,
  type ProcessingGroup,
  type ServerFilterableStatus,
} from './filter-definitions';
import { useContractsSort } from './use-contracts-sort';

const DOCUMENT_STATUSES: ReadonlyArray<DocumentStatus> = ['ACTIVE', 'ARCHIVED', 'DELETED'];
const PROCESSING_GROUPS: ReadonlyArray<ProcessingGroup> = ['done', 'in_progress', 'error'];
const DAY_MS = 86_400_000;
const DATE_RANGE_DAYS: Readonly<Record<string, number>> = { '7d': 7, '30d': 30, '90d': 90 };

function toDocumentStatus(value: unknown): DocumentStatus | undefined {
  if (typeof value !== 'string') return undefined;
  return (DOCUMENT_STATUSES as readonly string[]).includes(value)
    ? (value as DocumentStatus)
    : undefined;
}

function toRiskLevel(value: unknown): RiskLevel | undefined {
  if (typeof value !== 'string') return undefined;
  return (RISK_LEVELS as readonly string[]).includes(value) ? (value as RiskLevel) : undefined;
}

// Отбирает валидные ContractType, дедуплицирует и сортирует (стабильный кэш-ключ).
function toContractTypes(value: unknown): ContractType[] {
  if (!Array.isArray(value)) return [];
  const allow = new Set<string>(CONTRACT_TYPES);
  const valid = value.filter((v): v is ContractType => typeof v === 'string' && allow.has(v));
  return [...new Set(valid)].sort();
}

// Разворачивает выбранные группы в server-safe статусы. AWAITING_USER_INPUT
// невозможен по построению PROCESSING_GROUP_TO_STATUSES (тип ServerFilterableStatus),
// но guard всё равно опирается только на эту карту — даже при подмене URL невалидная
// группа игнорируется, и AWAITING_USER_INPUT никогда не уходит на бэкенд (иначе 400).
function toProcessingStatuses(value: unknown): ServerFilterableStatus[] {
  if (!Array.isArray(value)) return [];
  const set = new Set<ServerFilterableStatus>();
  for (const group of value) {
    if (typeof group === 'string' && (PROCESSING_GROUPS as readonly string[]).includes(group)) {
      for (const status of PROCESSING_GROUP_TO_STATUSES[group as ProcessingGroup]) set.add(status);
    }
  }
  return [...set].sort();
}

// Чип «Период» → нижняя граница date_from в день-гранулярности (YYYY-MM-DD).
// День-гранулярность (а не ISO с миллисекундами) обязательна: значение стабильно
// в пределах суток, иначе каждый ре-расчёт memo (ввод в поиске, смена страницы)
// давал бы новый timestamp → новый query-key → лишний refetch + сброс
// keepPreviousData. 'all'/неизвестное → без нижней границы.
function dateFromForRange(token: unknown): string | undefined {
  if (typeof token !== 'string') return undefined;
  const days = DATE_RANGE_DAYS[token];
  if (days === undefined) return undefined;
  return new Date(Date.now() - days * DAY_MS).toISOString().slice(0, 10);
}

export interface UseContractsListQueryResult {
  // URL state exposed to UI:
  search: ReturnType<typeof useDebouncedSearchParam>;
  filters: ReturnType<typeof useFilterParams>;
  pagination: ReturnType<typeof usePageParams>;
  sort: ReturnType<typeof useContractsSort>;
  // Derived request params (normalised):
  params: ListParams;
  // TanStack query:
  query: ReturnType<typeof useContracts>;
  // Derived state:
  data: ContractList | undefined;
  items: readonly ContractSummary[];
  total: number;
  isLoading: boolean;
  isFetching: boolean;
  isError: boolean;
  error: unknown;
  hasActiveFilters: boolean;
}

export function useContractsListQuery(): UseContractsListQueryResult {
  const search = useDebouncedSearchParam({ key: 'q', minLength: 0, debounceMs: 300 });
  const filters = useFilterParams({ definitions: CONTRACTS_FILTER_DEFINITIONS });
  const pagination = usePageParams();
  const sort = useContractsSort();

  // Сброс на 1-ю страницу при смене server-фильтров/поиска (сортировка делает это
  // сама в useContractsSort). Иначе на page>1 фильтр, сузивший выборку, запросил бы
  // несуществующую страницу → пустой ответ при total>0 и ложное «ничего не найдено».
  // Первый рендер пропускаем — чтобы deep-link `?page=2&risk=high` открывался как есть.
  const selectionKey = `${JSON.stringify(filters.values)}|${search.committedValue}`;
  const prevSelectionKey = useRef<string | null>(null);
  const { page: currentPage, setPage } = pagination;
  useEffect(() => {
    if (prevSelectionKey.current === null) {
      prevSelectionKey.current = selectionKey;
      return;
    }
    if (prevSelectionKey.current !== selectionKey) {
      prevSelectionKey.current = selectionKey;
      if (currentPage > 1) setPage(1);
    }
  }, [selectionKey, currentPage, setPage]);

  const params = useMemo<ListParams>(() => {
    const p: ListParams = {
      page: pagination.page,
      size: pagination.size,
    };
    const status = toDocumentStatus(filters.values.status);
    if (status !== undefined) p.status = status;
    if (search.committedValue !== '') p.search = search.committedValue;

    const riskLevel = toRiskLevel(filters.values.risk);
    if (riskLevel !== undefined) p.risk_level = riskLevel;

    const contractTypes = toContractTypes(filters.values.type);
    if (contractTypes.length > 0) p.contract_type = contractTypes;

    const processingStatuses = toProcessingStatuses(filters.values.processing);
    if (processingStatuses.length > 0) p.processing_status = processingStatuses;

    const dateFrom = dateFromForRange(filters.values.date);
    if (dateFrom !== undefined) p.date_from = dateFrom;

    if (!sort.isDefault) {
      p.sort = sort.sort;
      p.order = sort.order;
    }
    return p;
  }, [
    pagination.page,
    pagination.size,
    filters.values.status,
    filters.values.risk,
    filters.values.type,
    filters.values.processing,
    filters.values.date,
    search.committedValue,
    sort.sort,
    sort.order,
    sort.isDefault,
  ]);

  const query = useContracts(params);
  const items: readonly ContractSummary[] = query.data?.items ?? [];
  const total = query.data?.total ?? items.length;

  const hasActiveFilters =
    filters.activeCount > 0 ||
    search.committedValue !== '' ||
    (search.inputValue !== '' && search.inputValue !== search.committedValue);

  return {
    search,
    filters,
    pagination,
    sort,
    params,
    query,
    data: query.data,
    items,
    total,
    isLoading: query.isLoading,
    isFetching: query.isFetching,
    isError: query.isError,
    error: query.error,
    hasActiveFilters,
  };
}
