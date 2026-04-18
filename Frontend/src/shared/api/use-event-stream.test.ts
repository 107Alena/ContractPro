// Unit-тесты диспетчера status_update → побочные эффекты (§20.2
// high-architecture.md). Логика вынесена в чистую `dispatchStatusEvent`,
// поэтому React/jsdom не требуется. Хук-уровневые тесты —
// `use-event-stream.hook.test.tsx` (jsdom + renderHook).
import { QueryClient } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { toast as toastApi } from '@/shared/ui/toast';

import { qk } from './query-keys';
import type { StatusEvent, UserProcessingStatus } from './sse-events';
import { dispatchStatusEvent } from './use-event-stream';

type ToastApi = typeof toastApi;
type ToastMock = {
  [K in keyof ToastApi]: ReturnType<typeof vi.fn<Parameters<ToastApi[K]>, ReturnType<ToastApi[K]>>>;
};

function makeToast(): ToastMock {
  // vi.fn без ReturnType-параметра возвращает undefined — для `.error`/`.warning`
  // это совпадает с сигнатурой `() => string` в рантайме тестов (TS ок через
  // Mock<..., string>-generic). toast.dismiss/clear возвращают void.
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

function makeEvent(overrides: Partial<StatusEvent> = {}): StatusEvent {
  return {
    document_id: 'doc-1',
    version_id: 'ver-1',
    status: 'PROCESSING',
    ...overrides,
  };
}

describe('dispatchStatusEvent — setQueryData', () => {
  let qc: QueryClient;
  let toast: ToastMock;

  beforeEach(() => {
    qc = new QueryClient();
    toast = makeToast();
  });

  it('пишет event в qk.contracts.status на любом событии', () => {
    const event = makeEvent({ status: 'ANALYZING' });
    dispatchStatusEvent(event, { qc, toast });
    // TanStack Query structurally-клонирует data; сравниваем по значению.
    expect(qc.getQueryData(qk.contracts.status('doc-1', 'ver-1'))).toStrictEqual(event);
  });

  it('каждый повторный event перезаписывает status-кэш', () => {
    dispatchStatusEvent(makeEvent({ status: 'QUEUED' }), { qc, toast });
    const second = makeEvent({ status: 'PROCESSING' });
    dispatchStatusEvent(second, { qc, toast });
    expect(qc.getQueryData(qk.contracts.status('doc-1', 'ver-1'))).toStrictEqual(second);
  });
});

describe('dispatchStatusEvent — READY', () => {
  it('invalidateQueries(results) вызывается при status=READY', () => {
    const qc = new QueryClient();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const toast = makeToast();
    dispatchStatusEvent(makeEvent({ status: 'READY' }), { qc, toast });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: qk.contracts.results('doc-1', 'ver-1'),
    });
    expect(toast.error).not.toHaveBeenCalled();
  });

  it('setQueryData вызывается ДО invalidate (порядок важен для UI)', () => {
    const qc = new QueryClient();
    const order: string[] = [];
    vi.spyOn(qc, 'setQueryData').mockImplementation((...args) => {
      order.push('setQueryData');
      return args[1];
    });
    vi.spyOn(qc, 'invalidateQueries').mockImplementation(async () => {
      order.push('invalidateQueries');
    });
    dispatchStatusEvent(makeEvent({ status: 'READY' }), { qc, toast: makeToast() });
    expect(order).toEqual(['setQueryData', 'invalidateQueries']);
  });
});

describe('dispatchStatusEvent — FAILED/REJECTED', () => {
  it.each<UserProcessingStatus>(['FAILED', 'REJECTED', 'PARTIALLY_FAILED'])(
    'toast.error для %s',
    (status) => {
      const qc = new QueryClient();
      const toast = makeToast();
      dispatchStatusEvent(makeEvent({ status, message: 'Что-то сломалось' }), { qc, toast });
      expect(toast.error).toHaveBeenCalledTimes(1);
      expect(toast.error).toHaveBeenCalledWith(
        expect.objectContaining({ title: 'Что-то сломалось' }),
      );
    },
  );

  it('correlation_id → description', () => {
    const qc = new QueryClient();
    const toast = makeToast();
    dispatchStatusEvent(
      makeEvent({
        status: 'FAILED',
        message: 'Ошибка',
        correlation_id: 'req-abc-123',
      }),
      { qc, toast },
    );
    expect(toast.error).toHaveBeenCalledWith({
      title: 'Ошибка',
      description: 'correlation_id: req-abc-123',
    });
  });

  it('без correlation_id — description отсутствует', () => {
    const qc = new QueryClient();
    const toast = makeToast();
    dispatchStatusEvent(makeEvent({ status: 'FAILED', message: 'Ошибка' }), { qc, toast });
    expect(toast.error).toHaveBeenCalledWith({ title: 'Ошибка' });
  });

  it('без message — fallback-title по статусу', () => {
    const qc = new QueryClient();
    const toast = makeToast();
    dispatchStatusEvent(makeEvent({ status: 'FAILED' }), { qc, toast });
    expect(toast.error).toHaveBeenCalledWith({ title: 'Обработка завершилась ошибкой' });
  });

  it('пустой message (только пробелы) → fallback', () => {
    const qc = new QueryClient();
    const toast = makeToast();
    dispatchStatusEvent(makeEvent({ status: 'REJECTED', message: '   ' }), { qc, toast });
    expect(toast.error).toHaveBeenCalledWith({ title: 'Договор отклонён' });
  });

  it('invalidate НЕ вызывается на FAILED', () => {
    const qc = new QueryClient();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    dispatchStatusEvent(makeEvent({ status: 'FAILED' }), { qc, toast: makeToast() });
    expect(invalidateSpy).not.toHaveBeenCalled();
  });
});

describe('dispatchStatusEvent — AWAITING_USER_INPUT', () => {
  it('toast.warning + onAwaitingUserInput callback', () => {
    const qc = new QueryClient();
    const toast = makeToast();
    const onAwaitingUserInput = vi.fn();
    const event = makeEvent({ status: 'AWAITING_USER_INPUT' });
    dispatchStatusEvent(event, { qc, toast, onAwaitingUserInput });
    expect(toast.warning).toHaveBeenCalledWith({
      title: 'Требуется подтверждение типа договора',
    });
    expect(onAwaitingUserInput).toHaveBeenCalledWith(event);
    expect(toast.error).not.toHaveBeenCalled();
  });

  it('без onAwaitingUserInput — только toast.warning, без throw', () => {
    const qc = new QueryClient();
    const toast = makeToast();
    expect(() => {
      dispatchStatusEvent(makeEvent({ status: 'AWAITING_USER_INPUT' }), { qc, toast });
    }).not.toThrow();
    expect(toast.warning).toHaveBeenCalledTimes(1);
  });
});

describe('dispatchStatusEvent — transient статусы (UPLOADED..GENERATING_REPORTS)', () => {
  it.each<UserProcessingStatus>([
    'UPLOADED',
    'QUEUED',
    'PROCESSING',
    'ANALYZING',
    'GENERATING_REPORTS',
  ])('не шлёт тост и не инвалидирует для %s', (status) => {
    const qc = new QueryClient();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const toast = makeToast();
    dispatchStatusEvent(makeEvent({ status }), { qc, toast });
    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
    expect(toast.warning).not.toHaveBeenCalled();
  });
});

describe('dispatchStatusEvent — malformed events', () => {
  it('без document_id — no-op', () => {
    const qc = new QueryClient();
    const setSpy = vi.spyOn(qc, 'setQueryData');
    const toast = makeToast();
    dispatchStatusEvent({ document_id: '', version_id: 'v', status: 'FAILED' } as StatusEvent, {
      qc,
      toast,
    });
    expect(setSpy).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it('без version_id — no-op', () => {
    const qc = new QueryClient();
    const setSpy = vi.spyOn(qc, 'setQueryData');
    dispatchStatusEvent({ document_id: 'd', version_id: '', status: 'READY' } as StatusEvent, {
      qc,
      toast: makeToast(),
    });
    expect(setSpy).not.toHaveBeenCalled();
  });
});
