// useConfirmType — useMutation на POST /confirm-type.
//
// Контракт:
//   confirm(contractType) — запускает мутацию для current event'а из store.
//   onSuccess: store.resolve() (закрывает модалку, кладёт версию в recent),
//              invalidateQueries qk.contracts.versions(id) — список версий
//              отрисует новый processing_status (ANALYZING) до прихода SSE.
//   onError:
//     - REQUEST_ABORTED → no-op (cancel/unmount).
//     - VERSION_NOT_AWAITING_INPUT (409) → toast.warning + store.dismiss():
//       state протух, модалку убрать.
//     - VALIDATION_ERROR (400 invalid contract_type) → пробрасывается
//       наружу через onError-callback, модалка остаётся открыта.
//     - Прочие → onError-callback, page показывает toast.error.
import { useMutation, type UseMutationResult, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef } from 'react';

import { type OrchestratorError, qk, toUserMessage, type UserMessage } from '@/shared/api';
import { toast as defaultToast } from '@/shared/ui/toast';

import { confirmType, type ConfirmTypeResponse } from '../api/confirm-type';
import {
  mapConfirmTypeError,
  STALE_TOAST_HINT,
  STALE_TOAST_TITLE,
} from '../lib/map-confirm-type-error';
import { useLowConfidenceStore } from './low-confidence-store';
import type { ConfirmTypeInput, TypeConfirmationEvent } from './types';

type ToastApi = typeof defaultToast;

export interface UseConfirmTypeOptions {
  /** Page-level фолбэк для непокрытых кодов (5xx, 401/403/404). */
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
  /** Хук об успешном подтверждении (для analytics/breadcrumb). */
  onSuccess?: (data: ConfirmTypeResponse) => void;
  /** @internal DI для unit-тестов (toast вне jsdom). */
  toast?: ToastApi;
}

export interface UseConfirmTypeResult extends Omit<
  UseMutationResult<ConfirmTypeResponse, OrchestratorError, ConfirmTypeInput>,
  'mutate' | 'mutateAsync'
> {
  /** Запустить confirm. Берёт contract_id/version_id из current event'а в store. */
  confirm: (contractType: string) => void;
  /** Async-вариант для подписки на результат (например, navigate). */
  confirmAsync: (contractType: string) => Promise<ConfirmTypeResponse>;
  /** Отменить in-flight запрос (AbortController). */
  cancel: () => void;
}

export function useConfirmType(opts: UseConfirmTypeOptions = {}): UseConfirmTypeResult {
  const queryClient = useQueryClient();
  const optsRef = useRef(opts);
  optsRef.current = opts;
  const abortRef = useRef<AbortController | null>(null);

  const cancel = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  // Cleanup на unmount — отменяем in-flight, не ломая onError handler.
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const mutation = useMutation<ConfirmTypeResponse, OrchestratorError, ConfirmTypeInput>({
    mutationFn: (input) => {
      const controller = new AbortController();
      abortRef.current = controller;
      return confirmType({ ...input, signal: controller.signal });
    },
    onSuccess: (data) => {
      const toast = optsRef.current.toast ?? defaultToast;
      // Закрываем модалку и кладём версию в recent (60s блокировка повторных
      // type_confirmation_required для этой же версии — backend retry-safe).
      useLowConfidenceStore.getState().resolve();
      // SSE сам пришлёт следующий status_update (ANALYZING), но invalidate
      // ускоряет UI на странице ContractDetailPage до прихода события.
      void queryClient.invalidateQueries({
        queryKey: qk.contracts.versions(data.contractId),
      });
      void queryClient.invalidateQueries({
        queryKey: qk.contracts.status(data.contractId, data.versionId),
      });
      toast.success({ title: 'Тип договора подтверждён' });
      optsRef.current.onSuccess?.(data);
    },
    onError: (err) => {
      const toast = optsRef.current.toast ?? defaultToast;
      const action = mapConfirmTypeError(err);
      if (action.kind === 'aborted') return;
      if (action.kind === 'stale') {
        useLowConfidenceStore.getState().dismiss();
        toast.warning({ title: STALE_TOAST_TITLE, description: STALE_TOAST_HINT });
        return;
      }
      // invalid-type и unknown — пробрасываем page-level callback'у. Модалка
      // остаётся открыта (пользователь может выбрать другой тип).
      optsRef.current.onError?.(err, toUserMessage(err));
    },
  });

  const confirm = useCallback(
    (contractType: string) => {
      const event = useLowConfidenceStore.getState().current;
      if (!event) return; // Модалка закрыта — нечего подтверждать.
      mutation.mutate(makeInput(event, contractType));
    },
    [mutation],
  );

  const confirmAsync = useCallback(
    (contractType: string) => {
      const event = useLowConfidenceStore.getState().current;
      if (!event) {
        return Promise.reject(new Error('No active type-confirmation event.'));
      }
      return mutation.mutateAsync(makeInput(event, contractType));
    },
    [mutation],
  );

  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, confirm, confirmAsync, cancel };
}

function makeInput(event: TypeConfirmationEvent, contractType: string): ConfirmTypeInput {
  return {
    contractId: event.document_id,
    versionId: event.version_id,
    contractType,
  };
}
