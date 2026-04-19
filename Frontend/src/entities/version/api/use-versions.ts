// useVersions — useQuery на GET /contracts/{id}/versions (§17.1, §17.3).
//
// queryKey: qk.contracts.versions(contractId); enabled по Boolean(contractId).
// Используется на ContractDetailPage (FE-TASK-045) для VersionsTimeline,
// ChecksHistory, VersionPicker. Инвалидация — version-upload/recheck мутации
// (§17.3). SSE status-update попадает в qk.contracts.status(id,vid), сам
// список не реактивный на отдельные статусы — refetch по staleTime.
import { keepPreviousData, useQuery, type UseQueryResult } from '@tanstack/react-query';

import { http, type ListParams, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

export type VersionList = components['schemas']['VersionList'];
export type VersionDetails = components['schemas']['VersionDetails'];

const ENDPOINT = (contractId: string) => `/contracts/${contractId}/versions`;

async function fetchVersions(
  contractId: string,
  params: ListParams,
  signal?: AbortSignal,
): Promise<VersionList> {
  const config: Parameters<typeof http.get>[1] = { params };
  if (signal) config.signal = signal;
  const { data } = await http.get<VersionList>(ENDPOINT(contractId), config);
  return data;
}

export function useVersions(
  contractId: string | undefined,
  params: ListParams = {},
  options: { enabled?: boolean } = {},
): UseQueryResult<VersionList> {
  const { enabled = true } = options;
  return useQuery({
    queryKey: qk.contracts.versions(contractId ?? ''),
    queryFn: ({ signal }) => fetchVersions(contractId as string, params, signal),
    enabled: Boolean(contractId) && enabled,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });
}

export { ENDPOINT as VERSIONS_ENDPOINT };
