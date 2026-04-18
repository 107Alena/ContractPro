// useContracts — useQuery на GET /contracts (§17.1, §17.3).
//
// queryKey: qk.contracts.list(params); placeholderData: keepPreviousData
// сохраняет предыдущий набор при смене filter/page (плавный UX, без skeleton
// на каждый переход). DashboardPage использует size=5 (5 последних проверок).
//
// SSE-обновления статусов попадают напрямую в qk.contracts.status(id,vid)
// через useEventStream (§7.7). Список /contracts инвалидируется при
// мутациях upload/archive/delete (§17.3); на dashboard — пересматривается
// по обычному TanStack refetch'у.
import { keepPreviousData, useQuery, type UseQueryResult } from '@tanstack/react-query';

import { http, type ListParams, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

export type ContractSummary = components['schemas']['ContractSummary'];
export type ContractList = components['schemas']['ContractList'];

const ENDPOINT = '/contracts';

async function fetchContracts(params: ListParams, signal?: AbortSignal): Promise<ContractList> {
  const config: Parameters<typeof http.get>[1] = { params };
  if (signal) config.signal = signal;
  const { data } = await http.get<ContractList>(ENDPOINT, config);
  return data;
}

export function useContracts(params: ListParams = {}): UseQueryResult<ContractList> {
  return useQuery({
    queryKey: qk.contracts.list(params),
    queryFn: ({ signal }) => fetchContracts(params, signal),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });
}

export { ENDPOINT as CONTRACTS_ENDPOINT };
