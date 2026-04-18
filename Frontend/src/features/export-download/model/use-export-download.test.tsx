// @vitest-environment jsdom
//
// Хук-тест useExportDownload: 302 → navigate(location), onSuccess/onError,
// REQUEST_ABORTED фильтруется, DI navigate.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance, AxiosRequestConfig } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { __setHttpForTests } from '../api/http';
import { useExportDownload } from './use-export-download';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const PRESIGNED = 'https://presigned.example/contractpro/report.pdf?X-Expires=300';

const INPUT = {
  contractId: CONTRACT_ID,
  versionId: VERSION_ID,
  format: 'pdf' as const,
};

function makeWrapper(): (props: { children: ReactNode }) => JSX.Element {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return function Wrapper({ children }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function orch(code: string, status?: number, message: string = ''): OrchestratorError {
  return new OrchestratorError(
    status !== undefined ? { error_code: code, message, status } : { error_code: code, message },
  );
}

let getSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  getSpy = vi.fn();
  __setHttpForTests({ get: getSpy } as unknown as AxiosInstance);
});

afterEach(() => {
  __setHttpForTests(null);
});

describe('useExportDownload — success', () => {
  it('302 → navigate(location) вызван с presigned URL', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: { location: PRESIGNED } });
    const navigate = vi.fn();
    const onSuccess = vi.fn();
    const { result } = renderHook(() => useExportDownload({ navigate, onSuccess }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.download(INPUT);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(navigate).toHaveBeenCalledWith(PRESIGNED);
    expect(onSuccess).toHaveBeenCalledWith(expect.objectContaining({ location: PRESIGNED }), INPUT);
  });

  it('downloadAsync резолвит промис с {location}', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: { location: PRESIGNED } });
    const navigate = vi.fn();
    const { result } = renderHook(() => useExportDownload({ navigate }), {
      wrapper: makeWrapper(),
    });

    const promise = result.current.downloadAsync(INPUT);
    await expect(promise).resolves.toMatchObject({ location: PRESIGNED });
    expect(navigate).toHaveBeenCalledWith(PRESIGNED);
  });

  it('передаёт signal=undefined в config (без AbortController)', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: { location: PRESIGNED } });
    const { result } = renderHook(() => useExportDownload({ navigate: () => {} }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.download(INPUT);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const [, config] = getSpy.mock.calls[0]! as [string, AxiosRequestConfig];
    expect('signal' in (config ?? {})).toBe(false);
  });
});

describe('useExportDownload — error handling', () => {
  it('403 PERMISSION_DENIED → onError c catalog-сообщением', async () => {
    getSpy.mockRejectedValueOnce(orch('PERMISSION_DENIED', 403));
    const navigate = vi.fn();
    const onError = vi.fn();
    const { result } = renderHook(() => useExportDownload({ navigate, onError }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.download(INPUT);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(navigate).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledTimes(1);
    const [err, userMessage] = onError.mock.calls[0]!;
    expect(err).toMatchObject({ error_code: 'PERMISSION_DENIED', status: 403 });
    // Пустой server-message → fallback на ERORR_UX title "У вас нет прав …".
    expect(userMessage).toMatchObject({
      title: expect.stringContaining('прав'),
    });
  });

  it('404 ARTIFACT_NOT_FOUND → onError c title «Результаты ещё не готовы»', async () => {
    getSpy.mockRejectedValueOnce(orch('ARTIFACT_NOT_FOUND', 404));
    const onError = vi.fn();
    const { result } = renderHook(() => useExportDownload({ onError, navigate: () => {} }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.download(INPUT);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'ARTIFACT_NOT_FOUND' }),
      expect.objectContaining({ title: expect.any(String) }),
    );
  });

  it('REQUEST_ABORTED → onError НЕ вызван', async () => {
    getSpy.mockRejectedValueOnce(orch('REQUEST_ABORTED'));
    const onError = vi.fn();
    const { result } = renderHook(() => useExportDownload({ onError, navigate: () => {} }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.download(INPUT);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(onError).not.toHaveBeenCalled();
  });

  it('onError НЕ вызван на success', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: { location: PRESIGNED } });
    const onError = vi.fn();
    const { result } = renderHook(() => useExportDownload({ navigate: () => {}, onError }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.download(INPUT);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(onError).not.toHaveBeenCalled();
  });
});

describe('useExportDownload — default navigate', () => {
  it('без navigate — не падает, мутация завершается success', async () => {
    // jsdom 24: `spyOn(window.location, 'assign')` / прямой redefine свойства
    // выдают «Cannot redefine property» либо «Not implemented: navigation».
    // Перекрываем весь location через defineProperty на window, чтобы default
    // navigate мог вызвать assign без реальной навигации.
    const assignSpy = vi.fn();
    const fakeLocation = { assign: assignSpy } as unknown as Location;
    const originalLocation = window.location;
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: fakeLocation,
    });

    try {
      getSpy.mockResolvedValueOnce({ status: 302, headers: { location: PRESIGNED } });
      const { result } = renderHook(() => useExportDownload(), {
        wrapper: makeWrapper(),
      });

      act(() => {
        result.current.download(INPUT);
      });
      await waitFor(() => expect(result.current.isSuccess).toBe(true));
      expect(assignSpy).toHaveBeenCalledWith(PRESIGNED);
    } finally {
      Object.defineProperty(window, 'location', {
        configurable: true,
        writable: true,
        value: originalLocation,
      });
    }
  });
});
