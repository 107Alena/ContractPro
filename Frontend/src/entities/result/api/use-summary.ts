// useSummary — useQuery на GET /contracts/{id}/versions/{vid}/summary (§17.5 row 11).
//
// Возвращает ContractSummaryResult = {summary, aggregate_score, key_parameters}.
// Доступен всем аутентифицированным ролям (в том числе BUSINESS_USER —
// это ключевой потребитель R-2 из ТЗ). ResultPage берёт summary из useResults;
// useSummary существует отдельно для лёгких потребителей вроде Dashboard
// «Last Check Card» (FE-TASK-048), где AnalysisResults избыточен.
import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { http, isOrchestratorError, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

export type ContractSummaryResult = components['schemas']['ContractSummaryResult'];

const ENDPOINT = (contractId: string, versionId: string): string =>
  `/contracts/${contractId}/versions/${versionId}/summary`;

async function fetchSummary(
  contractId: string,
  versionId: string,
  signal?: AbortSignal,
): Promise<ContractSummaryResult> {
  const config: Parameters<typeof http.get>[1] = {};
  if (signal) config.signal = signal;
  const { data } = await http.get<ContractSummaryResult>(ENDPOINT(contractId, versionId), config);
  return data;
}

export interface UseSummaryParams {
  contractId: string | undefined;
  versionId: string | undefined;
}

export interface UseSummaryOptions {
  enabled?: boolean;
}

export function useSummary(
  params: UseSummaryParams,
  options: UseSummaryOptions = {},
): UseQueryResult<ContractSummaryResult> {
  const { contractId, versionId } = params;
  const { enabled = true } = options;
  return useQuery({
    queryKey: qk.contracts.summary(contractId ?? '', versionId ?? ''),
    queryFn: ({ signal }) => fetchSummary(contractId as string, versionId as string, signal),
    enabled: Boolean(contractId && versionId) && enabled,
    staleTime: 30_000,
    retry: (count, err) =>
      !(
        isOrchestratorError(err) &&
        (err.error_code === 'ARTIFACT_NOT_FOUND' || err.error_code === 'VERSION_NOT_FOUND')
      ) && count < 1,
  });
}

export { ENDPOINT as SUMMARY_ENDPOINT };
