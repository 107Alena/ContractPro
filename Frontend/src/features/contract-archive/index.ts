// Barrel: публичный API feature contract-archive (§6.1, §17.3 high-architecture).
//
// Импортировать ТОЛЬКО этот путь (FSD-граница). Потребители — ContractsListPage
// (FE-TASK-044) и ContractDetailPage (FE-TASK-045).
export { archiveContract, archiveContractEndpoint } from './api/archive-contract';
export type { ArchiveContractInput, ArchiveContractResponse, ContractSummary } from './model/types';
export type {
  UseArchiveContractOptions,
  UseArchiveContractResult,
} from './model/use-archive-contract';
export { useArchiveContract } from './model/use-archive-contract';
