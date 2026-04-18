// SSE handler для /events/stream. MSW v2 не имеет нативного SSE-плагина,
// поэтому отдаём text/event-stream через ReadableStream (см. §10.3).
//
// WARNING (code-architect): НЕ использовать `vi.useFakeTimers()` в тестах,
// которые рассчитывают на SSE-события — `setTimeout` внутри потока не
// стреляет. Если нужны детерминированные тайминги, предпочитайте прямой
// `controller.enqueue(...)` сразу в `start(...)` без отложенных таймеров.
//
// По умолчанию шлём:
//   1) один `status_update` с заданным статусом через 10 мс
//   2) heartbeat-комментарий через 50 мс (backend пингует каждые 15с, §7.7)
//   3) stream держится открытым — закрывает потребитель (unsubscribe)
//
// Тесты могут переопределить events через factory: createSseHandler(base, {
//   events: [{ delayMs: 0, event: 'status_update', data: {...} }],
// }).

import { http, HttpResponse } from 'msw';

import type { components } from '@/shared/api/openapi';

import { IDS } from '../fixtures/ids';
import { type HandlerBase, joinPath } from './_helpers';

type UserProcessingStatus = components['schemas']['UserProcessingStatus'];

export type SseEvent =
  | {
      type: 'status_update';
      delayMs?: number;
      data: {
        version_id: string;
        status: UserProcessingStatus;
        message?: string;
        progress?: number;
      };
    }
  | {
      type: 'type_confirmation_required';
      delayMs?: number;
      data: {
        version_id: string;
        suggested_type: string;
        confidence: number;
        threshold: number;
        alternatives: { contract_type: string; confidence: number }[];
      };
    }
  | { type: 'heartbeat'; delayMs?: number };

export interface SseHandlerOptions {
  /** Какие события отдать клиенту. Если не указано — default-последовательность. */
  events?: SseEvent[];
  /** Закрывать ли поток после последнего события (по умолчанию — нет). */
  closeAfterEvents?: boolean;
}

function encodeEvent(enc: TextEncoder, evt: SseEvent): Uint8Array {
  if (evt.type === 'heartbeat') {
    // SSE-комментарий (строка, начинающаяся с ':') — не попадает в onmessage,
    // сбрасывает heartbeat-watchdog клиента (§7.7).
    return enc.encode(`: heartbeat ${Date.now()}\n\n`);
  }
  return enc.encode(`event: ${evt.type}\ndata: ${JSON.stringify(evt.data)}\n\n`);
}

const DEFAULT_EVENTS: SseEvent[] = [
  {
    type: 'status_update',
    delayMs: 10,
    data: {
      version_id: IDS.versions.alphaV1,
      status: 'ANALYZING',
      message: 'Юридический анализ',
      progress: 35,
    },
  },
  { type: 'heartbeat', delayMs: 50 },
];

export function createSseHandlers(base: HandlerBase, options: SseHandlerOptions = {}) {
  const events = options.events ?? DEFAULT_EVENTS;
  const closeAfterEvents = options.closeAfterEvents ?? false;

  return [
    http.get(joinPath(base, '/events/stream'), () => {
      const enc = new TextEncoder();
      let timers: ReturnType<typeof setTimeout>[] = [];
      // `closed` — явный флаг на случай, когда cancel() отработал, но
      // setTimeout-колбэк уже был поставлен в макротаск до clearTimeout.
      // Без флага возможен enqueue/close на уже-cancelled контроллере.
      let closed = false;
      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          for (const evt of events) {
            const delay = evt.delayMs ?? 0;
            const t = setTimeout(() => {
              if (closed) return;
              try {
                controller.enqueue(encodeEvent(enc, evt));
              } catch {
                // controller уже закрыт (unsubscribe на клиенте); игнор.
              }
            }, delay);
            timers.push(t);
          }
          if (closeAfterEvents) {
            const lastDelay = events.reduce((acc, e) => Math.max(acc, e.delayMs ?? 0), 0);
            const t = setTimeout(() => {
              if (closed) return;
              closed = true;
              try {
                controller.close();
              } catch {
                // уже закрыт
              }
            }, lastDelay + 1);
            timers.push(t);
          }
        },
        cancel() {
          closed = true;
          for (const t of timers) clearTimeout(t);
          timers = [];
        },
      });

      return new HttpResponse(stream, {
        status: 200,
        headers: {
          'content-type': 'text/event-stream; charset=utf-8',
          'cache-control': 'no-cache',
          connection: 'keep-alive',
        },
      });
    }),
  ];
}
