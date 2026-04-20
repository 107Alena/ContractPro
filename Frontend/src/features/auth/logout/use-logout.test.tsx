// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const logoutMock = vi.fn<[], Promise<void>>();

vi.mock('@/processes/auth-flow', () => ({
  logout: (...args: unknown[]) => logoutMock(...(args as [])),
}));

import { useLogout } from './use-logout';

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

beforeEach(() => {
  logoutMock.mockResolvedValue();
});

describe('useLogout', () => {
  it('вызывает processes/auth-flow logout()', async () => {
    const { result } = renderHook(() => useLogout());
    await act(async () => {
      await result.current.logout();
    });
    expect(logoutMock).toHaveBeenCalledTimes(1);
  });

  it('устанавливает isPending=true во время запроса', async () => {
    let resolve!: () => void;
    logoutMock.mockImplementation(
      () =>
        new Promise<void>((r) => {
          resolve = r;
        }),
    );
    const { result } = renderHook(() => useLogout());
    expect(result.current.isPending).toBe(false);

    let logoutPromise!: Promise<void>;
    act(() => {
      logoutPromise = result.current.logout();
    });
    await waitFor(() => expect(result.current.isPending).toBe(true));

    await act(async () => {
      resolve();
      await logoutPromise;
    });
    expect(result.current.isPending).toBe(false);
  });

  it('игнорирует повторный клик пока idPending', async () => {
    let resolve!: () => void;
    logoutMock.mockImplementation(
      () =>
        new Promise<void>((r) => {
          resolve = r;
        }),
    );
    const { result } = renderHook(() => useLogout());

    let first!: Promise<void>;
    act(() => {
      first = result.current.logout();
    });
    await waitFor(() => expect(result.current.isPending).toBe(true));

    await act(async () => {
      await result.current.logout();
    });

    expect(logoutMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolve();
      await first;
    });
  });

  it('сбрасывает isPending даже если coreLogout отклонил промис', async () => {
    logoutMock.mockRejectedValueOnce(new Error('network'));
    const { result } = renderHook(() => useLogout());
    await act(async () => {
      await expect(result.current.logout()).rejects.toThrow('network');
    });
    expect(result.current.isPending).toBe(false);
  });
});
