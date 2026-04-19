// Доменные типы feature contract-archive.
//
// Endpoint: POST /contracts/{contract_id}/archive. Запрос без тела; ответ 200 —
// обновлённый `ContractSummary` со `status='ARCHIVED'`. Все поля
// `ContractSummary` в OpenAPI nullable (`?`), narrow сохраняет их через spread.
import type { components } from '@/shared/api/openapi';

export type ContractSummary = components['schemas']['ContractSummary'];

export interface ArchiveContractInput {
  contractId: string;
}

/** Narrowed-ответ: все поля `ContractSummary` optional (как в OpenAPI). */
export type ArchiveContractResponse = ContractSummary;
