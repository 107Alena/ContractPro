// useShareLink — useMutation-хук для share-link (§7.6, UR-10, FE-TASK-039).
//
// Контракт:
//   mutationFn — getShareLink(input) → {location}.
//   onSuccess — copy(location) через shared/lib useCopy; флаг `copied`
//     автоматически сбрасывается через 1500мс. opts.onSuccess получает
//     `{ location, copied }` для дополнительного UX (toast).
//   onError:
//     - REQUEST_ABORTED — фильтруется.
//     - 403/404/5xx → opts.onError(err, userMessage) — consumer показывает toast.
//
// Хук не содержит RBAC-гейтинга — см. widgets/export-share-modal + useCanExport.
import { useMutation, type UseMutationResult } from '@tanstack/react-query';
import { useCallback, useRef } from 'react';

import { type OrchestratorError, toUserMessage, type UserMessage } from '@/shared/api';
import { useCopy } from '@/shared/lib/use-copy';

import { getShareLink } from '../api/get-share-link';
import type { ShareLinkInput, ShareLinkResult } from './types';

export interface UseShareLinkOptions {
  /** Вызывается ПОСЛЕ успешного копирования. `copied=false` если clipboard отклонил. */
  onSuccess?: (data: ShareLinkResult, meta: { input: ShareLinkInput; copied: boolean }) => void;
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
}

export interface UseShareLinkResult extends Omit<
  UseMutationResult<ShareLinkResult, OrchestratorError, ShareLinkInput>,
  'mutate' | 'mutateAsync'
> {
  share: (input: ShareLinkInput) => void;
  shareAsync: (input: ShareLinkInput) => Promise<ShareLinkResult>;
  /** true в течение ~1500мс после успешного копирования. */
  copied: boolean;
}

export function useShareLink(opts: UseShareLinkOptions = {}): UseShareLinkResult {
  const { copy, copied } = useCopy();
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const mutation = useMutation<ShareLinkResult, OrchestratorError, ShareLinkInput>({
    mutationFn: (input) => getShareLink(input),
    onSuccess: async (data, input) => {
      const ok = await copy(data.location);
      optsRef.current.onSuccess?.(data, { input, copied: ok });
    },
    onError: (err) => {
      if (err.error_code === 'REQUEST_ABORTED') return;
      optsRef.current.onError?.(err, toUserMessage(err));
    },
  });

  const share = useCallback((input: ShareLinkInput) => mutation.mutate(input), [mutation]);
  const shareAsync = useCallback(
    (input: ShareLinkInput) => mutation.mutateAsync(input),
    [mutation],
  );

  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, share, shareAsync, copied };
}
