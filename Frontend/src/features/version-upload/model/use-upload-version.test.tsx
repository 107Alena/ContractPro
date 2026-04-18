// @vitest-environment jsdom
//
// Хук-тест useUploadVersion: React-слой (useMutation, callback-порядок,
// инвалидация трёх query-keys, AbortController). HTTP подменяется через
// __setHttpForTests — MSW не нужен для логики хука.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { __setHttpForTests } from '../api/http';
import { useUploadVersion } from './use-upload-version';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  version_id: 'v2ee0000-aaaa-bbbb-cccc-dddddddddddd',
  version_number: 2,
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'UPLOADED' as const,
};

function makeFile(): File {
  return new File([new Uint8Array([0x25, 0x50, 0x44, 0x46])], 'v2.pdf', {
    type: 'application/pdf',
  });
}

function orch(code: string, message = 'msg', status?: number): OrchestratorError {
  return new OrchestratorError(
    status !== undefined
      ? { error_code: code, message, status }
      : { error_code: code, message },
  );
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

beforeEach(() => {
  postSpy = vi.fn();
  __setHttpForTests({ post: postSpy } as unknown as AxiosInstance);
});

afterEach(() => {
  __setHttpForTests(null);
});

describe('useUploadVersion — success + invalidation', () => {
  it('202 → onSuccess с narrowed-response, три query-keys инвалидированы', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const onSuccess = vi.fn();
    const { wrapper, qc } = makeWrapper();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');

    const { result } = renderHook(() => useUploadVersion({ onSuccess }), { wrapper });

    act(() => {
      result.current.upload({ contractId: CONTRACT_ID, file: makeFile() });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onSuccess).toHaveBeenCalledWith(
      expect.objectContaining({
        contractId: CONTRACT_ID,
        versionId: OK_RESPONSE.version_id,
        versionNumber: 2,
      }),
    );

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) =>
      (c[0] as { queryKey: unknown[] }).queryKey,
    );
    expect(invalidatedKeys).toEqual(
      expect.arrayContaining([
        ['contracts', CONTRACT_ID, 'versions'],
        ['contracts', CONTRACT_ID],
        ['contracts', 'list'],
      ]),
    );
  });

  it('onUploadProgress прокидывается', async () => {
    postSpy.mockImplementationOnce(async (_url, _fd, config) => {
      config.onUploadProgress?.({ loaded: 1, total: 2 });
      return { data: OK_RESPONSE };
    });
    const onUploadProgress = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useUploadVersion({ onUploadProgress }), { wrapper });

    act(() => {
      result.current.upload({ contractId: CONTRACT_ID, file: makeFile() });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onUploadProgress).toHaveBeenCalledWith({ loaded: 1, total: 2, fraction: 0.5 });
  });
});

describe('useUploadVersion — file-field errors', () => {
  it.each([
    ['FILE_TOO_LARGE', 413, 'Файл больше 20 МБ'],
    ['UNSUPPORTED_FORMAT', 415, 'Поддерживается только PDF'],
    ['INVALID_FILE', 400, 'Файл повреждён'],
  ])('%s → setError("file", {type, message}) + onError', async (code, status, msg) => {
    postSpy.mockRejectedValueOnce(orch(code, msg, status));
    const setError = vi.fn();
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useUploadVersion({ setError, onError }), { wrapper });

    act(() => {
      result.current.upload({ contractId: CONTRACT_ID, file: makeFile() });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(setError).toHaveBeenCalledWith(
      'file',
      { type: code, message: msg },
      { shouldFocus: true },
    );
    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: code }),
      expect.objectContaining({ title: msg }),
    );
  });

  it('без setError — file-field-коды НЕ бросают, onError получает ошибку', async () => {
    postSpy.mockRejectedValueOnce(orch('FILE_TOO_LARGE', 'big', 413));
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useUploadVersion({ onError }), { wrapper });

    act(() => {
      result.current.upload({ contractId: CONTRACT_ID, file: makeFile() });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'FILE_TOO_LARGE' }),
      expect.anything(),
    );
  });
});

describe('useUploadVersion — VALIDATION_ERROR', () => {
  it('fields из details.fields → applyValidationErrors', async () => {
    postSpy.mockRejectedValueOnce(
      new OrchestratorError({
        error_code: 'VALIDATION_ERROR',
        message: 'Проверьте введённые данные',
        status: 400,
        details: {
          fields: [{ field: 'file', code: 'REQUIRED', message: 'Выберите файл' }],
        } as unknown as OrchestratorError['details'],
      }),
    );
    const setError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useUploadVersion({ setError }), { wrapper });

    act(() => {
      result.current.upload({ contractId: CONTRACT_ID, file: makeFile() });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(setError).toHaveBeenCalledWith(
      'file',
      { type: 'REQUIRED', message: 'Выберите файл' },
      { shouldFocus: true },
    );
  });
});

describe('useUploadVersion — passthrough / abort', () => {
  it('500 INTERNAL_ERROR → setError НЕ вызван, onError с toUserMessage', async () => {
    postSpy.mockRejectedValueOnce(orch('INTERNAL_ERROR', 'Ошибка на сервере', 500));
    const setError = vi.fn();
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useUploadVersion({ setError, onError }), { wrapper });

    act(() => {
      result.current.upload({ contractId: CONTRACT_ID, file: makeFile() });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(setError).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'INTERNAL_ERROR' }),
      expect.objectContaining({ title: 'Ошибка на сервере' }),
    );
  });

  it('REQUEST_ABORTED → ни setError, ни onError не вызваны', async () => {
    postSpy.mockRejectedValueOnce(orch('REQUEST_ABORTED', 'Запрос отменён'));
    const setError = vi.fn();
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useUploadVersion({ setError, onError }), { wrapper });

    act(() => {
      result.current.upload({ contractId: CONTRACT_ID, file: makeFile() });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(setError).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  });

  it('cancel() до завершения → AbortController.abort()', async () => {
    let capturedSignal: AbortSignal | undefined;
    postSpy.mockImplementationOnce(async (_url, _fd, config) => {
      capturedSignal = config.signal as AbortSignal | undefined;
      await new Promise<never>((_resolve, reject) => {
        capturedSignal?.addEventListener('abort', () => {
          reject(new OrchestratorError({ error_code: 'REQUEST_ABORTED', message: 'aborted' }));
        });
      });
      return { data: OK_RESPONSE };
    });

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useUploadVersion(), { wrapper });

    act(() => {
      result.current.upload({ contractId: CONTRACT_ID, file: makeFile() });
    });
    await waitFor(() => expect(capturedSignal).toBeDefined());
    act(() => result.current.cancel());

    expect(capturedSignal?.aborted).toBe(true);
  });

  it('unmount → AbortController.abort()', async () => {
    let capturedSignal: AbortSignal | undefined;
    postSpy.mockImplementationOnce(async (_url, _fd, config) => {
      capturedSignal = config.signal as AbortSignal | undefined;
      await new Promise<never>((_resolve, reject) => {
        capturedSignal?.addEventListener('abort', () => {
          reject(new OrchestratorError({ error_code: 'REQUEST_ABORTED', message: 'aborted' }));
        });
      });
      return { data: OK_RESPONSE };
    });

    const { wrapper } = makeWrapper();
    const { result, unmount } = renderHook(() => useUploadVersion(), { wrapper });

    act(() => {
      result.current.upload({ contractId: CONTRACT_ID, file: makeFile() });
    });
    await waitFor(() => expect(capturedSignal).toBeDefined());
    unmount();

    expect(capturedSignal?.aborted).toBe(true);
  });
});
