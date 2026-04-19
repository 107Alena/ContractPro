// useResults — useQuery на GET /contracts/{id}/versions/{vid}/results (§17.5, §17.3).
//
// Единый агрегированный endpoint: возвращает AnalysisResults со всеми артефактами
// LIC (risks, recommendations, summary, key_parameters, risk_profile, aggregate_score,
// contract_type, warnings). Backend фильтрует по роли: BUSINESS_USER не получает
// поля risks/recommendations (§17.5 row 3). ResultPage (FE-TASK-046) вызывает
// ТОЛЬКО useResults — под-ресурсы (useRisks/useSummary/...) существуют для
// других потребителей (Dashboard KeyRisksCards, ContractDetail).
//
// 404 ARTIFACT_NOT_FOUND типично означает "ещё не готово" (§17.5 п.4 / §7.3
// catalog), page сама принимает решение по processing_status (SSE live), retry
// лимитирован 1 попыткой.
import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { http, isOrchestratorError, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

export type AnalysisResults = components['schemas']['AnalysisResults'];

const ENDPOINT = (contractId: string, versionId: string): string =>
  `/contracts/${contractId}/versions/${versionId}/results`;

async function fetchResults(
  contractId: string,
  versionId: string,
  signal?: AbortSignal,
): Promise<AnalysisResults> {
  const config: Parameters<typeof http.get>[1] = {};
  if (signal) config.signal = signal;
  const { data } = await http.get<AnalysisResults>(ENDPOINT(contractId, versionId), config);
  return data;
}

export interface UseResultsParams {
  contractId: string | undefined;
  versionId: string | undefined;
}

export interface UseResultsOptions {
  enabled?: boolean;
}

export function useResults(
  params: UseResultsParams,
  options: UseResultsOptions = {},
): UseQueryResult<AnalysisResults> {
  const { contractId, versionId } = params;
  const { enabled = true } = options;
  return useQuery({
    queryKey: qk.contracts.results(contractId ?? '', versionId ?? ''),
    queryFn: ({ signal }) => fetchResults(contractId as string, versionId as string, signal),
    enabled: Boolean(contractId && versionId) && enabled,
    staleTime: 30_000,
    retry: (count, err) =>
      !(
        isOrchestratorError(err) &&
        (err.error_code === 'CONTRACT_NOT_FOUND' ||
          err.error_code === 'VERSION_NOT_FOUND' ||
          err.error_code === 'ARTIFACT_NOT_FOUND')
      ) && count < 1,
  });
}

export { ENDPOINT as RESULTS_ENDPOINT };
