// useExportDownload — useMutation-хук (§7.6, §17.5, FE-TASK-039).
//
// Контракт:
//   mutationFn — exportReport({contractId, versionId, format}) → {location}.
//   onSuccess — window.location.assign(location): браузер следует на presigned
//     URL S3, получает Content-Disposition: attachment и инициирует скачивание.
//     Объект навигации инъектируем через DI-параметр `navigate` (default —
//     `(url) => window.location.assign(url)`), чтобы тесты могли подменить
//     без jsdom-патчей глобального location.
//   onError:
//     - REQUEST_ABORTED — фильтруется (user-driven cancel).
//     - Остальные (403 PERMISSION_DENIED, 404, 5xx) → opts.onError(err, userMsg).
//
// RBAC-гейтинг рендерится ВНЕ хука — см. widgets/export-share-modal +
// shared/auth/useCanExport (§5.6). Хук сам по себе не знает о роли.
import { useMutation, type UseMutationResult } from '@tanstack/react-query';
import { useCallback, useRef } from 'react';

import { type OrchestratorError, toUserMessage, type UserMessage } from '@/shared/api';

import { exportReport } from '../api/export-report';
import type { ExportLocation, ExportReportInput } from './types';

/** @internal Инъекция навигации для тестов. В prod — window.location.assign. */
export type NavigateFn = (url: string) => void;

const defaultNavigate: NavigateFn = (url) => {
  // window может отсутствовать в SSR/тестах — на такой случай no-op.
  if (typeof window !== 'undefined') window.location.assign(url);
};

export interface UseExportDownloadOptions {
  /** Вызывается на 302. `data` содержит presigned URL (уже переданный в navigate). */
  onSuccess?: (data: ExportLocation, input: ExportReportInput) => void;
  /** Вызывается при любой ошибке, кроме REQUEST_ABORTED. */
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
  /** @internal DI для тестов. */
  navigate?: NavigateFn;
}

export interface UseExportDownloadResult extends Omit<
  UseMutationResult<ExportLocation, OrchestratorError, ExportReportInput>,
  'mutate' | 'mutateAsync'
> {
  download: (input: ExportReportInput) => void;
  downloadAsync: (input: ExportReportInput) => Promise<ExportLocation>;
}

export function useExportDownload(opts: UseExportDownloadOptions = {}): UseExportDownloadResult {
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const mutation = useMutation<ExportLocation, OrchestratorError, ExportReportInput>({
    mutationFn: (input) => exportReport(input),
    onSuccess: (data, input) => {
      const navigate = optsRef.current.navigate ?? defaultNavigate;
      navigate(data.location);
      optsRef.current.onSuccess?.(data, input);
    },
    onError: (err) => {
      if (err.error_code === 'REQUEST_ABORTED') return;
      optsRef.current.onError?.(err, toUserMessage(err));
    },
  });

  const download = useCallback((input: ExportReportInput) => mutation.mutate(input), [mutation]);
  const downloadAsync = useCallback(
    (input: ExportReportInput) => mutation.mutateAsync(input),
    [mutation],
  );

  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, download, downloadAsync };
}
