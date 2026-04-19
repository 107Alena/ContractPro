// useFeedbackSubmit — React-хук на базе useMutation (§7.5, §16.2).
//
// Контракт:
//   mutationFn — submitFeedback({contractId, versionId, isUseful, comment?}).
//   onSuccess — БЕЗ инвалидации (feedback — write-only эндпоинт, ни одна query
//     его не читает; локальный UI — забота page/widget).
//   onError:
//     - REQUEST_ABORTED — фильтруется (user-driven отмена).
//     - Остальные ошибки → onError(err, toUserMessage(err)) для toast.
import { useMutation, type UseMutationResult } from '@tanstack/react-query';
import { useCallback, useRef } from 'react';

import { type OrchestratorError, toUserMessage, type UserMessage } from '@/shared/api';

import { submitFeedback } from '../api/submit-feedback';
import type { SubmitFeedbackInput, SubmitFeedbackResponse } from './types';

export interface UseFeedbackSubmitOptions {
  /** Вызывается на 201. */
  onSuccess?: (data: SubmitFeedbackResponse) => void;
  /** Вызывается на любой ошибке. REQUEST_ABORTED фильтруется. */
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
}

export interface UseFeedbackSubmitResult extends Omit<
  UseMutationResult<SubmitFeedbackResponse, OrchestratorError, SubmitFeedbackInput>,
  'mutate' | 'mutateAsync'
> {
  submit: (input: SubmitFeedbackInput) => void;
  submitAsync: (input: SubmitFeedbackInput) => Promise<SubmitFeedbackResponse>;
}

export function useFeedbackSubmit(opts: UseFeedbackSubmitOptions = {}): UseFeedbackSubmitResult {
  // Live-коллбэки в ref — не пересоздаём mutationFn при смене handler-ссылок.
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const mutation = useMutation<SubmitFeedbackResponse, OrchestratorError, SubmitFeedbackInput>({
    mutationFn: (input) => submitFeedback(input),
    onSuccess: (data) => {
      optsRef.current.onSuccess?.(data);
    },
    onError: (err) => {
      if (err.error_code === 'REQUEST_ABORTED') return;
      optsRef.current.onError?.(err, toUserMessage(err));
    },
  });

  const submit = useCallback((input: SubmitFeedbackInput) => mutation.mutate(input), [mutation]);
  const submitAsync = useCallback(
    (input: SubmitFeedbackInput) => mutation.mutateAsync(input),
    [mutation],
  );

  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, submit, submitAsync };
}
