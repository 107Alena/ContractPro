// Доменные типы feature contract-delete.
//
// Endpoint: DELETE /contracts/{contract_id} — soft delete в DM.
// Ответ 200 — обновлённый `ContractSummary` со `status='DELETED'`.
// Семантика: ресурс продолжает существовать (можно восстановить), но
// скрывается из дефолтных list-запросов (фильтруется по status в UI).
import type { components } from '@/shared/api/openapi';

export type ContractSummary = components['schemas']['ContractSummary'];

export interface DeleteContractInput {
  contractId: string;
}

/** Narrowed-ответ: все поля `ContractSummary` optional (как в OpenAPI). */
export type DeleteContractResponse = ContractSummary;
