// @vitest-environment jsdom
//
// Тесты bridge-хука: SSE → store. Проверяем RBAC-gate (BUSINESS_USER не
// получает callback) и forward event в store. EventSource мокается через
// shared/api openEventStream — заменяем модуль через vi.mock.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, renderHook } from '@testing-library/react';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// ─── DI: подменяем opener в shared/api через `openEventStreamFn` опцию.
//     Достаточно использовать реальный useEventStream — он принимает
//     openEventStreamFn, но useLowConfidenceBridge вызывает useEventStream
//     БЕЗ DI-инжекта. Поэтому мокаем `openEventStream` через vi.mock.
import * as sseModule from '@/shared/api/sse';
import { sessionStore } from '@/shared/auth/session-store';

import { useLowConfidenceStore } from './low-confidence-store';
import type { TypeConfirmationEvent } from './types';
import { useLowConfidenceBridge } from './use-low-confidence-bridge';

let lastOpts: sseModule.OpenEventStreamOptions | null = null;
const openSpy = vi.fn((opts: sseModule.OpenEventStreamOptions): (() => void) => {
  lastOpts = opts;
  return () => {};
});

beforeEach(() => {
  vi.spyOn(sseModule, 'openEventStream').mockImplementation(openSpy);
  lastOpts = null;
  openSpy.mockClear();
  useLowConfidenceStore.getState().__reset();
  // Set valid session by default — bridge requires accessToken to subscribe.
  sessionStore.getState().setAccess('jwt-test', 3600);
  sessionStore.getState().setUser({
    user_id: 'u-1',
    email: 'u@x.test',
    name: 'U',
    role: 'LAWYER',
    organization_id: 'o-1',
    organization_name: 'Org',
    permissions: { export_enabled: true },
  });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  sessionStore.getState().clear();
  useLowConfidenceStore.getState().__reset();
});

function makeEvent(): TypeConfirmationEvent {
  return {
    document_id: 'doc-1',
    version_id: 'ver-1',
    status: 'AWAITING_USER_INPUT',
    suggested_type: 'услуги',
    confidence: 0.62,
    threshold: 0.75,
  };
}

function makeWrapper(): (p: { children: ReactNode }) => JSX.Element {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function QcWrapper({ children }: { children: ReactNode }): JSX.Element {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }
  return QcWrapper;
}

describe('useLowConfidenceBridge — RBAC gate', () => {
  it('LAWYER → SSE-подписка открыта, событие из SSE доходит до store.open', () => {
    renderHook(() => useLowConfidenceBridge(), { wrapper: makeWrapper() });
    expect(openSpy).toHaveBeenCalledTimes(1);
    expect(lastOpts?.onTypeConfirmation).toBeTypeOf('function');

    const event = makeEvent();
    lastOpts!.onTypeConfirmation!(event);
    expect(useLowConfidenceStore.getState().current).toBe(event);
  });

  it('ORG_ADMIN → SSE-подписка открыта, событие из SSE доходит до store.open', () => {
    sessionStore.getState().setUser({
      user_id: 'u-2',
      email: 'a@x.test',
      name: 'A',
      role: 'ORG_ADMIN',
      organization_id: 'o-1',
      organization_name: 'Org',
      permissions: { export_enabled: true },
    });
    renderHook(() => useLowConfidenceBridge(), { wrapper: makeWrapper() });
    expect(openSpy).toHaveBeenCalledTimes(1);

    const event = makeEvent();
    lastOpts!.onTypeConfirmation!(event);
    expect(useLowConfidenceStore.getState().current).toBe(event);
  });

  it('BUSINESS_USER → SSE-подписка НЕ открыта (enabled=false, без EventSource)', () => {
    sessionStore.getState().setUser({
      user_id: 'u-3',
      email: 'b@x.test',
      name: 'B',
      role: 'BUSINESS_USER',
      organization_id: 'o-1',
      organization_name: 'Org',
      permissions: { export_enabled: false },
    });
    renderHook(() => useLowConfidenceBridge(), { wrapper: makeWrapper() });
    expect(openSpy).not.toHaveBeenCalled();
    expect(useLowConfidenceStore.getState().current).toBeNull();
  });

  it('Без сессии → SSE-подписка НЕ открыта (enabled=false)', () => {
    sessionStore.getState().clear();
    renderHook(() => useLowConfidenceBridge(), { wrapper: makeWrapper() });
    expect(openSpy).not.toHaveBeenCalled();
  });
});
