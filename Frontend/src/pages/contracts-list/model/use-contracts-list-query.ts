// Page-level glue: склейка поиска + фильтров + пагинации в единый вызов
// useContracts (§17.3). Логика маппинга `filters.values` в `ListParams`
// изолирована здесь, чтобы page-компонент оставался декларативным.
//
// v1 backend поддерживает только `status`; остальные фильтры сохраняются
// в URL, но не уходят в API (см. filter-definitions.ts TODO).
import { useMemo } from 'react';

import { type ContractList, type ContractSummary, useContracts } from '@/entities/contract';
import { useFilterParams } from '@/features/filters';
import { usePageParams } from '@/features/pagination';
import { useDebouncedSearchParam } from '@/features/search';
import { type DocumentStatus, type ListParams } from '@/shared/api';

import { CONTRACTS_FILTER_DEFINITIONS } from './filter-definitions';

const DOCUMENT_STATUSES: ReadonlyArray<DocumentStatus> = ['ACTIVE', 'ARCHIVED', 'DELETED'];

function toDocumentStatus(value: unknown): DocumentStatus | undefined {
  if (typeof value !== 'string') return undefined;
  return (DOCUMENT_STATUSES as readonly string[]).includes(value)
    ? (value as DocumentStatus)
    : undefined;
}

export interface UseContractsListQueryResult {
  // URL state exposed to UI:
  search: ReturnType<typeof useDebouncedSearchParam>;
  filters: ReturnType<typeof useFilterParams>;
  pagination: ReturnType<typeof usePageParams>;
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

  // Мапим только server-supported фильтр (status). Остальные остаются в URL
  // как UI state до появления серверной поддержки.
  const params = useMemo<ListParams>(() => {
    const p: ListParams = {
      page: pagination.page,
      size: pagination.size,
    };
    const status = toDocumentStatus(filters.values.status);
    if (status !== undefined) p.status = status;
    if (search.committedValue !== '') p.search = search.committedValue;
    return p;
  }, [pagination.page, pagination.size, filters.values.status, search.committedValue]);

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
