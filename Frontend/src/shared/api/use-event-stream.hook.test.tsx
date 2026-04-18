// @vitest-environment jsdom
//
// Хук-тесты useEventStream: React-lifecycle (подписка в useEffect, cleanup,
// ресабскрайб при смене documentId, latest-ref для callbacks). Логика
// реакций (setQueryData/invalidate/toast) покрыта в use-event-stream.test.ts —
// чистая функция `dispatchStatusEvent`.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react';
import { type ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { toast as toastApi } from '@/shared/ui/toast';

import { qk } from './query-keys';
import type { OpenEventStreamFn, OpenEventStreamOptions } from './sse';
import type { StatusEvent } from './sse-events';
import { useEventStream } from './use-event-stream';

type ToastApi = typeof toastApi;

interface FakeSseControl {
  emit: (event: StatusEvent) => void;
  unsubscribed: boolean;
  opts: OpenEventStreamOptions;
}

function makeFakeOpener(): {
  fn: OpenEventStreamFn;
  instances: FakeSseControl[];
} {
  const instances: FakeSseControl[] = [];
  const fn: OpenEventStreamFn = (opts) => {
    const control: FakeSseControl = {
      emit: (event) => opts.onEvent(event),
      unsubscribed: false,
      opts,
    };
    instances.push(control);
    return () => {
      control.unsubscribed = true;
    };
  };
  return { fn, instances };
}

type ToastSpy = {
  [K in keyof ToastApi]: ReturnType<typeof vi.fn<Parameters<ToastApi[K]>, ReturnType<ToastApi[K]>>>;
};

function makeToastSpy(): ToastSpy {
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

function makeWrapper(): { wrapper: (p: { children: ReactNode }) => JSX.Element; qc: QueryClient } {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }): JSX.Element => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return { wrapper, qc };
}

const BASE_EVENT: StatusEvent = {
  document_id: 'doc-1',
  version_id: 'ver-1',
  status: 'PROCESSING',
};

describe('useEventStream — lifecycle', () => {
  let toast: ToastSpy;

  beforeEach(() => {
    toast = makeToastSpy();
  });

  it('при монтировании открывает одну подписку и передаёт documentId/versionId', () => {
    const { fn, instances } = makeFakeOpener();
    const { wrapper } = makeWrapper();

    renderHook(
      () =>
        useEventStream('doc-1', {
          versionId: 'ver-1',
          openEventStreamFn: fn,
          toast,
        }),
      { wrapper },
    );

    expect(instances).toHaveLength(1);
    expect(instances[0]?.opts.documentId).toBe('doc-1');
    expect(instances[0]?.opts.versionId).toBe('ver-1');
  });

  it('при unmount вызывается unsubscribe из openEventStream', () => {
    const { fn, instances } = makeFakeOpener();
    const { wrapper } = makeWrapper();

    const { unmount } = renderHook(
      () => useEventStream('doc-1', { openEventStreamFn: fn, toast }),
      { wrapper },
    );

    expect(instances[0]?.unsubscribed).toBe(false);
    unmount();
    expect(instances[0]?.unsubscribed).toBe(true);
  });

  it('ресабскрайбится при смене documentId (старый unsubscribe, новый subscribe)', () => {
    const { fn, instances } = makeFakeOpener();
    const { wrapper } = makeWrapper();

    const { rerender } = renderHook(
      ({ docId }: { docId: string }) => useEventStream(docId, { openEventStreamFn: fn, toast }),
      { wrapper, initialProps: { docId: 'doc-1' } },
    );

    expect(instances).toHaveLength(1);
    rerender({ docId: 'doc-2' });
    expect(instances).toHaveLength(2);
    expect(instances[0]?.unsubscribed).toBe(true);
    expect(instances[1]?.opts.documentId).toBe('doc-2');
    expect(instances[1]?.unsubscribed).toBe(false);
  });

  it('не ресабскрайбится при смене onAwaitingUserInput-колбэка (latest-ref)', () => {
    const { fn, instances } = makeFakeOpener();
    const { wrapper } = makeWrapper();

    const cbA = vi.fn();
    const cbB = vi.fn();

    const { rerender } = renderHook(
      ({ cb }: { cb: (e: StatusEvent) => void }) =>
        useEventStream('doc-1', {
          onAwaitingUserInput: cb,
          openEventStreamFn: fn,
          toast,
        }),
      { wrapper, initialProps: { cb: cbA } },
    );

    expect(instances).toHaveLength(1);
    rerender({ cb: cbB });
    expect(instances).toHaveLength(1); // подписка осталась прежней

    // Событие AWAITING_USER_INPUT должно вызвать СВЕЖИЙ cbB, а не старый cbA.
    act(() => {
      instances[0]?.emit({ ...BASE_EVENT, status: 'AWAITING_USER_INPUT' });
    });
    expect(cbA).not.toHaveBeenCalled();
    expect(cbB).toHaveBeenCalledTimes(1);
  });
});

describe('useEventStream — интеграция с QueryClient и toast', () => {
  it('событие → setQueryData в qk.contracts.status', () => {
    const { fn, instances } = makeFakeOpener();
    const { wrapper, qc } = makeWrapper();
    const toast = makeToastSpy();

    renderHook(() => useEventStream('doc-1', { openEventStreamFn: fn, toast }), { wrapper });

    act(() => {
      instances[0]?.emit({ ...BASE_EVENT, status: 'ANALYZING' });
    });

    const cached = qc.getQueryData(qk.contracts.status('doc-1', 'ver-1')) as StatusEvent;
    expect(cached?.status).toBe('ANALYZING');
  });

  it('READY → invalidateQueries(results)', () => {
    const { fn, instances } = makeFakeOpener();
    const { wrapper, qc } = makeWrapper();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const toast = makeToastSpy();

    renderHook(() => useEventStream('doc-1', { openEventStreamFn: fn, toast }), { wrapper });

    act(() => {
      instances[0]?.emit({ ...BASE_EVENT, status: 'READY' });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: qk.contracts.results('doc-1', 'ver-1'),
    });
  });

  it('FAILED → toast.error вызван с message и correlation_id', () => {
    const { fn, instances } = makeFakeOpener();
    const { wrapper } = makeWrapper();
    const toast = makeToastSpy();

    renderHook(() => useEventStream('doc-1', { openEventStreamFn: fn, toast }), { wrapper });

    act(() => {
      instances[0]?.emit({
        ...BASE_EVENT,
        status: 'FAILED',
        message: 'Upstream error',
        correlation_id: 'req-xyz',
      });
    });

    expect(toast.error).toHaveBeenCalledWith({
      title: 'Upstream error',
      description: 'correlation_id: req-xyz',
    });
  });
});
