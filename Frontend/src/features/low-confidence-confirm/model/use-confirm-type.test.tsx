// @vitest-environment jsdom
//
// Unit-тесты useConfirmType: TanStack mutation, маппинг ошибок (409→stale,
// VALIDATION_ERROR→passthrough, REQUEST_ABORTED→silent), invalidateQueries
// + store.resolve/dismiss. HTTP мокается через __setHttpForTests (без MSW).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError, qk } from '@/shared/api';
import { toast as toastApi } from '@/shared/ui/toast';

import { __setHttpForTests } from '../api/http';
import { useLowConfidenceStore } from './low-confidence-store';
import type { TypeConfirmationEvent } from './types';
import { useConfirmType } from './use-confirm-type';

type ToastApi = typeof toastApi;
type ToastMock = {
  [K in keyof ToastApi]: ReturnType<typeof vi.fn<Parameters<ToastApi[K]>, ReturnType<ToastApi[K]>>>;
};

function makeToast(): ToastMock {
  return {
    success: vi.fn<Parameters<ToastApi['success']>, string>(() => ''),
    error: vi.fn<Parameters<ToastApi['error']>, string>(() => ''),
    warning: vi.fn<Parameters<ToastApi['warning']>, string>(() => ''),
    warn: vi.fn<Parameters<ToastApi['warn']>, string>(() => ''),
    info: vi.fn<Parameters<ToastApi['info']>, string>(() => ''),
    sticky: vi.fn<Parameters<ToastApi['sticky']>, string>(() => ''),
    dismiss: vi.fn<Parameters<ToastApi['dismiss']>, void>(),
    clear: vi.fn<Parameters<ToastApi['clear']>, void>(),
  };
}

const OK_RESPONSE = {
  contract_id: 'doc-1',
  version_id: 'ver-1',
  version_number: 2,
  job_id: 'job-1',
  status: 'ANALYZING' as const,
};

function makeEvent(overrides: Partial<TypeConfirmationEvent> = {}): TypeConfirmationEvent {
  return {
    document_id: 'doc-1',
    version_id: 'ver-1',
    status: 'AWAITING_USER_INPUT',
    suggested_type: 'услуги',
    confidence: 0.62,
    threshold: 0.75,
    alternatives: [{ contract_type: 'подряд', confidence: 0.21 }],
    ...overrides,
  };
}

function err(code: string, message = 'msg', status = 409): OrchestratorError {
  return new OrchestratorError({ error_code: code, message, status });
}

function makeWrapper(): {
  wrapper: (props: { children: ReactNode }) => JSX.Element;
  qc: QueryClient;
} {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  const wrapper = ({ children }: { children: ReactNode }): JSX.Element => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return { wrapper, qc };
}

let postSpy: ReturnType<typeof vi.fn>;
let toast: ToastMock;

beforeEach(() => {
  postSpy = vi.fn();
  toast = makeToast();
  __setHttpForTests({ post: postSpy } as unknown as AxiosInstance);
  useLowConfidenceStore.getState().__reset();
});

afterEach(() => {
  __setHttpForTests(null);
  useLowConfidenceStore.getState().__reset();
});

describe('useConfirmType — confirm without active event', () => {
  it('confirm() при пустом store → mutation НЕ запускается', () => {
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useConfirmType({ toast }), { wrapper });

    act(() => result.current.confirm('услуги'));

    expect(postSpy).not.toHaveBeenCalled();
    expect(result.current.isPending).toBe(false);
  });

  it('confirmAsync() при пустом store → reject', async () => {
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useConfirmType({ toast }), { wrapper });

    await expect(result.current.confirmAsync('услуги')).rejects.toThrow(
      'No active type-confirmation event',
    );
  });
});

describe('useConfirmType — happy path', () => {
  it('202 → resolve store + invalidate qk.contracts.versions/status + toast.success', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    useLowConfidenceStore.getState().open(makeEvent());
    const { wrapper, qc } = makeWrapper();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');

    const { result } = renderHook(() => useConfirmType({ toast }), { wrapper });

    act(() => result.current.confirm('услуги'));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(useLowConfidenceStore.getState().current).toBeNull();
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: qk.contracts.versions('doc-1') });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: qk.contracts.status('doc-1', 'ver-1'),
    });
    expect(toast.success).toHaveBeenCalledWith({ title: 'Тип договора подтверждён' });
  });

  it('confirm передаёт правильные поля в POST: contract_type из аргумента + IDs из event', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    useLowConfidenceStore.getState().open(
      makeEvent({
        document_id: 'doc-X',
        version_id: 'ver-X',
      }),
    );
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useConfirmType({ toast }), { wrapper });

    act(() => result.current.confirm('подряд'));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const [path, body] = postSpy.mock.calls[0]!;
    expect(path).toBe('/contracts/doc-X/versions/ver-X/confirm-type');
    expect(body).toEqual({ contract_type: 'подряд', confirmed_by_user: true });
  });
});

describe('useConfirmType — error handling', () => {
  it('VERSION_NOT_AWAITING_INPUT (409) → store.dismiss + toast.warning + onError НЕ вызван', async () => {
    postSpy.mockRejectedValueOnce(err('VERSION_NOT_AWAITING_INPUT', 'too late', 409));
    useLowConfidenceStore.getState().open(makeEvent());
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useConfirmType({ toast, onError }), { wrapper });

    act(() => result.current.confirm('услуги'));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(useLowConfidenceStore.getState().current).toBeNull();
    expect(toast.warning).toHaveBeenCalledWith(
      expect.objectContaining({ title: expect.stringContaining('Подтверждение') }),
    );
    expect(onError).not.toHaveBeenCalled();
  });

  it('VALIDATION_ERROR → onError вызван, current event ОСТАЁТСЯ (модалка открыта)', async () => {
    postSpy.mockRejectedValueOnce(err('VALIDATION_ERROR', 'не whitelist', 400));
    const event = makeEvent();
    useLowConfidenceStore.getState().open(event);
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useConfirmType({ toast, onError }), { wrapper });

    act(() => result.current.confirm('левый-тип'));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(useLowConfidenceStore.getState().current).toBe(event);
    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'VALIDATION_ERROR' }),
      expect.objectContaining({ title: 'не whitelist' }),
    );
    expect(toast.warning).not.toHaveBeenCalled();
  });

  it('500 INTERNAL_ERROR → onError + current ОСТАЁТСЯ', async () => {
    postSpy.mockRejectedValueOnce(err('INTERNAL_ERROR', 'Ошибка', 500));
    const event = makeEvent();
    useLowConfidenceStore.getState().open(event);
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useConfirmType({ toast, onError }), { wrapper });

    act(() => result.current.confirm('услуги'));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(useLowConfidenceStore.getState().current).toBe(event);
    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'INTERNAL_ERROR' }),
      expect.anything(),
    );
  });

  it('REQUEST_ABORTED → toast.warning НЕ вызван, onError НЕ вызван, store не трогается', async () => {
    postSpy.mockRejectedValueOnce(err('REQUEST_ABORTED', 'cancelled'));
    const event = makeEvent();
    useLowConfidenceStore.getState().open(event);
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useConfirmType({ toast, onError }), { wrapper });

    act(() => result.current.confirm('услуги'));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(useLowConfidenceStore.getState().current).toBe(event);
    expect(toast.warning).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  });
});

describe('useConfirmType — cancel/unmount', () => {
  it('cancel() абортит текущий request', async () => {
    let capturedSignal: AbortSignal | undefined;
    postSpy.mockImplementationOnce(async (_url, _body, config) => {
      capturedSignal = config.signal as AbortSignal | undefined;
      await new Promise<never>((_resolve, reject) => {
        capturedSignal?.addEventListener('abort', () => {
          reject(new OrchestratorError({ error_code: 'REQUEST_ABORTED', message: 'aborted' }));
        });
      });
      return { data: OK_RESPONSE };
    });
    useLowConfidenceStore.getState().open(makeEvent());
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useConfirmType({ toast }), { wrapper });

    act(() => result.current.confirm('услуги'));
    await waitFor(() => expect(capturedSignal).toBeDefined());
    act(() => result.current.cancel());

    expect(capturedSignal?.aborted).toBe(true);
  });

  it('unmount → AbortController.abort()', async () => {
    let capturedSignal: AbortSignal | undefined;
    postSpy.mockImplementationOnce(async (_url, _body, config) => {
      capturedSignal = config.signal as AbortSignal | undefined;
      await new Promise<never>((_resolve, reject) => {
        capturedSignal?.addEventListener('abort', () => {
          reject(new OrchestratorError({ error_code: 'REQUEST_ABORTED', message: 'aborted' }));
        });
      });
      return { data: OK_RESPONSE };
    });
    useLowConfidenceStore.getState().open(makeEvent());
    const { wrapper } = makeWrapper();
    const { result, unmount } = renderHook(() => useConfirmType({ toast }), { wrapper });

    act(() => result.current.confirm('услуги'));
    await waitFor(() => expect(capturedSignal).toBeDefined());
    unmount();

    expect(capturedSignal?.aborted).toBe(true);
  });
});
