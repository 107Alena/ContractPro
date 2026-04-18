// useRecheckVersion — React-хук на базе useMutation (§7.5, §16.2).
//
// Контракт:
//   mutationFn — recheckVersion({contractId, versionId}).
//   onSuccess — инвалидация:
//     - qk.contracts.versions(contractId)    — новая версия появилась в списке;
//     - qk.contracts.status(contractId, vid) — новый processing_status у исходной
//                                              версии (UPLOADED/QUEUED → ...).
//   onError:
//     - REQUEST_ABORTED — фильтруется (user-driven отмена).
//     - 409 VERSION_STILL_PROCESSING → `onError(err, toUserMessage(err))` — toast
//       с title "Версия ещё обрабатывается" + hint "Дождитесь завершения"
//       (ERROR_UX §9.3 / catalog).
//     - Остальные ошибки — тот же onError, page сама решает toast/retry.
//
// AbortController НЕ используется: запрос 202 возвращается быстро, cancel UX
// бесполезный. unmount при in-flight → react-query штатно обработает (mutation
// всё равно не имеет наблюдателей).
import { useMutation, type UseMutationResult, useQueryClient } from '@tanstack/react-query';
import { useCallback, useRef } from 'react';

import {
  type OrchestratorError,
  qk,
  toUserMessage,
  type UserMessage,
} from '@/shared/api';

import { recheckVersion } from '../api/recheck-version';
import type { RecheckVersionInput, RecheckVersionResponse } from './types';

export interface UseRecheckVersionOptions {
  /** Вызывается на 202. */
  onSuccess?: (data: RecheckVersionResponse) => void;
  /**
   * Вызывается на любой ошибке. REQUEST_ABORTED фильтруется.
   * 409 VERSION_STILL_PROCESSING уже покрыт ERROR_UX — page просто показывает toast.
   */
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
}

export interface UseRecheckVersionResult
  extends Omit<
    UseMutationResult<RecheckVersionResponse, OrchestratorError, RecheckVersionInput>,
    'mutate' | 'mutateAsync'
  > {
  recheck: (input: RecheckVersionInput) => void;
  recheckAsync: (input: RecheckVersionInput) => Promise<RecheckVersionResponse>;
}

export function useRecheckVersion(
  opts: UseRecheckVersionOptions = {},
): UseRecheckVersionResult {
  const queryClient = useQueryClient();
  // Live-коллбэки в ref — не пересоздаём mutationFn при смене handler-ссылок.
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const mutation = useMutation<RecheckVersionResponse, OrchestratorError, RecheckVersionInput>({
    mutationFn: (input) => recheckVersion(input),
    onSuccess: (data, input) => {
      void queryClient.invalidateQueries({ queryKey: qk.contracts.versions(input.contractId) });
      void queryClient.invalidateQueries({
        queryKey: qk.contracts.status(input.contractId, input.versionId),
      });
      optsRef.current.onSuccess?.(data);
    },
    onError: (err) => {
      if (err.error_code === 'REQUEST_ABORTED') return;
      optsRef.current.onError?.(err, toUserMessage(err));
    },
  });

  const recheck = useCallback(
    (input: RecheckVersionInput) => mutation.mutate(input),
    [mutation],
  );
  const recheckAsync = useCallback(
    (input: RecheckVersionInput) => mutation.mutateAsync(input),
    [mutation],
  );

  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, recheck, recheckAsync };
}
