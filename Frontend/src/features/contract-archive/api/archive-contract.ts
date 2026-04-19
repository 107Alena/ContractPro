// POST /contracts/{contract_id}/archive (§7.5 api-specification).
//
// Контракт: POST без тела → 200 `ContractSummary` со `status='ARCHIVED'`.
// Тонкая обёртка: вызывает axios, сужает тип ответа. 409 конфликты
// (DOCUMENT_ARCHIVED / VERSION_STILL_PROCESSING) прокидываются как
// OrchestratorError через shared interceptor.
import type { components } from '@/shared/api/openapi';

import type { ArchiveContractInput, ArchiveContractResponse } from '../model/types';
import { getHttpInstance } from './http';

function endpointFor(contractId: string): string {
  return `/contracts/${encodeURIComponent(contractId)}/archive`;
}

type RawResponse = components['schemas']['ContractSummary'];

/**
 * Архивирует договор (UR-6). POST без тела; параметр берётся из path.
 * Таймаут дефолтный — операция быстрая (UPDATE status в БД).
 */
export async function archiveContract(
  input: ArchiveContractInput,
  opts: { signal?: AbortSignal } = {},
): Promise<ArchiveContractResponse> {
  const http = getHttpInstance();
  const { data } = await http.post<RawResponse>(
    endpointFor(input.contractId),
    // body=undefined для POST без тела. Axios при undefined не выставляет
    // Content-Type и не сериализует payload — нужное поведение.
    undefined,
    {
      ...(opts.signal && { signal: opts.signal }),
    },
  );
  return data;
}

export { endpointFor as archiveContractEndpoint };
