// Real-time SSE-подписка для кэша TanStack Query (§20.2 + §4.4
// high-architecture.md). Тонкая обёртка над `openEventStream` (§7.7):
// транспорт/reconnect/polling живут в `sse.ts`, здесь — только
// React-lifecycle + диспетчер побочных эффектов:
//   - setQueryData(qk.contracts.status(...))  — на каждое событие
//   - invalidateQueries(qk.contracts.results) — при READY
//   - toast.error                             — при FAILED/REJECTED
//   - toast.warning + onAwaitingUserInput     — при AWAITING_USER_INPUT
//
// Диспетчер вынесен в чистую `dispatchStatusEvent`, чтобы покрыть
// реакции unit-тестом без renderHook/jsdom. Хук остаётся lifecycle-only.
//
// Latest-ref pattern: колбэки и toast-api читаются через ref внутри
// подписки, чтобы пересоздание options-объекта в рендере родителя не
// рвало SSE-соединение. Подписка ресабскрайбится только при смене
// `documentId`/`versionId` (и `qc` — он стабилен в рамках провайдера).
import { type QueryClient, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';

import { toast as defaultToast } from '@/shared/ui/toast';

import { qk } from './query-keys';
import {
  openEventStream as defaultOpenEventStream,
  type OpenEventStreamFn,
  type TransportMode,
} from './sse';
import type { StatusEvent, UserProcessingStatus } from './sse-events';

type ToastApi = typeof defaultToast;

// Фолбэк-заголовки тостов на случай, если backend не прислал `message`.
// Короткие, без технических кодов — рядом с `description` (correlation_id).
// Для transient-статусов (UPLOADED/QUEUED/PROCESSING/ANALYZING/
// GENERATING_REPORTS/READY) тост не рендерится — эти статусы отражает
// `ProcessingProgress` widget.
// TODO(i18n): вынести в `shared/i18n/ru/sse.ts` с ключами `sse.fallback.*`
// после установки i18next-namespace-конвенции на уровне проекта.
const FALLBACK_TITLE: Partial<Record<UserProcessingStatus, string>> = {
  FAILED: 'Обработка завершилась ошибкой',
  REJECTED: 'Договор отклонён',
  PARTIALLY_FAILED: 'Обработка завершена частично',
  AWAITING_USER_INPUT: 'Требуется подтверждение типа договора',
};

export interface DispatchStatusEventDeps {
  qc: QueryClient;
  toast: ToastApi;
  onAwaitingUserInput?: (event: StatusEvent) => void;
}

/**
 * Чистая функция: применяет побочные эффекты одного `status_update` к
 * кэшу и UI. Экспортируется ради unit-тестов — хук делегирует сюда.
 * Не бросает на малформед-event'ы: один битый payload не должен ронять
 * подписку (backend — внешняя граница, defence-in-depth).
 */
export function dispatchStatusEvent(event: StatusEvent, deps: DispatchStatusEventDeps): void {
  if (!event || !event.document_id || !event.version_id) {
    return;
  }
  const { qc, toast, onAwaitingUserInput } = deps;
  const { document_id, version_id, status } = event;

  qc.setQueryData(qk.contracts.status(document_id, version_id), event);

  if (status === 'READY') {
    void qc.invalidateQueries({ queryKey: qk.contracts.results(document_id, version_id) });
    return;
  }

  if (status === 'FAILED' || status === 'REJECTED' || status === 'PARTIALLY_FAILED') {
    const title = pickTitle(event, status);
    const description = event.correlation_id
      ? `correlation_id: ${event.correlation_id}`
      : undefined;
    toast.error({ title, ...(description !== undefined && { description }) });
    return;
  }

  if (status === 'AWAITING_USER_INPUT') {
    const title = pickTitle(event, status);
    toast.warning({ title });
    onAwaitingUserInput?.(event);
    return;
  }
}

function pickTitle(event: StatusEvent, status: UserProcessingStatus): string {
  const trimmed = event.message?.trim();
  if (trimmed) return trimmed;
  return FALLBACK_TITLE[status] ?? 'Статус обновлён';
}

export interface UseEventStreamOptions {
  /** UUID версии — фильтр подписки + активация polling-fallback в `sse.ts`. */
  versionId?: string;
  /** Callback при AWAITING_USER_INPUT. Единственная точка низкой связности с
   *  модалкой LowConfidenceConfirm (event-bus появится позже). */
  onAwaitingUserInput?: (event: StatusEvent) => void;
  /** Диагностический callback (sse/polling) — пробрасывается в `openEventStream`. */
  onTransportChange?: (mode: TransportMode) => void;
  /**
   * @internal DI для unit-тестов — аналог `createHttpClient`/`createEventStreamOpener`.
   * Продакшен-code должен использовать дефолт — `openEventStream` из `shared/api/sse.ts`.
   */
  openEventStreamFn?: OpenEventStreamFn;
  /**
   * @internal DI для unit-тестов. В прод-коде — default из `shared/ui/toast`.
   */
  toast?: ToastApi;
}

/**
 * Подписка на SSE-стрим статуса обработки. Идентификаторы в deps → смена
 * документа/версии создаёт новое соединение; смена колбэков — нет.
 *
 * Без `documentId` подписка всё равно открывается — backend шлёт все
 * события по JWT (useful для dashboard-страницы со всеми документами).
 */
export function useEventStream(documentId?: string, options: UseEventStreamOptions = {}): void {
  const qc = useQueryClient();
  const optionsRef = useRef(options);
  // Latest-ref (TkDodo/Dan Abramov): мутация ref во время render допустима,
  // потому что все чтения происходят после коммита (в useEffect / emit'ах
  // подписки). В Strict Mode отброшенный рендер не портит корректность —
  // последующий коммит перезапишет ref свежим объектом options.
  optionsRef.current = options;

  const versionId = options.versionId;

  useEffect(() => {
    const current = optionsRef.current;
    const opener = current.openEventStreamFn ?? defaultOpenEventStream;
    return opener({
      ...(documentId !== undefined && { documentId }),
      ...(versionId !== undefined && { versionId }),
      onEvent: (event) => {
        const latest = optionsRef.current;
        dispatchStatusEvent(event, {
          qc,
          toast: latest.toast ?? defaultToast,
          ...(latest.onAwaitingUserInput !== undefined && {
            onAwaitingUserInput: latest.onAwaitingUserInput,
          }),
        });
      },
      onTransportChange: (mode) => {
        optionsRef.current.onTransportChange?.(mode);
      },
    });
  }, [documentId, versionId, qc]);
}
