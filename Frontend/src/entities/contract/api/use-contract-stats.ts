// useContractStats — useQuery на GET /contracts/stats (§17.1 ORCH, Путь C).
//
// Агрегированные счётчики договоров организации для дашборда (карточка «Сводка»).
// «проверено» = total, «в работе» = сумма незавершённых processing-статусов
// (см. inProgressCount). Источник истины — Document Management; Orchestrator
// агрегирует. На текущем этапе (Путь C) эндпоинт замокан в MSW; реальная
// реализация DM+Orchestrator — ORCH-TASK-057 / DM-TASK-059.
import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { http, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

export type ContractStats = components['schemas']['ContractStats'];

const ENDPOINT = '/contracts/stats';

async function fetchContractStats(signal?: AbortSignal): Promise<ContractStats> {
  const config: Parameters<typeof http.get>[1] = {};
  if (signal) config.signal = signal;
  const { data } = await http.get<ContractStats>(ENDPOINT, config);
  return data;
}

export function useContractStats(): UseQueryResult<ContractStats> {
  return useQuery({
    queryKey: qk.contracts.stats,
    queryFn: ({ signal }) => fetchContractStats(signal),
    // Статистика обновляется реже списка — больше staleTime.
    staleTime: 60_000,
  });
}

// «В работе» = договоры с незавершённой обработкой: pending (UPLOADED, QUEUED) +
// in_progress (PROCESSING, ANALYZING, GENERATING_REPORTS). НЕ входят: READY
// (готово), AWAITING_USER_INPUT (ждёт пользователя), FAILED/REJECTED/
// PARTIALLY_FAILED (терминальные), not_started (без версии). Соответствует
// bucket-модели entities/contract/model/status-view (pending+in_progress).
export function inProgressCount(stats: ContractStats | undefined): number | undefined {
  if (!stats) return undefined;
  const s = stats.by_processing_status;
  return s.uploaded + s.queued + s.processing + s.analyzing + s.generating_reports;
}
