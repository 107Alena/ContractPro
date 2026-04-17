// SSE-wrapper для real-time стрима статусов обработки (§7.7 + §20.2
// high-architecture.md). Реализует:
//   - нативный EventSource → onEvent(StatusEvent)
//   - heartbeat watchdog 45s (backend пингует 15s, запас ×3)
//   - exponential backoff reconnect: 2^retry секунд, clamp max 30s
//   - polling fallback на GET /contracts/{id}/versions/{vid}/status после
//     5 неудачных реконнектов подряд (активен только если и contractId, и
//     versionId известны — иначе бесконечный SSE-reconnect с clamp'ом)
//   - 24-часовой soft-reset: принудительное переподключение раз в сутки
//
// Testability: `createEventStreamOpener({ eventSourceCtor, http, now })`
// (по аналогии с `createHttpClient` в client.ts) позволяет инжектить
// EventSource-мок в vitest (env=node, где EventSource отсутствует) и fake
// `now()` для детерминизма 24h-таймера. Production-потребители вызывают
// экспортируемый `openEventStream` — он строится на default-зависимостях.
//
// Безопасность: JWT летит в query-параметре (`?token=...`) — EventSource API
// не поддерживает кастомные заголовки. Остаточная клиентская поверхность
// утечки документирована в ADR-FE-10 и мигрируется на одноразовый sse_ticket.
import type { AxiosInstance } from 'axios';

import { sessionStore } from '@/shared/auth/session-store';

import { http as defaultHttp } from './client';
import { isOrchestratorError } from './errors';
import type { components } from './openapi';
import type { StatusEvent } from './sse-events';

// ─────────── Публичные типы ───────────

// StatusEvent — доменный контракт события. Владелец — транспортный слой
// shared/api (sse-events.ts); re-export из entities/job предоставляется
// consume'ерам из слоёв features/widgets/pages по §20.2.
export type { StatusEvent, UserProcessingStatus } from './sse-events';

export type TransportMode = 'sse' | 'polling';

export interface OpenEventStreamOptions {
  /**
   * UUID договора (в DM это document_id, в Orchestrator-API — contract_id).
   * Прокидывается backend как фильтр потока `?document_id=`.
   */
  documentId?: string;
  /**
   * UUID версии. Требуется для активации polling-fallback, поскольку
   * endpoint `/contracts/{id}/versions/{vid}/status` предусматривает
   * обязательный `version_id`. Без него fallback отключён — остаётся
   * бесконечный SSE-reconnect с clamp'ом `SSE_MAX_BACKOFF_MS`.
   */
  versionId?: string;
  /** Callback при каждом `status_update`-событии (SSE или poll). */
  onEvent: (event: StatusEvent) => void;
  /** Переключение транспорта (для логирования/диагностики в UI). */
  onTransportChange?: (mode: TransportMode) => void;
}

export type Unsubscribe = () => void;
export type OpenEventStreamFn = (opts: OpenEventStreamOptions) => Unsubscribe;

export interface EventSourceCtor {
  new (url: string | URL): EventSource;
}

export interface OpenEventStreamDeps {
  eventSourceCtor?: EventSourceCtor;
  http?: AxiosInstance;
  now?: () => number;
}

// ─────────── Константы §7.7 ───────────

/** backend пингует 15s; запас ×3 до признания соединения мёртвым. */
export const SSE_HEARTBEAT_TIMEOUT_MS = 45_000;
/** после N подряд неуспешных попыток SSE — переключаемся на polling. */
export const SSE_MAX_RECONNECT_ATTEMPTS = 5;
/** clamp экспоненциального backoff'а (2^retry секунд). */
export const SSE_MAX_BACKOFF_MS = 30_000;
/** интервал polling-fallback (середина диапазона 2–5с из ТЗ). */
export const SSE_POLLING_INTERVAL_MS = 3_000;
/** 24-часовой soft-reset — браузеры/прокси могут «подвешивать» долгие SSE. */
export const SSE_SOFT_RESET_MS = 24 * 60 * 60 * 1_000;

// ─────────── Публичный API ───────────

type ProcessingStatus = components['schemas']['ProcessingStatus'];

/**
 * Фабрика, аналог `createHttpClient`. Все «глобальные» зависимости
 * (EventSource-конструктор, http-клиент, `now()`) инъектируются для
 * unit-тестирования в node-окружении.
 */
export function createEventStreamOpener(deps: OpenEventStreamDeps = {}): OpenEventStreamFn {
  // В node-vitest `globalThis.EventSource` = undefined. Резолвим лениво —
  // чтобы default-инстанс можно было импортировать в обеих средах и
  // тесты инжектили свой `FakeEventSource`.
  const resolveEventSource = (): EventSourceCtor | undefined =>
    deps.eventSourceCtor ??
    (typeof globalThis !== 'undefined'
      ? (globalThis as { EventSource?: EventSourceCtor }).EventSource
      : undefined);
  const http = deps.http ?? defaultHttp;
  const now = deps.now ?? ((): number => Date.now());

  return function openEventStream(opts: OpenEventStreamOptions): Unsubscribe {
    const token = sessionStore.getState().accessToken;
    const EventSourceImpl = resolveEventSource();

    // Ранний exit без токена: useEffect-потребитель ещё не успел получить
    // profile/refresh — auth-flow редиректит на /login сам. Тихо возвращаем
    // noop, чтобы cleanup в useEffect отработал безопасно.
    if (!token) {
      return (): void => {};
    }
    // Ранний exit без EventSource-импла (node-ssr без мока). Симметрично токену.
    if (!EventSourceImpl) {
      return (): void => {};
    }

    const url = new URL('/api/v1/events/stream', window.location.origin);
    // SECURITY: JWT передаётся в query-параметре, поскольку EventSource API
    // не поддерживает кастомные заголовки. Backend исключает `token` из
    // application и proxy access-логов (ApiBackendOrchestrator/architecture/
    // security.md §1.7), но JWT всё ещё может утечь через browser history,
    // third-party JS (Sentry/GA/расширения), CDN raw access logs, screenshots.
    // План миграции на одноразовый `sse_ticket` — Frontend/architecture/adr/
    // 010-sse-ticket-auth.md (зависит от backend ORCH-TASK-047).
    url.searchParams.set('token', token);
    if (opts.documentId) url.searchParams.set('document_id', opts.documentId);

    // ─── Closure state ───
    let cancelled = false;
    let mode: TransportMode = 'sse';
    let es: EventSource | null = null;
    let retry = 0;
    let heartbeatTimer: ReturnType<typeof setTimeout> | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let pollingTimer: ReturnType<typeof setTimeout> | null = null;
    let pollingController: AbortController | null = null;
    let softResetAt = now() + SSE_SOFT_RESET_MS;

    const clearTimer = (t: ReturnType<typeof setTimeout> | null): null => {
      if (t !== null) clearTimeout(t);
      return null;
    };

    const notifyTransport = (next: TransportMode): void => {
      if (mode === next) return;
      mode = next;
      opts.onTransportChange?.(next);
    };

    const resetHeartbeat = (): void => {
      heartbeatTimer = clearTimer(heartbeatTimer);
      if (cancelled) return;
      heartbeatTimer = setTimeout(onHeartbeatStale, SSE_HEARTBEAT_TIMEOUT_MS);
    };

    const onHeartbeatStale = (): void => {
      if (cancelled) return;
      // Backend не пинговал > 45с — соединение «молчит». Рвём и реконнектим.
      es?.close();
      es = null;
      scheduleReconnect();
    };

    const scheduleReconnect = (): void => {
      if (cancelled) return;
      heartbeatTimer = clearTimer(heartbeatTimer);

      // 24h soft-reset: если срок вышел — forced reconnect со сбросом retry.
      if (now() >= softResetAt) {
        retry = 0;
        softResetAt = now() + SSE_SOFT_RESET_MS;
      }

      retry += 1;
      if (retry > SSE_MAX_RECONNECT_ATTEMPTS) {
        startPollingFallback();
        return;
      }
      const delay = Math.min(SSE_MAX_BACKOFF_MS, 2 ** retry * 1_000);
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null;
        connect();
      }, delay);
    };

    const connect = (): void => {
      if (cancelled) return;
      notifyTransport('sse');

      // Cheap insurance: закрываем предыдущий es перед созданием нового
      // (review M3). Актуально для polling→SSE возврата и 404→SSE возврата,
      // где es мог не успеть обнулиться через onerror.
      es?.close();
      es = null;

      let instance: EventSource;
      try {
        instance = new EventSourceImpl(url);
      } catch {
        // Например, window.location.origin недоступен в SSR. Реконнект не
        // поможет — переходим на polling-fallback если возможно, иначе
        // ждём следующий backoff (пусть retry растёт, чтобы не зациклиться).
        scheduleReconnect();
        return;
      }
      es = instance;

      instance.addEventListener('status_update', (msg) => {
        if (cancelled) return;
        resetHeartbeat();
        const data = (msg as MessageEvent<string>).data;
        try {
          const parsed = JSON.parse(data) as StatusEvent;
          opts.onEvent(parsed);
        } catch {
          // Мусорный payload — не ломаем подписку, полагаемся на следующий event.
        }
      });

      instance.onopen = (): void => {
        if (cancelled) return;
        retry = 0;
        resetHeartbeat();
      };

      instance.onerror = (): void => {
        if (cancelled) return;
        instance.close();
        if (es === instance) es = null;
        scheduleReconnect();
      };
    };

    // ─── Polling fallback ───

    const canPoll = Boolean(opts.documentId && opts.versionId);

    const stopPolling = (): void => {
      pollingTimer = clearTimer(pollingTimer);
      pollingController?.abort();
      pollingController = null;
    };

    const startPollingFallback = (): void => {
      if (cancelled) return;
      if (!canPoll) {
        // Без version_id fallback невозможен — остаёмся в бесконечном
        // reconnect'е с clamp'ом max 30s. Сбрасываем retry, чтобы backoff
        // не «залип» на 30s навсегда.
        retry = SSE_MAX_RECONNECT_ATTEMPTS;
        reconnectTimer = setTimeout(() => {
          reconnectTimer = null;
          connect();
        }, SSE_MAX_BACKOFF_MS);
        return;
      }
      notifyTransport('polling');
      void pollOnce();
    };

    const pollOnce = async (): Promise<void> => {
      if (cancelled) return;
      // Captured-контроллер + захваченные id-шники: stale-guard после await
      // (review M1). Если unsubscribe/stopPolling произошёл во время
      // сетевого запроса, новый pollingController уже указывает на другой
      // цикл, или цикл сменился на SSE — тогда данное резолюшн стало stale.
      const controller = new AbortController();
      pollingController = controller;
      const docId = opts.documentId!;
      const verId = opts.versionId!;
      const path = `/contracts/${docId}/versions/${verId}/status`;
      try {
        const res = await http.get<ProcessingStatus>(path, {
          signal: controller.signal,
        });
        if (cancelled || pollingController !== controller) return;
        const body = res.data;
        if (body?.status && body.version_id) {
          opts.onEvent({
            version_id: body.version_id,
            document_id: docId,
            status: body.status,
            ...(body.message !== undefined && { message: body.message }),
            ...(body.updated_at !== undefined && { timestamp: body.updated_at }),
          });
        }
      } catch (err) {
        if (cancelled || pollingController !== controller) return;
        if (isOrchestratorError(err)) {
          // 404/403 → остановить polling и попытаться восстановить SSE.
          // Версия могла быть удалена/доступ отозван — дальнейшие попытки
          // бессмысленны; отдаём управление reconnect'у с retry=0 (новый цикл).
          // softResetAt НЕ трогаем (review M2): 404 — событие polling-цикла,
          // не имеет отношения к 24h-сроку SSE-соединения.
          if (err.status === 404 || err.status === 403) {
            retry = 0;
            stopPolling();
            connect();
            return;
          }
        }
        // Остальные ошибки — транзиентны, продолжаем polling.
      }
      if (cancelled) return;
      pollingTimer = setTimeout(() => {
        pollingTimer = null;
        // После тика polling пытаемся вернуться в SSE: закрываем polling
        // и запускаем новый connect(). Если SSE снова упадёт — retry-счётчик
        // заново достигнет 5, и вернёмся в polling. Это реализует §7.7
        // «возвращаемся к SSE при успешном open».
        stopPolling();
        retry = 0;
        connect();
      }, SSE_POLLING_INTERVAL_MS);
    };

    // ─── Kick-off ───
    connect();

    return (): void => {
      cancelled = true;
      heartbeatTimer = clearTimer(heartbeatTimer);
      reconnectTimer = clearTimer(reconnectTimer);
      stopPolling();
      es?.close();
      es = null;
    };
  };
}

/** Дефолтный экземпляр. Прод-потребители (`useEventStream`) зовут именно его. */
export const openEventStream: OpenEventStreamFn = createEventStreamOpener();
