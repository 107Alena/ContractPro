// DELETE /contracts/{contract_id} (§7.5 api-specification).
//
// Soft-delete в DM. Ответ 200 — `ContractSummary` со `status='DELETED'`.
// 409 конфликты (VERSION_STILL_PROCESSING) прокидываются как OrchestratorError.
import type { components } from '@/shared/api/openapi';

import type { DeleteContractInput, DeleteContractResponse } from '../model/types';
import { getHttpInstance } from './http';

function endpointFor(contractId: string): string {
  return `/contracts/${encodeURIComponent(contractId)}`;
}

type RawResponse = components['schemas']['ContractSummary'];

/**
 * Удаляет (soft) договор (UR-6). DELETE без тела; параметр берётся из path.
 */
export async function deleteContract(
  input: DeleteContractInput,
  opts: { signal?: AbortSignal } = {},
): Promise<DeleteContractResponse> {
  const http = getHttpInstance();
  const { data } = await http.delete<RawResponse>(endpointFor(input.contractId), {
    ...(opts.signal && { signal: opts.signal }),
  });
  return data;
}

export { endpointFor as deleteContractEndpoint };
