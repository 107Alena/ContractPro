// Barrel: публичный API feature contract-delete (§6.1, §17.3 high-architecture).
//
// Импортировать ТОЛЬКО этот путь (FSD-граница). Потребители — ContractsListPage
// (FE-TASK-044) и ContractDetailPage (FE-TASK-045).
export { deleteContract, deleteContractEndpoint } from './api/delete-contract';
export type { ContractSummary, DeleteContractInput, DeleteContractResponse } from './model/types';
export type {
  UseDeleteContractOptions,
  UseDeleteContractResult,
} from './model/use-delete-contract';
export { useDeleteContract } from './model/use-delete-contract';
export {
  ConfirmDeleteContractModal,
  type ConfirmDeleteContractModalProps,
} from './ui/ConfirmDeleteContractModal';
