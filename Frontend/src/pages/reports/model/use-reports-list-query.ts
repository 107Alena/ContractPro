// Page-level хук для ReportsPage (FE-TASK-048, §17.4).
//
// Поверх useContracts() применяет клиентский фильтр по processing_status —
// серверный GET /contracts не поддерживает этот параметр (см.
// api-specification.yaml:180-193). Это честная плата за то, что для v1 нет
// отдельного /reports-endpoint'а; мы полагаемся на то, что у организации
// десятки-сотни договоров (не десятки тысяч), и фильтрация на клиенте не
// ухудшает UX.
//
// `state`-фильтр — единственный способ выбрать processing_status; server
// `status` зафиксирован на ACTIVE (архивные и удалённые в реестре отчётов не
// показываются).
import { useMemo } from 'react';

import { type ContractList, type ContractSummary, useContracts } from '@/entities/contract';
import { useFilterParams } from '@/features/filters';
import { usePageParams } from '@/features/pagination';
import { useDebouncedSearchParam } from '@/features/search';
import { type ListParams } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

import {
  type ReportPeriodFilter,
  REPORTS_FILTER_DEFINITIONS,
  type ReportStateFilter,
} from './filter-definitions';

type UserProcessingStatus = components['schemas']['UserProcessingStatus'];

const REPORT_STATE_VALUES: ReadonlyArray<ReportStateFilter> = ['READY', 'PARTIALLY_FAILED'];
const REPORT_PERIOD_VALUES: ReadonlyArray<ReportPeriodFilter> = ['7d', '30d', '90d', 'all'];

function toReportState(value: unknown): ReportStateFilter {
  return typeof value === 'string' && (REPORT_STATE_VALUES as readonly string[]).includes(value)
    ? (value as ReportStateFilter)
    : 'READY';
}

function toReportPeriod(value: unknown): ReportPeriodFilter {
  return typeof value === 'string' && (REPORT_PERIOD_VALUES as readonly string[]).includes(value)
    ? (value as ReportPeriodFilter)
    : 'all';
}

const PERIOD_TO_MS: Record<ReportPeriodFilter, number | null> = {
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
  '90d': 90 * 24 * 60 * 60 * 1000,
  all: null,
};

export function isWithinPeriod(iso: string | undefined, period: ReportPeriodFilter): boolean {
  const windowMs = PERIOD_TO_MS[period];
  if (windowMs == null) return true;
  if (!iso) return false;
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return false;
  return Date.now() - t <= windowMs;
}

export function filterReports(
  items: readonly ContractSummary[],
  state: ReportStateFilter,
  period: ReportPeriodFilter,
): readonly ContractSummary[] {
  const wantedProcessingStatus: UserProcessingStatus = state;
  return items.filter((it) => {
    if (it.processing_status !== wantedProcessingStatus) return false;
    if (!isWithinPeriod(it.updated_at ?? it.created_at, period)) return false;
    return true;
  });
}

export interface UseReportsListQueryResult {
  search: ReturnType<typeof useDebouncedSearchParam>;
  filters: ReturnType<typeof useFilterParams>;
  pagination: ReturnType<typeof usePageParams>;
  params: ListParams;
  query: ReturnType<typeof useContracts>;
  data: ContractList | undefined;
  /** Отфильтрованные по processing_status + period items текущей страницы. */
  items: readonly ContractSummary[];
  /** Полный items[] с сервера — используется для метрик (считать честные счётчики
   *  до клиентской фильтрации). */
  rawItems: readonly ContractSummary[];
  /** Серверный total (по всем страницам с выбранным DocumentStatus). */
  total: number;
  state: ReportStateFilter;
  period: ReportPeriodFilter;
  isLoading: boolean;
  isFetching: boolean;
  isError: boolean;
  error: unknown;
  hasActiveFilters: boolean;
}

export function useReportsListQuery(): UseReportsListQueryResult {
  const search = useDebouncedSearchParam({ key: 'q', minLength: 0, debounceMs: 300 });
  const filters = useFilterParams({ definitions: REPORTS_FILTER_DEFINITIONS });
  const pagination = usePageParams();

  const state = toReportState(filters.values.state);
  const period = toReportPeriod(filters.values.period);

  const params = useMemo<ListParams>(() => {
    const p: ListParams = {
      page: pagination.page,
      size: pagination.size,
      status: 'ACTIVE',
    };
    if (search.committedValue !== '') p.search = search.committedValue;
    return p;
  }, [pagination.page, pagination.size, search.committedValue]);

  const query = useContracts(params);
  const rawItems = useMemo<readonly ContractSummary[]>(
    () => query.data?.items ?? [],
    [query.data?.items],
  );
  const items = useMemo(() => filterReports(rawItems, state, period), [rawItems, state, period]);
  const total = query.data?.total ?? rawItems.length;

  const hasActiveFilters =
    filters.activeCount > 0 ||
    search.committedValue !== '' ||
    (search.inputValue !== '' && search.inputValue !== search.committedValue);

  return {
    search,
    filters,
    pagination,
    params,
    query,
    data: query.data,
    items,
    rawItems,
    total,
    state,
    period,
    isLoading: query.isLoading,
    isFetching: query.isFetching,
    isError: query.isError,
    error: query.error,
    hasActiveFilters,
  };
}
