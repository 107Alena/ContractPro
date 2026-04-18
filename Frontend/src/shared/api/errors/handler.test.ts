// Тесты toUserMessage (§20.4): приоритет server message, fallback на ERROL_UX,
// offline-detection, неизвестные коды, non-Orchestrator error.
// FE-TASK-054: использует единый global MSW-server (tests/msw/server.ts) —
// собственный setupServer() удалён во избежание двойного счёта interceptors.
import { http as mswHttp, HttpResponse } from 'msw';
import { describe, expect, it, vi } from 'vitest';

import { server } from '../../../../tests/msw/server';
import { createHttpClient } from '../client';
import { toUserMessage } from './handler';
import { OrchestratorError } from './orchestrator-error';

const BASE = 'http://orch.test/api/v1';

describe('toUserMessage — OrchestratorError', () => {
  it('предпочитает серверный message каталогу', () => {
    const err = new OrchestratorError({
      error_code: 'FILE_TOO_LARGE',
      message: 'Файл 25 МБ, лимит 20 МБ.',
      status: 413,
    });
    expect(toUserMessage(err).title).toBe('Файл 25 МБ, лимит 20 МБ.');
  });

  it('fallback на ERROR_UX.title при пустом серверном message', () => {
    const err = new OrchestratorError({
      error_code: 'AUTH_TOKEN_EXPIRED',
      message: '   ',
      status: 401,
    });
    expect(toUserMessage(err).title).toBe('Сессия истекла. Войдите заново.');
  });

  it('fallback на ERROR_UX.title при точно пустой строке (без пробелов)', () => {
    const err = new OrchestratorError({
      error_code: 'AUTH_TOKEN_MISSING',
      message: '',
    });
    expect(toUserMessage(err).title).toBe('Требуется вход в систему');
  });

  it('неизвестный код + пустой message → generic «Произошла ошибка»', () => {
    const err = new OrchestratorError({
      error_code: 'SOMETHING_NOT_IN_UNION',
      message: '',
    });
    expect(toUserMessage(err).title).toBe('Произошла ошибка');
  });

  it('hint: server suggestion имеет приоритет', () => {
    const err = new OrchestratorError({
      error_code: 'FILE_TOO_LARGE',
      message: 'Файл больше 20 МБ',
      suggestion: 'Конкретная подсказка от сервера.',
    });
    expect(toUserMessage(err).hint).toBe('Конкретная подсказка от сервера.');
  });

  it('hint: fallback на catalog.hint при пустом suggestion', () => {
    const err = new OrchestratorError({
      error_code: 'FILE_TOO_LARGE',
      message: 'Файл больше 20 МБ',
      suggestion: null,
    });
    expect(toUserMessage(err).hint).toBe('Сократите объём или разделите документ.');
  });

  it('action берётся только из каталога', () => {
    const err = new OrchestratorError({
      error_code: 'RATE_LIMIT_EXCEEDED',
      message: 'Слишком много запросов',
    });
    expect(toUserMessage(err).action).toBe('retry');
  });

  it('correlationId пробрасывается без изменений', () => {
    const err = new OrchestratorError({
      error_code: 'INTERNAL_ERROR',
      message: 'Боом',
      correlationId: 'c0rr-1234',
    });
    expect(toUserMessage(err).correlationId).toBe('c0rr-1234');
  });

  it('неизвестный код → title из серверного message, без action', () => {
    const err = new OrchestratorError({
      error_code: 'SOMETHING_NEW',
      message: 'Новая, неизвестная ошибка',
    });
    const result = toUserMessage(err);
    expect(result.title).toBe('Новая, неизвестная ошибка');
    expect(result.action).toBeUndefined();
  });
});

describe('toUserMessage — non-Orchestrator', () => {
  it('offline → navigator.onLine === false → спец-сообщение', () => {
    const spy = vi.spyOn(globalThis, 'navigator', 'get').mockReturnValue({
      onLine: false,
    } as Navigator);
    try {
      const result = toUserMessage(new TypeError('fetch failed'));
      expect(result.title).toBe('Нет соединения с интернетом');
      expect(result.action).toBe('retry');
    } finally {
      spy.mockRestore();
    }
  });

  it('generic Error → «Непредвиденная ошибка»', () => {
    const spy = vi.spyOn(globalThis, 'navigator', 'get').mockReturnValue({
      onLine: true,
    } as Navigator);
    try {
      const result = toUserMessage(new Error('oops'));
      expect(result.title).toBe('Непредвиденная ошибка');
      expect(result.action).toBe('retry');
    } finally {
      spy.mockRestore();
    }
  });

  it('примитивы не бросают (null, string, undefined)', () => {
    expect(() => toUserMessage(null)).not.toThrow();
    expect(() => toUserMessage('string error')).not.toThrow();
    expect(() => toUserMessage(undefined)).not.toThrow();
  });
});

describe('toUserMessage — интеграция с MSW error-response', () => {
  it('ErrorResponse из body нормализуется и попадает в UserMessage', async () => {
    server.use(
      mswHttp.get(`${BASE}/contracts/123`, () =>
        HttpResponse.json(
          {
            error_code: 'DOCUMENT_NOT_FOUND',
            message: 'Документ с id=123 не существует',
            correlation_id: 'mw-test-1',
          },
          { status: 404 },
        ),
      ),
    );

    const http = createHttpClient(BASE);
    let caught: unknown;
    try {
      await http.get('/contracts/123');
    } catch (e) {
      caught = e;
    }
    const msg = toUserMessage(caught);
    expect(msg.title).toBe('Документ с id=123 не существует');
    expect(msg.correlationId).toBe('mw-test-1');
  });
});
