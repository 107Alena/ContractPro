// useDiff — React-хук на базе useQuery (§7.5, §16.2, §17.3).
//
// Контракт:
//   queryKey:  qk.contracts.diff(contractId, baseVersionId, targetVersionId);
//   queryFn:   getDiff({...}, { signal });
//   retry:     DIFF_NOT_FOUND (404) → НЕ ретраим (soft-state, ждём SSE); прочие — 1 раз.
//
// Рендер в page:
//   result.data             — VersionDiffResult, когда готов.
//   isDiffNotReadyError(err) — страница показывает "Сравнение ещё не готово".
//   прочие err              — toUserMessage → toast.
//
// SSE COMPARISON_COMPLETED инвалидирует qk.contracts.diff(...) через
// useEventStream (§7.7) — fresh-запрос запустится автоматически.
import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { OrchestratorError, qk } from '@/shared/api';

import { getDiff } from '../api/get-diff';
import type { GetDiffInput, VersionDiffResult } from './types';

const DEFAULT_RETRIES = 1;

export interface UseDiffOptions {
  /**
   * Включает query. Page выставляет false пока пользователь не выбрал обе версии.
   * При переходе на true и наличии кэша — отдаёт мгновенно; иначе — fetch.
   */
  enabled?: boolean;
  /** Переопределение staleTime; по умолчанию — дефолт queryClient (30s). */
  staleTime?: number;
}

export function useDiff(
  input: GetDiffInput,
  opts: UseDiffOptions = {},
): UseQueryResult<VersionDiffResult, OrchestratorError> {
  const baseOptions = {
    queryKey: qk.contracts.diff(input.contractId, input.baseVersionId, input.targetVersionId),
    queryFn: ({ signal }: { signal: AbortSignal }) => getDiff(input, { signal }),
    retry: (failureCount: number, err: OrchestratorError): boolean => {
      // DIFF_NOT_FOUND — soft-state, ретраить бесполезно (LIC ещё считает).
      // REQUEST_ABORTED — query уничтожен / перемонтирован; retry не нужен.
      if (err instanceof OrchestratorError) {
        if (err.error_code === 'DIFF_NOT_FOUND') return false;
        if (err.error_code === 'REQUEST_ABORTED') return false;
      }
      return failureCount < DEFAULT_RETRIES;
    },
    enabled: opts.enabled ?? true,
  };
  return useQuery<VersionDiffResult, OrchestratorError>(
    opts.staleTime !== undefined
      ? { ...baseOptions, staleTime: opts.staleTime }
      : baseOptions,
  );
}
