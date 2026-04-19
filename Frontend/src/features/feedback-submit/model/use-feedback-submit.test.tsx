// @vitest-environment jsdom
//
// Хук-тест useFeedbackSubmit: React-слой (useMutation без инвалидации,
// REQUEST_ABORTED фильтр, toUserMessage на error).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { __setHttpForTests } from '../api/http';
import { useFeedbackSubmit } from './use-feedback-submit';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd';
const FEEDBACK_ID = 'fb000000-5555-6666-7777-888888888888';
const CREATED_AT = '2026-04-19T10:00:00Z';

const OK_RESPONSE = {
  feedback_id: FEEDBACK_ID,
  created_at: CREATED_AT,
};

function orch(code: string, message = 'msg', status?: number): OrchestratorError {
  return new OrchestratorError(
    status !== undefined ? { error_code: code, message, status } : { error_code: code, message },
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

describe('useFeedbackSubmit — success', () => {
  it('201 → onSuccess с narrowed-response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const onSuccess = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useFeedbackSubmit({ onSuccess }), { wrapper });

    act(() => {
      result.current.submit({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        isUseful: true,
        comment: 'ок',
      });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onSuccess).toHaveBeenCalledWith({
      feedbackId: FEEDBACK_ID,
      createdAt: CREATED_AT,
    });
  });

  it('submitAsync резолвит промис с response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useFeedbackSubmit(), { wrapper });

    const promise = result.current.submitAsync({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      isUseful: true,
    });
    await expect(promise).resolves.toMatchObject({
      feedbackId: FEEDBACK_ID,
    });
  });

  it('НЕ вызывает invalidateQueries (write-only эндпоинт)', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper, qc } = makeWrapper();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useFeedbackSubmit(), { wrapper });

    act(() => {
      result.current.submit({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        isUseful: true,
      });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidateSpy).not.toHaveBeenCalled();
  });
});

describe('useFeedbackSubmit — error handling', () => {
  it('400 VALIDATION_ERROR → onError с title из ERROR_UX', async () => {
    postSpy.mockRejectedValueOnce(orch('VALIDATION_ERROR', 'Проверьте введённые данные', 400));
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useFeedbackSubmit({ onError }), { wrapper });

    act(() => {
      result.current.submit({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        isUseful: true,
      });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).toHaveBeenCalledTimes(1);
    const [err, userMessage] = onError.mock.calls[0]!;
    expect(err).toMatchObject({ error_code: 'VALIDATION_ERROR', status: 400 });
    expect(userMessage).toMatchObject({ title: expect.any(String) });
  });

  it('500 INTERNAL_ERROR → onError с action=retry', async () => {
    postSpy.mockRejectedValueOnce(orch('INTERNAL_ERROR', 'Ошибка на сервере', 500));
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useFeedbackSubmit({ onError }), { wrapper });

    act(() => {
      result.current.submit({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        isUseful: false,
      });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'INTERNAL_ERROR' }),
      expect.objectContaining({ action: 'retry' }),
    );
  });

  it('REQUEST_ABORTED → onError НЕ вызван', async () => {
    postSpy.mockRejectedValueOnce(orch('REQUEST_ABORTED', 'Запрос отменён'));
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useFeedbackSubmit({ onError }), { wrapper });

    act(() => {
      result.current.submit({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        isUseful: true,
      });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).not.toHaveBeenCalled();
  });

  it('onError НЕ вызван на success', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useFeedbackSubmit({ onError }), { wrapper });

    act(() => {
      result.current.submit({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        isUseful: true,
      });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onError).not.toHaveBeenCalled();
  });
});
