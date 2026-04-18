// useUploadVersion — React-хук на базе useMutation (§7.5, §16.2, §20.4a).
//
// Контракт:
//   mutationFn — uploadVersion({contractId, file}, {signal, onUploadProgress});
//   onSuccess — инвалидация:
//     - qk.contracts.versions(contractId) — обязательное, список версий договора;
//     - qk.contracts.byId(contractId)     — обновлённый last_version_*;
//     - ['contracts','list']              — prefix-match для ContractsListPage
//                                           (строка показывает last_version_number/updated_at).
//   onError — двухступенчатый маппинг на форму:
//     1. file-field-ошибки (413/415/400 INVALID_FILE) → setError('file', {...});
//     2. VALIDATION_ERROR с details.fields → applyValidationErrors;
//     3. onError(err, toUserMessage(err)) для toast.
//
// Навигация (на /contracts/{id}/versions/{vid}/result) — ответственность page.
// FSD-слои `features/*` не имеют доступа к роутеру.
//
// AbortController: на unmount / cancel() → abort(). Axios транслирует в
// DOMException AbortError, interceptor нормализует в OrchestratorError с кодом
// REQUEST_ABORTED — фильтруем, чтобы не показывать UX.
import { useMutation, type UseMutationResult, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef } from 'react';

import {
  applyValidationErrors,
  type FieldValuesLike,
  isValidationError,
  type OrchestratorError,
  qk,
  toUserMessage,
  type UseFormSetErrorLike,
  type UserMessage,
} from '@/shared/api';

import { uploadVersion } from '../api/upload-version';
import { isUploadVersionFileFieldError, mapUploadVersionError } from '../lib/map-upload-error';
import type {
  UploadVersionFormValues,
  UploadVersionInput,
  UploadVersionProgress,
  UploadVersionResponse,
} from './types';

export interface UseUploadVersionOptions<
  TForm extends FieldValuesLike = UploadVersionFormValues,
> {
  /** react-hook-form-совместимая функция setError для file-field / VALIDATION_ERROR. */
  setError?: UseFormSetErrorLike<TForm>;
  /** Вызывается на 202. `data` уже narrowed. */
  onSuccess?: (data: UploadVersionResponse) => void;
  /**
   * Вызывается на любой ошибке ПОСЛЕ попытки маппинга в форму.
   * REQUEST_ABORTED фильтруется — пользователь сам отменил, не флудим.
   */
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
  /** Прогресс upload'а в долях от 0 до 1. */
  onUploadProgress?: (progress: UploadVersionProgress) => void;
}

export interface UseUploadVersionResult
  extends Omit<
    UseMutationResult<UploadVersionResponse, OrchestratorError, UploadVersionInput>,
    'mutate' | 'mutateAsync'
  > {
  /** Запустить upload. AbortController создаётся автоматически. */
  upload: (input: UploadVersionInput) => void;
  /** Async-вариант для page, которой нужен await перед navigate. */
  uploadAsync: (input: UploadVersionInput) => Promise<UploadVersionResponse>;
  /** Отменить текущий upload (вызывает AbortController.abort). */
  cancel: () => void;
}

export function useUploadVersion<TForm extends FieldValuesLike = UploadVersionFormValues>(
  opts: UseUploadVersionOptions<TForm> = {},
): UseUploadVersionResult {
  const queryClient = useQueryClient();
  const abortRef = useRef<AbortController | null>(null);
  // Live-коллбэки в ref'е — не пересоздаём mutationFn на каждый рендер.
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const cancel = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  // Отменяем in-flight upload на unmount.
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const mutation = useMutation<UploadVersionResponse, OrchestratorError, UploadVersionInput>({
    mutationFn: (input) => {
      const controller = new AbortController();
      abortRef.current = controller;
      return uploadVersion(input, {
        signal: controller.signal,
        onUploadProgress: (p) => optsRef.current.onUploadProgress?.(p),
      });
    },
    onSuccess: (data, input) => {
      // 1. Список версий договора — основной consumer (VersionsList, Detail).
      void queryClient.invalidateQueries({ queryKey: qk.contracts.versions(input.contractId) });
      // 2. Карточка договора — last_version_id/updated_at меняются.
      void queryClient.invalidateQueries({ queryKey: qk.contracts.byId(input.contractId) });
      // 3. Списки договоров — строка в ContractsListPage показывает last_version_number.
      void queryClient.invalidateQueries({ queryKey: ['contracts', 'list'] });
      optsRef.current.onSuccess?.(data);
    },
    onError: (err) => {
      const { setError, onError } = optsRef.current;
      if (err.error_code === 'REQUEST_ABORTED') return;

      // 1. File-field коды → setError('file', ...).
      if (setError && isUploadVersionFileFieldError(err)) {
        const mapped = mapUploadVersionError(err);
        if (mapped) {
          try {
            setError(
              mapped.field as keyof TForm & string,
              { type: mapped.code, message: mapped.message },
              { shouldFocus: true },
            );
          } catch {
            // Форма не имеет поля `file` — fallback на toast через onError ниже.
          }
        }
      }

      // 2. VALIDATION_ERROR с details.fields → маппер по полям формы.
      if (setError && isValidationError(err)) {
        applyValidationErrors<TForm>(err, setError);
      }

      // 3. Page-level callback для toast.
      onError?.(err, toUserMessage(err));
    },
  });

  const upload = useCallback(
    (input: UploadVersionInput) => mutation.mutate(input),
    [mutation],
  );
  const uploadAsync = useCallback(
    (input: UploadVersionInput) => mutation.mutateAsync(input),
    [mutation],
  );

  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, upload, uploadAsync, cancel };
}
