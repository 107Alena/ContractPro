// useStartComparison — React-хук на базе useMutation (§7.5, §16.2).
//
// Контракт:
//   mutationFn — startComparison({contractId, baseVersionId, targetVersionId}).
//   onSuccess — инвалидация:
//     - qk.contracts.diff(id, base, target) — старый 404 сбрасывается; следующий
//       fetch может вернуть 404 повторно (работа ещё не закончена), реальный
//       completed придёт через SSE COMPARISON_COMPLETED (useEventStream
//       инвалидирует ту же key повторно).
//   onError:
//     - REQUEST_ABORTED — фильтруется (user-driven отмена).
//     - 409 VERSION_STILL_PROCESSING → onError(err, toUserMessage(err)) — toast
//       с title "Версия ещё обрабатывается" + hint "Дождитесь завершения"
//       (ERROR_UX §9.3 / catalog).
//     - Остальные коды (VERSION_NOT_FOUND, PERMISSION_DENIED, INTERNAL_ERROR) —
//       тот же onError, page сама решает toast/retry.
//
// AbortController НЕ используется: 202 возвращается быстро, cancel UX бесполезный.
import { useMutation, type UseMutationResult, useQueryClient } from '@tanstack/react-query';
import { useCallback, useRef } from 'react';

import {
  type OrchestratorError,
  qk,
  toUserMessage,
  type UserMessage,
} from '@/shared/api';

import { startComparison } from '../api/start-comparison';
import type { StartComparisonInput, StartComparisonResponse } from './types';

export interface UseStartComparisonOptions {
  /** Вызывается на 202. `data` — narrowed {jobId, status}. */
  onSuccess?: (data: StartComparisonResponse) => void;
  /**
   * Вызывается на любой ошибке. REQUEST_ABORTED фильтруется.
   * 409 VERSION_STILL_PROCESSING уже покрыт ERROR_UX — page просто показывает toast.
   */
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
}

export interface UseStartComparisonResult
  extends Omit<
    UseMutationResult<StartComparisonResponse, OrchestratorError, StartComparisonInput>,
    'mutate' | 'mutateAsync'
  > {
  startComparison: (input: StartComparisonInput) => void;
  startComparisonAsync: (input: StartComparisonInput) => Promise<StartComparisonResponse>;
}

export function useStartComparison(
  opts: UseStartComparisonOptions = {},
): UseStartComparisonResult {
  const queryClient = useQueryClient();
  // Live-коллбэки в ref — не пересоздаём mutationFn при смене handler-ссылок.
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const mutation = useMutation<
    StartComparisonResponse,
    OrchestratorError,
    StartComparisonInput
  >({
    mutationFn: (input) => startComparison(input),
    onSuccess: (data, input) => {
      void queryClient.invalidateQueries({
        queryKey: qk.contracts.diff(input.contractId, input.baseVersionId, input.targetVersionId),
      });
      optsRef.current.onSuccess?.(data);
    },
    onError: (err) => {
      if (err.error_code === 'REQUEST_ABORTED') return;
      optsRef.current.onError?.(err, toUserMessage(err));
    },
  });

  const startComparisonFn = useCallback(
    (input: StartComparisonInput) => mutation.mutate(input),
    [mutation],
  );
  const startComparisonAsync = useCallback(
    (input: StartComparisonInput) => mutation.mutateAsync(input),
    [mutation],
  );

  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, startComparison: startComparisonFn, startComparisonAsync };
}
