// useRisks — useQuery на GET /contracts/{id}/versions/{vid}/risks (§17.5 row 8).
//
// Sub-resource endpoint: нужен отдельным потребителям, которым достаточно
// только списка рисков без полного AnalysisResults — Dashboard «Ключевые
// риски» и ContractDetail KeyRisks (FE-TASK-048). ResultPage использует
// useResults (§17.5) и НЕ дёргает useRisks. BUSINESS_USER получает 403 —
// потребители обязаны оборачивать вызов `enabled: useCan('risks.view')`.
import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { http, isOrchestratorError, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

export type RiskList = components['schemas']['RiskList'];

const ENDPOINT = (contractId: string, versionId: string): string =>
  `/contracts/${contractId}/versions/${versionId}/risks`;

async function fetchRisks(
  contractId: string,
  versionId: string,
  signal?: AbortSignal,
): Promise<RiskList> {
  const config: Parameters<typeof http.get>[1] = {};
  if (signal) config.signal = signal;
  const { data } = await http.get<RiskList>(ENDPOINT(contractId, versionId), config);
  return data;
}

export interface UseRisksParams {
  contractId: string | undefined;
  versionId: string | undefined;
}

export interface UseRisksOptions {
  enabled?: boolean;
}

export function useRisks(
  params: UseRisksParams,
  options: UseRisksOptions = {},
): UseQueryResult<RiskList> {
  const { contractId, versionId } = params;
  const { enabled = true } = options;
  return useQuery({
    queryKey: qk.contracts.risks(contractId ?? '', versionId ?? ''),
    queryFn: ({ signal }) => fetchRisks(contractId as string, versionId as string, signal),
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

export { ENDPOINT as RISKS_ENDPOINT };
