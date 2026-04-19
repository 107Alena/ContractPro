// useRecommendations — useQuery на GET /contracts/{id}/versions/{vid}/recommendations (§17.5 row 10).
//
// Sub-resource endpoint. BUSINESS_USER получает 403 — потребители оборачивают
// `enabled: useCan('recommendations.view')`. ResultPage использует useResults
// и берёт recommendations из агрегата, useRecommendations нужен отдельным
// потребителям (ContractDetail recommendations-list в FE-TASK-048).
import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { http, isOrchestratorError, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

export type RecommendationList = components['schemas']['RecommendationList'];

const ENDPOINT = (contractId: string, versionId: string): string =>
  `/contracts/${contractId}/versions/${versionId}/recommendations`;

async function fetchRecommendations(
  contractId: string,
  versionId: string,
  signal?: AbortSignal,
): Promise<RecommendationList> {
  const config: Parameters<typeof http.get>[1] = {};
  if (signal) config.signal = signal;
  const { data } = await http.get<RecommendationList>(ENDPOINT(contractId, versionId), config);
  return data;
}

export interface UseRecommendationsParams {
  contractId: string | undefined;
  versionId: string | undefined;
}

export interface UseRecommendationsOptions {
  enabled?: boolean;
}

export function useRecommendations(
  params: UseRecommendationsParams,
  options: UseRecommendationsOptions = {},
): UseQueryResult<RecommendationList> {
  const { contractId, versionId } = params;
  const { enabled = true } = options;
  return useQuery({
    queryKey: qk.contracts.recommendations(contractId ?? '', versionId ?? ''),
    queryFn: ({ signal }) =>
      fetchRecommendations(contractId as string, versionId as string, signal),
    enabled: Boolean(contractId && versionId) && enabled,
    staleTime: 30_000,
    retry: (count, err) =>
      !(
        isOrchestratorError(err) &&
        (err.error_code === 'ARTIFACT_NOT_FOUND' ||
          err.error_code === 'VERSION_NOT_FOUND' ||
          err.error_code === 'PERMISSION_DENIED')
      ) && count < 1,
  });
}

export { ENDPOINT as RECOMMENDATIONS_ENDPOINT };
