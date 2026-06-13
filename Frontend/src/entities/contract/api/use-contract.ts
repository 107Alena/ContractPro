// useContract — useQuery на GET /contracts/{id} (§17.1, §17.3).
//
// queryKey: qk.contracts.byId(id); enabled: Boolean(id). Используется на
// ContractDetailPage (FE-TASK-045). Инвалидация — archive/delete/version-upload
// мутациями (§17.3).
//
// SSE-обновления статусов текущей версии идут через useEventStream в
// qk.contracts.status(id,vid) (§7.7). Пока текущая версия в обработке,
// дополнительно поллим `byId` (refetchInterval) — polling-fallback к SSE:
// гарантирует, что ResultPage/ContractDetailPage увидят смену статуса до READY
// даже без живого SSE-канала. На терминальных статусах интервал выключается.
import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { http, isOrchestratorError, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

export type ContractDetails = components['schemas']['ContractDetails'];

type ProcessingStatus = NonNullable<ContractDetails['current_version']>['processing_status'];

// Непереходные статусы pipeline'а §5.2 — пока версия в одном из них, поллим.
const IN_PROGRESS_STATUSES = new Set<ProcessingStatus>([
  'UPLOADED',
  'QUEUED',
  'PROCESSING',
  'ANALYZING',
  'GENERATING_REPORTS',
]);

const POLL_INTERVAL_MS = 2_000;

const ENDPOINT = (id: string) => `/contracts/${id}`;

async function fetchContract(id: string, signal?: AbortSignal): Promise<ContractDetails> {
  const config: Parameters<typeof http.get>[1] = {};
  if (signal) config.signal = signal;
  const { data } = await http.get<ContractDetails>(ENDPOINT(id), config);
  return data;
}

export function useContract(
  id: string | undefined,
  options: { enabled?: boolean } = {},
): UseQueryResult<ContractDetails> {
  const { enabled = true } = options;
  return useQuery({
    queryKey: qk.contracts.byId(id ?? ''),
    queryFn: ({ signal }) => fetchContract(id as string, signal),
    enabled: Boolean(id) && enabled,
    staleTime: 30_000,
    // Polling-fallback: тикаем только пока текущая версия в обработке.
    refetchInterval: (query) => {
      const status = query.state.data?.current_version?.processing_status;
      return status && IN_PROGRESS_STATUSES.has(status) ? POLL_INTERVAL_MS : false;
    },
    // Soft-404 CONTRACT_NOT_FOUND: страница рендерит inline NotFound-state,
    // retry тратит network-touch впустую. Паттерн зеркалит useDiff (§9.3).
    retry: (count, err) =>
      !(isOrchestratorError(err) && err.error_code === 'CONTRACT_NOT_FOUND') && count < 1,
  });
}

export { ENDPOINT as CONTRACT_ENDPOINT };
