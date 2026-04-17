// useUploadContract — React-хук на базе useMutation (§7.5, §16.2, §20.4a).
//
// Контракт:
//   mutationFn — uploadContract({file, title}, {signal, onUploadProgress});
//   onSuccess — инвалидация qk.contracts.all + вызов opts.onSuccess(data);
//   onError — двухступенчатый маппинг на форму:
//     1. file-field-ошибки (413 FILE_TOO_LARGE / 415 UNSUPPORTED_FORMAT /
//        400 INVALID_FILE) → setError('file', {...}) напрямую (§9.3 row 413/415);
//     2. VALIDATION_ERROR с details.fields → applyValidationErrors (§20.4a);
//     3. Вызов opts.onError(err, toUserMessage(err)) для логгинга/toast'а.
//
// Навигация (/contracts/{id}/versions/{vid}/result) — ответственность page,
// не feature: FSD-слои `features/*` не имеют доступа к роутеру. Page передаёт
// `onSuccess`-колбэк, который делает `navigate(...)` и бутстрапит SSE (§16.2).
//
// AbortController: на unmount вызываем abort(), чтобы отменить in-flight upload
// (axios транслирует в DOMException 'AbortError', interceptor нормализует в
// OrchestratorError с кодом REQUEST_ABORTED — пропускаем его в onError без
// показа toast).
import { useMutation, type UseMutationResult, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef } from 'react';

import {
  applyValidationErrors,
  type FieldValuesLike,
  isValidationError,
  type OrchestratorError,
  toUserMessage,
  type UseFormSetErrorLike,
  type UserMessage,
} from '@/shared/api';

import { uploadContract } from '../api/upload-contract';
import { isFileFieldError, mapUploadFileError } from '../lib/map-upload-error';
import type {
  UploadContractInput,
  UploadContractResponse,
  UploadFormValues,
  UploadProgress,
} from './types';

export interface UseUploadContractOptions<
  TForm extends FieldValuesLike = UploadFormValues,
> {
  /**
   * react-hook-form-совместимая функция setError. Если передана, хук сам
   * проставит inline-ошибки для file-field-кодов и VALIDATION_ERROR.
   */
  setError?: UseFormSetErrorLike<TForm>;
  /** Вызывается на 202. `data` уже narrowed (contractId/versionId — string). */
  onSuccess?: (data: UploadContractResponse) => void;
  /**
   * Вызывается на любой ошибке ПОСЛЕ попытки маппинга в форму. Page обычно
   * использует для toast. `REQUEST_ABORTED` пропускается фильтром и не
   * доходит до onError (пользователь сам отменил — не флудим).
   */
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
  /** Прогресс upload'а в долях от 0 до 1 (см. axios onUploadProgress §7.5). */
  onUploadProgress?: (progress: UploadProgress) => void;
}

export interface UseUploadContractResult
  extends Omit<
    UseMutationResult<UploadContractResponse, OrchestratorError, UploadContractInput>,
    'mutate' | 'mutateAsync'
  > {
  /** Запустить upload. AbortController создаётся автоматически. */
  upload: (input: UploadContractInput) => void;
  /** Async-вариант для page, которой нужен await перед navigate. */
  uploadAsync: (input: UploadContractInput) => Promise<UploadContractResponse>;
  /** Отменить текущий upload (вызывает AbortController.abort). */
  cancel: () => void;
}

export function useUploadContract<TForm extends FieldValuesLike = UploadFormValues>(
  opts: UseUploadContractOptions<TForm> = {},
): UseUploadContractResult {
  const queryClient = useQueryClient();
  const abortRef = useRef<AbortController | null>(null);
  // Храним live-коллбэки в ref'е, чтобы не пересоздавать mutationFn на каждый
  // рендер из-за смены handler-ссылок (частый баг с inline-стрелками).
  const optsRef = useRef(opts);
  optsRef.current = opts;

  // Не обнуляем abortRef в onSuccess/onError/cancel — это создавало гонку,
  // когда пользователь кликнул cancel() и СРАЗУ upload(input2): onError старой
  // мутации затирал valid controller новой. abort() на завершённом/уже
  // абортнутом controller'е — no-op, так что держать «устаревший» controller
  // в ref безопасно; перезапись произойдёт в mutationFn следующего upload'а.
  const cancel = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  // Отменяем in-flight upload на unmount (cleanup). mutation.reset не вызываем —
  // это создало бы race с ещё-не-отработавшим onError handler'ом.
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const mutation = useMutation<UploadContractResponse, OrchestratorError, UploadContractInput>({
    mutationFn: (input) => {
      const controller = new AbortController();
      abortRef.current = controller;
      return uploadContract(input, {
        signal: controller.signal,
        onUploadProgress: (p) => optsRef.current.onUploadProgress?.(p),
      });
    },
    onSuccess: (data) => {
      // Prefix-match: инвалидирует только ['contracts','list',...] — списки в
      // ContractsListPage. `qk.contracts.byId`/`versions` чужих контрактов
      // трогать не нужно (ревью M1).
      void queryClient.invalidateQueries({ queryKey: ['contracts', 'list'] });
      optsRef.current.onSuccess?.(data);
    },
    onError: (err) => {
      const { setError, onError } = optsRef.current;
      // REQUEST_ABORTED — user-driven отмена (unmount/cancel): не показываем UX.
      if (err.error_code === 'REQUEST_ABORTED') return;

      // 1. File-field коды (413/415/400 INVALID_FILE) → setError('file', ...).
      if (setError && isFileFieldError(err)) {
        const mapped = mapUploadFileError(err);
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

      // 3. Page-level callback для toast / sentry-breadcrumb.
      onError?.(err, toUserMessage(err));
    },
  });

  const upload = useCallback(
    (input: UploadContractInput) => mutation.mutate(input),
    [mutation],
  );
  const uploadAsync = useCallback(
    (input: UploadContractInput) => mutation.mutateAsync(input),
    [mutation],
  );

  // Собираем возвращаемый объект без `mutate`/`mutateAsync` — публичный API
  // feature ограничен `upload`/`uploadAsync` (единообразно с cancel).
  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, upload, uploadAsync, cancel };
}
