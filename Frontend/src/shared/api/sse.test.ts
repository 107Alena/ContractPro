// Unit-тесты SSE-обёртки (FE-TASK-015, §7.7 high-architecture.md).
// Покрывают: auth-gate (нет токена → noop), URL-composition + SECURITY-token,
// status_update → onEvent, heartbeat watchdog, exponential backoff,
// polling-fallback после 5 реконнектов, возврат SSE при polling-тике,
// cleanup-safety при отписке во время reconnect/polling, 24h soft-reset,
// ранний exit без EventSource-импла.
//
// EventSource в vitest env=node отсутствует — инжектим `FakeEventSource`
// через `createEventStreamOpener({ eventSourceCtor })`. http инжектим
// лёгким моком с .get(); 401-refresh, retry и прочая axios-логика
// покрыта в client.test.ts.
import type { AxiosInstance, AxiosResponse } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { sessionStore } from '@/shared/auth/session-store';

import { OrchestratorError } from './errors';
import {
  createEventStreamOpener,
  SSE_HEARTBEAT_TIMEOUT_MS,
  SSE_MAX_RECONNECT_ATTEMPTS,
  SSE_POLLING_INTERVAL_MS,
  SSE_SOFT_RESET_MS,
  type StatusEvent,
} from './sse';

// ─────────── Fake EventSource ───────────

type Listener = (ev: MessageEvent<string>) => void;

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  public url: string;
  public readyState: 0 | 1 | 2 = 0;
  public onopen: (() => void) | null = null;
  public onerror: (() => void) | null = null;
  public closed = false;
  private listeners = new Map<string, Listener[]>();

  constructor(url: string | URL) {
    this.url = url.toString();
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, cb: Listener): void {
    const arr = this.listeners.get(type) ?? [];
    arr.push(cb);
    this.listeners.set(type, arr);
  }

  emit(type: string, data: string): void {
    const arr = this.listeners.get(type) ?? [];
    for (const cb of arr) cb({ data } as MessageEvent<string>);
  }

  openHandshake(): void {
    this.readyState = 1;
    this.onopen?.();
  }

  fail(): void {
    this.onerror?.();
  }

  close(): void {
    this.closed = true;
    this.readyState = 2;
  }

  static reset(): void {
    FakeEventSource.instances = [];
  }

  static last(): FakeEventSource {
    const inst = FakeEventSource.instances.at(-1);
    if (!inst) throw new Error('FakeEventSource: no instance created');
    return inst;
  }
}

// ─────────── Fake http ───────────

interface HttpCall {
  url: string;
  signal?: AbortSignal;
}

function createFakeHttp(responder: (call: HttpCall) => Promise<AxiosResponse<unknown>>): {
  http: AxiosInstance;
  calls: HttpCall[];
} {
  const calls: HttpCall[] = [];
  const get = vi.fn(
    async (url: string, cfg?: { signal?: AbortSignal }): Promise<AxiosResponse<unknown>> => {
      const call: HttpCall = { url, ...(cfg?.signal ? { signal: cfg.signal } : {}) };
      calls.push(call);
      return responder(call);
    },
  );
  // Only `.get` is exercised by sse.ts polling; cast as AxiosInstance.
  const http = { get } as unknown as AxiosInstance;
  return { http, calls };
}

function axiosResponse<T>(data: T, status = 200): AxiosResponse<T> {
  return {
    data,
    status,
    statusText: 'OK',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- headers mock
    headers: {} as any,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- config mock
    config: {} as any,
  };
}

// ─────────── jsdom-esque shims ───────────
// sse.ts использует `window.location.origin` для построения URL. В node env
// window отсутствует → подмешаем минимальный shim только для тестов.
beforeEach(() => {
  (globalThis as unknown as { window: { location: { origin: string } } }).window = {
    location: { origin: 'http://orch.test' },
  };
});

afterEach(() => {
  FakeEventSource.reset();
  sessionStore.getState().clear();
  vi.useRealTimers();
  delete (globalThis as unknown as { window?: unknown }).window;
});

// ─────────── Helpers ───────────

function setup(
  opts: {
    httpResponder?: (call: HttpCall) => Promise<AxiosResponse<unknown>>;
    now?: () => number;
    token?: string | null;
  } = {},
): {
  opener: ReturnType<typeof createEventStreamOpener>;
  calls: HttpCall[];
} {
  if (opts.token === null) {
    sessionStore.getState().clear();
  } else {
    sessionStore.getState().setAccess(opts.token ?? 'tok-1', 3600);
  }
  const { http, calls } = createFakeHttp(
    opts.httpResponder ??
      (async () =>
        axiosResponse({
          version_id: 'v-1',
          status: 'PROCESSING',
          message: 'в работе',
          updated_at: '2026-04-17T10:00:00Z',
        })),
  );
  const deps: Parameters<typeof createEventStreamOpener>[0] = {
    eventSourceCtor: FakeEventSource as unknown as typeof EventSource,
    http,
  };
  if (opts.now) deps.now = opts.now;
  const opener = createEventStreamOpener(deps);
  return { opener, calls };
}

// ─────────── Тесты ───────────

describe('createEventStreamOpener — URL + auth gate', () => {
  it('без access-token — возвращает noop, EventSource не создаётся', () => {
    const { opener } = setup({ token: null });
    const onEvent = vi.fn();
    const unsub = opener({ onEvent });
    unsub();
    expect(FakeEventSource.instances).toHaveLength(0);
    expect(onEvent).not.toHaveBeenCalled();
  });

  it('кладёт JWT в ?token=... и опционально ?document_id=...', () => {
    const { opener } = setup({ token: 'jwt-xyz' });
    opener({ documentId: 'doc-1', onEvent: vi.fn() });
    const url = new URL(FakeEventSource.last().url);
    expect(url.pathname).toBe('/api/v1/events/stream');
    expect(url.searchParams.get('token')).toBe('jwt-xyz');
    expect(url.searchParams.get('document_id')).toBe('doc-1');
  });

  it('без documentId — document_id в URL отсутствует', () => {
    const { opener } = setup();
    opener({ onEvent: vi.fn() });
    const url = new URL(FakeEventSource.last().url);
    expect(url.searchParams.has('document_id')).toBe(false);
  });

  it('ранний exit без EventSource-импла — noop', () => {
    sessionStore.getState().setAccess('tok', 3600);
    const { http } = createFakeHttp(async () => axiosResponse({}));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- explicit undefined ctor
    const opener = createEventStreamOpener({ eventSourceCtor: undefined as any, http });
    expect(() => opener({ onEvent: vi.fn() })()).not.toThrow();
  });
});

describe('status_update — onEvent', () => {
  it('парсит JSON и вызывает onEvent со статусом', () => {
    const { opener } = setup();
    const onEvent = vi.fn<[StatusEvent], void>();
    opener({ documentId: 'd-1', onEvent });
    const payload: StatusEvent = {
      version_id: 'v-1',
      document_id: 'd-1',
      status: 'PROCESSING',
      message: 'в работе',
    };
    FakeEventSource.last().emit('status_update', JSON.stringify(payload));
    expect(onEvent).toHaveBeenCalledWith(payload);
  });

  it('невалидный JSON не ломает подписку — onEvent не вызван', () => {
    const { opener } = setup();
    const onEvent = vi.fn();
    opener({ onEvent });
    expect(() => FakeEventSource.last().emit('status_update', 'not-json')).not.toThrow();
    expect(onEvent).not.toHaveBeenCalled();
  });

  it('не вызывает onEvent после unsubscribe', () => {
    const { opener } = setup();
    const onEvent = vi.fn();
    const unsub = opener({ onEvent });
    unsub();
    FakeEventSource.last().emit(
      'status_update',
      '{"version_id":"v","document_id":"d","status":"READY"}',
    );
    expect(onEvent).not.toHaveBeenCalled();
  });
});

describe('type_confirmation_required — onTypeConfirmation (FR-2.1.3)', () => {
  it('парсит payload и вызывает onTypeConfirmation', () => {
    const { opener } = setup();
    const onTypeConfirmation = vi.fn();
    opener({ onEvent: vi.fn(), onTypeConfirmation });
    const payload = {
      document_id: 'doc-1',
      version_id: 'ver-1',
      status: 'AWAITING_USER_INPUT' as const,
      suggested_type: 'услуги',
      confidence: 0.62,
      threshold: 0.75,
      alternatives: [{ contract_type: 'подряд', confidence: 0.21 }],
    };
    FakeEventSource.last().emit('type_confirmation_required', JSON.stringify(payload));
    expect(onTypeConfirmation).toHaveBeenCalledWith(payload);
  });

  it('без onTypeConfirmation в options — событие молча игнорируется (без падений)', () => {
    const { opener } = setup();
    opener({ onEvent: vi.fn() });
    expect(() =>
      FakeEventSource.last().emit(
        'type_confirmation_required',
        '{"document_id":"d","version_id":"v","status":"AWAITING_USER_INPUT","suggested_type":"x","confidence":0.5,"threshold":0.7}',
      ),
    ).not.toThrow();
  });

  it('малформ-payload (отсутствует suggested_type) — onTypeConfirmation НЕ вызван', () => {
    const { opener } = setup();
    const onTypeConfirmation = vi.fn();
    opener({ onEvent: vi.fn(), onTypeConfirmation });
    FakeEventSource.last().emit(
      'type_confirmation_required',
      '{"document_id":"d","version_id":"v","status":"AWAITING_USER_INPUT","confidence":0.5,"threshold":0.7}',
    );
    expect(onTypeConfirmation).not.toHaveBeenCalled();
  });

  it('битый JSON — подписка живёт, callback не вызван', () => {
    const { opener } = setup();
    const onTypeConfirmation = vi.fn();
    opener({ onEvent: vi.fn(), onTypeConfirmation });
    expect(() =>
      FakeEventSource.last().emit('type_confirmation_required', 'garbage{'),
    ).not.toThrow();
    expect(onTypeConfirmation).not.toHaveBeenCalled();
  });

  it('после unsubscribe — onTypeConfirmation не вызван', () => {
    const { opener } = setup();
    const onTypeConfirmation = vi.fn();
    const unsub = opener({ onEvent: vi.fn(), onTypeConfirmation });
    unsub();
    FakeEventSource.last().emit(
      'type_confirmation_required',
      '{"document_id":"d","version_id":"v","status":"AWAITING_USER_INPUT","suggested_type":"x","confidence":0.5,"threshold":0.7}',
    );
    expect(onTypeConfirmation).not.toHaveBeenCalled();
  });
});

describe('heartbeat watchdog', () => {
  it('нет событий 45s → close + reconnect', () => {
    vi.useFakeTimers();
    const { opener } = setup();
    opener({ onEvent: vi.fn() });
    const es1 = FakeEventSource.last();
    es1.openHandshake();

    // Таймаут heartbeat истекает → es1.close() + backoff reconnect (2^1=2s).
    vi.advanceTimersByTime(SSE_HEARTBEAT_TIMEOUT_MS);
    expect(es1.closed).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(1);
    vi.advanceTimersByTime(2_000);
    expect(FakeEventSource.instances).toHaveLength(2);
  });

  it('событие сбрасывает таймер — reconnect не триггерится', () => {
    vi.useFakeTimers();
    const onEvent = vi.fn();
    const { opener } = setup();
    opener({ onEvent });
    const es1 = FakeEventSource.last();
    es1.openHandshake();

    // Через 30с приходит event — таймер сбрасывается.
    vi.advanceTimersByTime(30_000);
    es1.emit('status_update', '{"version_id":"v","document_id":"d","status":"PROCESSING"}');
    // Граница heartbeat'а: 44с после event → ещё жив.
    vi.advanceTimersByTime(SSE_HEARTBEAT_TIMEOUT_MS - 1_000);
    expect(es1.closed).toBe(false);
    // +2с → переход через границу 45с → тик.
    vi.advanceTimersByTime(2_000);
    expect(es1.closed).toBe(true);
  });
});

describe('exponential backoff reconnect', () => {
  it('после onerror реконнект через 2^retry секунд (max 30s)', () => {
    vi.useFakeTimers();
    const { opener } = setup();
    opener({ onEvent: vi.fn() });

    // retry=1 → 2s
    FakeEventSource.last().fail();
    vi.advanceTimersByTime(1_999);
    expect(FakeEventSource.instances).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(2);

    // retry=2 → 4s
    FakeEventSource.last().fail();
    vi.advanceTimersByTime(4_000);
    expect(FakeEventSource.instances).toHaveLength(3);

    // retry=3 → 8s
    FakeEventSource.last().fail();
    vi.advanceTimersByTime(8_000);
    expect(FakeEventSource.instances).toHaveLength(4);

    // retry=4 → 16s
    FakeEventSource.last().fail();
    vi.advanceTimersByTime(16_000);
    expect(FakeEventSource.instances).toHaveLength(5);
  });

  it('сбрасывает retry при успешном onopen', () => {
    vi.useFakeTimers();
    const { opener } = setup();
    opener({ onEvent: vi.fn() });
    // retry=1 → 2s
    FakeEventSource.last().fail();
    vi.advanceTimersByTime(2_000);
    FakeEventSource.last().openHandshake();
    // Новый onerror — ждём снова 2s (а не 4s), т.к. retry сбросился.
    FakeEventSource.last().fail();
    vi.advanceTimersByTime(1_999);
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(3);
  });
});

describe('polling fallback', () => {
  it('после 5 неудачных реконнектов без versionId — бесконечный reconnect с max 30s', () => {
    vi.useFakeTimers();
    const onTransportChange = vi.fn();
    const { opener } = setup();
    // Без versionId fallback невозможен.
    opener({ documentId: 'd-1', onEvent: vi.fn(), onTransportChange });

    // 5 onerror подряд → 5 × backoff. После 5-го вместо polling ждём 30s.
    for (let i = 0; i < SSE_MAX_RECONNECT_ATTEMPTS; i += 1) {
      FakeEventSource.last().fail();
      // Пропускаем любой backoff (до 16s при retry=4).
      vi.advanceTimersByTime(30_000);
    }
    // После 5-й неудачи retry > 5 → startPollingFallback()
    // Без versionId → не переходит в polling, но назначает reconnect через 30s.
    const snapshotBefore = FakeEventSource.instances.length;
    FakeEventSource.last().fail();
    vi.advanceTimersByTime(29_999);
    expect(FakeEventSource.instances).toHaveLength(snapshotBefore);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(snapshotBefore + 1);
    expect(onTransportChange).not.toHaveBeenCalledWith('polling');
  });

  it('после 5 неудачных реконнектов с documentId+versionId — переход на polling', async () => {
    vi.useFakeTimers();
    const onTransportChange = vi.fn();
    const onEvent = vi.fn<[StatusEvent], void>();
    const { opener, calls } = setup();
    opener({ documentId: 'd-1', versionId: 'v-1', onEvent, onTransportChange });

    // 5 неудач → retry=5. 6-я неудача → retry>5 → polling.
    for (let i = 0; i < SSE_MAX_RECONNECT_ATTEMPTS; i += 1) {
      FakeEventSource.last().fail();
      vi.advanceTimersByTime(30_000);
    }
    FakeEventSource.last().fail();
    await vi.runOnlyPendingTimersAsync(); // запуск pollOnce()
    expect(onTransportChange).toHaveBeenCalledWith('polling');
    // Ожидаем, что вызов http.get улетел на /contracts/d-1/versions/v-1/status.
    expect(calls[0]?.url).toBe('/contracts/d-1/versions/v-1/status');
    expect(onEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        version_id: 'v-1',
        document_id: 'd-1',
        status: 'PROCESSING',
      }),
    );
  });

  it('после polling-тика пытается вернуться в SSE', async () => {
    vi.useFakeTimers();
    const onTransportChange = vi.fn();
    const { opener } = setup();
    opener({ documentId: 'd-1', versionId: 'v-1', onEvent: vi.fn(), onTransportChange });

    for (let i = 0; i < SSE_MAX_RECONNECT_ATTEMPTS; i += 1) {
      FakeEventSource.last().fail();
      vi.advanceTimersByTime(30_000);
    }
    const sseCountBeforePolling = FakeEventSource.instances.length;
    FakeEventSource.last().fail();
    await vi.runOnlyPendingTimersAsync();

    // Тик polling-а — 3s.
    vi.advanceTimersByTime(SSE_POLLING_INTERVAL_MS);
    await vi.runOnlyPendingTimersAsync();
    // Обратный переход в SSE — новый EventSource создан.
    expect(FakeEventSource.instances.length).toBeGreaterThan(sseCountBeforePolling);
    expect(onTransportChange).toHaveBeenNthCalledWith(1, 'polling');
    // После возврата в SSE notifyTransport('sse') вызывается.
    expect(onTransportChange).toHaveBeenCalledWith('sse');
  });

  it('onerror после возврата в SSE из polling — новый backoff с retry=1', async () => {
    vi.useFakeTimers();
    const { opener } = setup();
    opener({ documentId: 'd-1', versionId: 'v-1', onEvent: vi.fn() });
    // Переход в polling.
    for (let i = 0; i < SSE_MAX_RECONNECT_ATTEMPTS; i += 1) {
      FakeEventSource.last().fail();
      vi.advanceTimersByTime(30_000);
    }
    FakeEventSource.last().fail();
    await vi.runOnlyPendingTimersAsync();
    // Polling-тик через 3с — создаёт новый EventSource.
    vi.advanceTimersByTime(SSE_POLLING_INTERVAL_MS);
    await vi.runOnlyPendingTimersAsync();
    const countAfterReturn = FakeEventSource.instances.length;
    // Новый es — сразу ошибка → retry=1 → backoff 2с.
    FakeEventSource.last().fail();
    vi.advanceTimersByTime(1_999);
    expect(FakeEventSource.instances).toHaveLength(countAfterReturn);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(countAfterReturn + 1);
  });

  it('polling 404 → остановка polling и возврат к SSE', async () => {
    vi.useFakeTimers();
    const httpResponder = vi.fn(async () => {
      throw new OrchestratorError({
        error_code: 'NOT_FOUND',
        message: 'Версия не найдена',
        status: 404,
      });
    });
    const { opener } = setup({ httpResponder });
    opener({ documentId: 'd-1', versionId: 'v-1', onEvent: vi.fn() });

    for (let i = 0; i < SSE_MAX_RECONNECT_ATTEMPTS; i += 1) {
      FakeEventSource.last().fail();
      vi.advanceTimersByTime(30_000);
    }
    const sseCountBeforePolling = FakeEventSource.instances.length;
    FakeEventSource.last().fail();
    await vi.runOnlyPendingTimersAsync();
    // 404 → немедленный возврат в SSE (создаётся новый ES без ожидания 3s-тика).
    expect(FakeEventSource.instances.length).toBeGreaterThan(sseCountBeforePolling);
  });
});

describe('unsubscribe / cleanup safety', () => {
  it('unsubscribe во время ожидающего reconnect — таймер очищается, нового ES не создаётся', () => {
    vi.useFakeTimers();
    const { opener } = setup();
    const unsub = opener({ onEvent: vi.fn() });
    FakeEventSource.last().fail();
    unsub();
    vi.advanceTimersByTime(60_000);
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it('unsubscribe в polling — AbortController вызван, нового poll нет', async () => {
    vi.useFakeTimers();
    let sawSignal: AbortSignal | undefined;
    const { opener, calls } = setup({
      httpResponder: async (call) => {
        sawSignal = call.signal;
        return axiosResponse({
          version_id: 'v-1',
          status: 'PROCESSING',
          updated_at: '2026-04-17T10:00:00Z',
        });
      },
    });
    const unsub = opener({ documentId: 'd-1', versionId: 'v-1', onEvent: vi.fn() });
    for (let i = 0; i < SSE_MAX_RECONNECT_ATTEMPTS; i += 1) {
      FakeEventSource.last().fail();
      vi.advanceTimersByTime(30_000);
    }
    FakeEventSource.last().fail();
    await vi.runOnlyPendingTimersAsync();
    expect(sawSignal).toBeDefined();
    const aborted = sawSignal;
    const callsBefore = calls.length;
    unsub();
    expect(aborted?.aborted).toBe(true);
    // После unsub следующий 3s-polling-тик не должен стартовать (нет новых http-вызовов).
    vi.advanceTimersByTime(SSE_POLLING_INTERVAL_MS * 3);
    await vi.runOnlyPendingTimersAsync();
    expect(calls.length).toBe(callsBefore);
  });

  it('последний es закрывается при unsubscribe', () => {
    const { opener } = setup();
    const unsub = opener({ onEvent: vi.fn() });
    const es = FakeEventSource.last();
    unsub();
    expect(es.closed).toBe(true);
  });
});

describe('24h soft-reset', () => {
  it('по истечении SSE_SOFT_RESET_MS retry сбрасывается до 0', () => {
    vi.useFakeTimers();
    let clock = 0;
    const { opener } = setup({ now: () => clock });
    opener({ onEvent: vi.fn() });
    // retry=1 → 2s
    FakeEventSource.last().fail();
    vi.advanceTimersByTime(2_000);
    // Имитируем, что прошло 25ч — следующий scheduleReconnect должен обнулить retry.
    clock = SSE_SOFT_RESET_MS + 1;
    FakeEventSource.last().fail();
    // После 24h-reset retry=0 → +1 = 1 → 2s backoff.
    vi.advanceTimersByTime(1_999);
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(3);
  });
});
