// POST /contracts/{contract_id}/versions/{version_id}/confirm-type (FR-2.1.3,
// §17.1 high-architecture, OpenAPI operation `confirmContractType`).
//
// Контракт: тело {contract_type, confirmed_by_user: true}; 202 UploadResponse;
// 400 INVALID contract_type / VALIDATION_ERROR; 409 VERSION_NOT_AWAITING_INPUT
// (модалка устарела — версия уже подтверждена другим пользователем или истёк
// watchdog ORCH_USER_CONFIRMATION_TIMEOUT). Маппинг в UI — `useConfirmType`.
import type { components } from '@/shared/api/openapi';

import type { ConfirmTypeInput } from '../model/types';
import { getHttpInstance } from './http';

type RawResponse = components['schemas']['UploadResponse'];

export interface ConfirmTypeResponse {
  contractId: string;
  versionId: string;
  status: string;
}

function endpointFor(contractId: string, versionId: string): string {
  return `/contracts/${encodeURIComponent(contractId)}/versions/${encodeURIComponent(versionId)}/confirm-type`;
}

/**
 * Защита от спецификационного drift'а: 202-ответ должен содержать contract_id /
 * version_id / status. Бэкенд по контракту их возвращает, OpenAPI помечает
 * optional. Не паникуем — pre-narrowing.
 */
function narrowResponse(raw: RawResponse, fallbackContractId: string): ConfirmTypeResponse {
  const contractId = typeof raw.contract_id === 'string' ? raw.contract_id : fallbackContractId;
  const versionId = raw.version_id;
  const status = raw.status;
  if (typeof versionId !== 'string' || typeof status !== 'string') {
    throw new Error('ConfirmTypeResponse: 202 без обязательных полей.');
  }
  return { contractId, versionId, status };
}

export async function confirmType(input: ConfirmTypeInput): Promise<ConfirmTypeResponse> {
  const http = getHttpInstance();
  const path = endpointFor(input.contractId, input.versionId);
  const body: components['schemas']['ConfirmTypeRequest'] = {
    contract_type: input.contractType,
    confirmed_by_user: true,
  };
  const { data } = await http.post<RawResponse>(path, body, {
    ...(input.signal && { signal: input.signal }),
  });
  return narrowResponse(data, input.contractId);
}

export { endpointFor as CONFIRM_TYPE_ENDPOINT };
