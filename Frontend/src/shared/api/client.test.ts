// Unit-тесты HTTP-клиента (§7.2-7.4 high-architecture).
// Покрывают: auth inject, X-Correlation-Id, 401 refresh race, 429 Retry-After,
// 502/503 GET backoff, network-error retry, ошибочная нормализация в OrchestratorError.
//
// MSW (node adapter) мокает HTTP поверх axios. Vitest environment='node' — ок,
// MSW v2 использует undici Interceptor API; fetch из axios маршрутится тем же
// механизмом (axios 1.7+ поддерживает undici через node adapter).
//
// Тайминги для retry/429 сокращены через vi.useFakeTimers + runAllTimersAsync.
import { http as mswHttp, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { sessionStore } from '@/shared/auth/session-store';

import { __resetForTests, createHttpClient, parseRetryAfter, setRefreshHandler } from './client';
import { OrchestratorError } from './errors';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterAll(() => server.close());
afterEach(() => {
  // useRealTimers до resetHandlers: некоторые cleanup-хуки MSW полагаются
  // на setTimeout, под fake-timers они могут зависнуть и «утечь» в следующий тест.
  vi.useRealTimers();
  server.resetHandlers();
  sessionStore.getState().clear();
  __resetForTests();
});

describe('request interceptor', () => {
  it('inject Authorization: Bearer {token} из sessionStore', async () => {
    sessionStore.getState().setAccess('tok-123', 3600);
    let seen: string | undefined;
    server.use(
      mswHttp.get(url('/users/me'), ({ request }) => {
        seen = request.headers.get('authorization') ?? undefined;
        return HttpResponse.json({ ok: true });
      }),
    );
    const http = createHttpClient(BASE);
    await http.get('/users/me');
    expect(seen).toBe('Bearer tok-123');
  });

  it('не перезаписывает Authorization, если заголовок передан явно', async () => {
    sessionStore.getState().setAccess('stored', 3600);
    let seen: string | undefined;
    server.use(
      mswHttp.get(url('/ping'), ({ request }) => {
        seen = request.headers.get('authorization') ?? undefined;
        return HttpResponse.json({});
      }),
    );
    const http = createHttpClient(BASE);
    await http.get('/ping', { headers: { Authorization: 'Bearer custom' } });
    expect(seen).toBe('Bearer custom');
  });

  it('генерирует X-Correlation-Id (UUID v4) если не передан', async () => {
    let seen: string | undefined;
    server.use(
      mswHttp.get(url('/ping'), ({ request }) => {
        seen = request.headers.get('x-correlation-id') ?? undefined;
        return HttpResponse.json({});
      }),
    );
    const http = createHttpClient(BASE);
    await http.get('/ping');
    // UUID v4 ABNF: 8-4-4-4-12 hex, version nibble=4 в 13-й позиции.
    expect(seen).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[0-9a-f]{4}-[0-9a-f]{12}$/i);
  });

  it('сохраняет переданный X-Correlation-Id', async () => {
    let seen: string | undefined;
    server.use(
      mswHttp.get(url('/ping'), ({ request }) => {
        seen = request.headers.get('x-correlation-id') ?? undefined;
        return HttpResponse.json({});
      }),
    );
    const http = createHttpClient(BASE);
    await http.get('/ping', { headers: { 'X-Correlation-Id': 'req-abc-123' } });
    expect(seen).toBe('req-abc-123');
  });
});

describe('401 AUTH_TOKEN_EXPIRED → shared-promise refresh', () => {
  it('вызывает refreshHandler и повторяет запрос со свежим токеном', async () => {
    sessionStore.getState().setAccess('old-token', 3600);
    const refreshFn = vi.fn(async () => {
      sessionStore.getState().setAccess('new-token', 3600);
      return 'new-token';
    });
    setRefreshHandler(refreshFn);

    const authHeadersSeen: string[] = [];
    let call = 0;
    server.use(
      mswHttp.get(url('/users/me'), ({ request }) => {
        authHeadersSeen.push(request.headers.get('authorization') ?? '');
        call += 1;
        if (call === 1) {
          return HttpResponse.json(
            {
              error_code: 'AUTH_TOKEN_EXPIRED',
              message: 'Токен истёк',
            },
            { status: 401 },
          );
        }
        return HttpResponse.json({ id: 'u-1' });
      }),
    );
    const http = createHttpClient(BASE);
    const res = await http.get<{ id: string }>('/users/me');

    expect(refreshFn).toHaveBeenCalledTimes(1);
    expect(res.data).toEqual({ id: 'u-1' });
    expect(authHeadersSeen[0]).toBe('Bearer old-token');
    expect(authHeadersSeen[1]).toBe('Bearer new-token');
  });

  it('shared-promise: N параллельных 401 → один refresh-вызов', async () => {
    sessionStore.getState().setAccess('old', 3600);
    let refreshCalls = 0;
    setRefreshHandler(async () => {
      refreshCalls += 1;
      await new Promise((r) => setTimeout(r, 10));
      sessionStore.getState().setAccess('fresh', 3600);
      return 'fresh';
    });

    const state = { firstWave: true };
    server.use(
      mswHttp.get(url('/users/me'), ({ request }) => {
        if (request.headers.get('authorization') === 'Bearer old' && state.firstWave) {
          return HttpResponse.json(
            { error_code: 'AUTH_TOKEN_EXPIRED', message: 'exp' },
            { status: 401 },
          );
        }
        return HttpResponse.json({ id: 'ok' });
      }),
    );
    const http = createHttpClient(BASE);
    const results = await Promise.all([
      http.get<{ id: string }>('/users/me'),
      http.get<{ id: string }>('/users/me'),
      http.get<{ id: string }>('/users/me'),
      http.get<{ id: string }>('/users/me'),
      http.get<{ id: string }>('/users/me'),
    ]);
    state.firstWave = false;
    expect(refreshCalls).toBe(1);
    results.forEach((r) => expect(r.data).toEqual({ id: 'ok' }));
  });

  it('refresh failure → OrchestratorError без second retry', async () => {
    sessionStore.getState().setAccess('old', 3600);
    setRefreshHandler(async () => {
      throw new OrchestratorError({
        error_code: 'AUTH_REFRESH_FAILED',
        message: 'Сессия завершена',
        status: 401,
      });
    });
    server.use(
      mswHttp.get(url('/users/me'), () =>
        HttpResponse.json({ error_code: 'AUTH_TOKEN_EXPIRED', message: 'exp' }, { status: 401 }),
      ),
    );
    const http = createHttpClient(BASE);
    await expect(http.get('/users/me')).rejects.toMatchObject({
      name: 'OrchestratorError',
      error_code: 'AUTH_REFRESH_FAILED',
    });
  });

  it('без зарегистрированного handler → AUTH_TOKEN_EXPIRED пробрасывается как OrchestratorError', async () => {
    sessionStore.getState().setAccess('tok', 3600);
    server.use(
      mswHttp.get(url('/x'), () =>
        HttpResponse.json({ error_code: 'AUTH_TOKEN_EXPIRED', message: 'exp' }, { status: 401 }),
      ),
    );
    const http = createHttpClient(BASE);
    await expect(http.get('/x')).rejects.toMatchObject({
      name: 'OrchestratorError',
      error_code: 'AUTH_TOKEN_EXPIRED',
      status: 401,
    });
  });

  it('401 с non-AUTH_TOKEN_EXPIRED кодом не вызывает refresh', async () => {
    const refreshFn = vi.fn();
    setRefreshHandler(refreshFn);
    server.use(
      mswHttp.get(url('/x'), () =>
        HttpResponse.json(
          { error_code: 'AUTH_TOKEN_INVALID', message: 'bad token' },
          { status: 401 },
        ),
      ),
    );
    const http = createHttpClient(BASE);
    await expect(http.get('/x')).rejects.toMatchObject({ error_code: 'AUTH_TOKEN_INVALID' });
    expect(refreshFn).not.toHaveBeenCalled();
  });

  it('повторный 401 после refresh → единичный throw, без петли', async () => {
    sessionStore.getState().setAccess('old', 3600);
    const refreshFn = vi.fn(async () => {
      sessionStore.getState().setAccess('fresh', 3600);
      return 'fresh';
    });
    setRefreshHandler(refreshFn);
    server.use(
      mswHttp.get(url('/x'), () =>
        HttpResponse.json({ error_code: 'AUTH_TOKEN_EXPIRED', message: 'exp' }, { status: 401 }),
      ),
    );
    const http = createHttpClient(BASE);
    await expect(http.get('/x')).rejects.toMatchObject({ error_code: 'AUTH_TOKEN_EXPIRED' });
    expect(refreshFn).toHaveBeenCalledTimes(1);
  });
});

describe('429 Retry-After', () => {
  it('ждёт Retry-After секунд и повторяет один раз', async () => {
    vi.useFakeTimers();
    let call = 0;
    server.use(
      mswHttp.get(url('/search'), () => {
        call += 1;
        if (call === 1) {
          return HttpResponse.json(
            { error_code: 'RATE_LIMIT_EXCEEDED', message: 'rate' },
            { status: 429, headers: { 'Retry-After': '2' } },
          );
        }
        return HttpResponse.json({ results: [] });
      }),
    );
    const http = createHttpClient(BASE);
    const p = http.get('/search');
    await vi.runAllTimersAsync();
    const res = await p;
    expect(call).toBe(2);
    expect(res.data).toEqual({ results: [] });
  });

  it('повторный 429 → OrchestratorError (только 1 retry)', async () => {
    vi.useFakeTimers();
    server.use(
      mswHttp.get(url('/search'), () =>
        HttpResponse.json(
          { error_code: 'RATE_LIMIT_EXCEEDED', message: 'rate' },
          { status: 429, headers: { 'Retry-After': '1' } },
        ),
      ),
    );
    const http = createHttpClient(BASE);
    const p = http.get('/search');
    const assertion = expect(p).rejects.toMatchObject({ error_code: 'RATE_LIMIT_EXCEEDED' });
    await vi.runAllTimersAsync();
    await assertion;
  });

  it('parseRetryAfter: integer секунд', () => {
    expect(parseRetryAfter('5')).toBe(5_000);
    expect(parseRetryAfter('0')).toBe(0);
  });

  it('parseRetryAfter: HTTP-date', () => {
    vi.useFakeTimers();
    const now = Date.parse('2026-04-17T10:00:00Z');
    vi.setSystemTime(new Date(now));
    // RFC 7231 HTTP-date имеет second-precision — используем ровную секунду,
    // чтобы избежать округления toUTCString().
    const future = new Date(now + 3_000).toUTCString();
    const parsed = parseRetryAfter(future);
    expect(parsed).toBe(3_000);
  });

  it('parseRetryAfter: fallback при пустом/невалидном', () => {
    expect(parseRetryAfter(null)).toBe(5_000);
    expect(parseRetryAfter('')).toBe(5_000);
    expect(parseRetryAfter('not-a-date')).toBe(5_000);
  });

  it('parseRetryAfter: clamp большого значения до 60s', () => {
    expect(parseRetryAfter('9999')).toBe(60_000);
  });
});

describe('502/503 GET retry (exponential backoff)', () => {
  it('502 GET: 3 попытки, успех на третьей', async () => {
    vi.useFakeTimers();
    let call = 0;
    server.use(
      mswHttp.get(url('/data'), () => {
        call += 1;
        if (call < 3) {
          return HttpResponse.json(
            { error_code: 'DM_UNAVAILABLE', message: 'down' },
            { status: 502 },
          );
        }
        return HttpResponse.json({ ok: true });
      }),
    );
    const http = createHttpClient(BASE);
    const p = http.get('/data');
    await vi.runAllTimersAsync();
    const res = await p;
    expect(call).toBe(3);
    expect(res.data).toEqual({ ok: true });
  });

  it('503 GET: после MAX_5XX_RETRIES → OrchestratorError', async () => {
    vi.useFakeTimers();
    let call = 0;
    server.use(
      mswHttp.get(url('/data'), () => {
        call += 1;
        return HttpResponse.json(
          { error_code: 'BROKER_UNAVAILABLE', message: 'queue down' },
          { status: 503 },
        );
      }),
    );
    const http = createHttpClient(BASE);
    const p = http.get('/data');
    const assertion = expect(p).rejects.toMatchObject({
      error_code: 'BROKER_UNAVAILABLE',
      status: 503,
    });
    await vi.runAllTimersAsync();
    await assertion;
    // Оригинал + 3 retry = 4 попытки.
    expect(call).toBe(4);
  });

  it('502 POST: без retry, сразу OrchestratorError (не-идемпотентный метод)', async () => {
    let call = 0;
    server.use(
      mswHttp.post(url('/contracts/upload'), () => {
        call += 1;
        return HttpResponse.json(
          { error_code: 'STORAGE_UNAVAILABLE', message: 'down' },
          { status: 502 },
        );
      }),
    );
    const http = createHttpClient(BASE);
    await expect(http.post('/contracts/upload', {})).rejects.toMatchObject({
      error_code: 'STORAGE_UNAVAILABLE',
    });
    expect(call).toBe(1);
  });
});

describe('Network error retry', () => {
  it('network error: 1 retry, успех на второй попытке', async () => {
    vi.useFakeTimers();
    let call = 0;
    server.use(
      mswHttp.get(url('/ping'), () => {
        call += 1;
        if (call === 1) return HttpResponse.error();
        return HttpResponse.json({ pong: true });
      }),
    );
    const http = createHttpClient(BASE);
    const p = http.get('/ping');
    await vi.runAllTimersAsync();
    const res = await p;
    expect(call).toBe(2);
    expect(res.data).toEqual({ pong: true });
  });

  it('повторный network error → OrchestratorError(NETWORK_ERROR)', async () => {
    vi.useFakeTimers();
    server.use(mswHttp.get(url('/ping'), () => HttpResponse.error()));
    const http = createHttpClient(BASE);
    const p = http.get('/ping');
    const assertion = expect(p).rejects.toMatchObject({
      name: 'OrchestratorError',
      error_code: 'NETWORK_ERROR',
    });
    await vi.runAllTimersAsync();
    await assertion;
  });

  it('timeout (ECONNABORTED) → OrchestratorError(TIMEOUT), без retry', async () => {
    let call = 0;
    server.use(
      mswHttp.get(url('/slow'), async () => {
        call += 1;
        await new Promise((r) => setTimeout(r, 50));
        return HttpResponse.json({});
      }),
    );
    const http = createHttpClient(BASE);
    // timeout override через per-request, чтобы не трогать глобальный 30s.
    await expect(http.get('/slow', { timeout: 5 })).rejects.toMatchObject({
      name: 'OrchestratorError',
      error_code: 'TIMEOUT',
    });
    // Timeout не ретраится — сервер получил ровно 1 запрос.
    expect(call).toBe(1);
  });
});

describe('Error normalization → OrchestratorError', () => {
  it('5xx с body ErrorResponse: поля переносятся 1:1', async () => {
    server.use(
      mswHttp.get(url('/x'), () =>
        HttpResponse.json(
          {
            error_code: 'INTERNAL_ERROR',
            message: 'Внутренняя ошибка',
            suggestion: 'Попробуйте позже',
            correlation_id: 'c-42',
          },
          { status: 500 },
        ),
      ),
    );
    const http = createHttpClient(BASE);
    try {
      await http.get('/x');
      throw new Error('should have thrown');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe('INTERNAL_ERROR');
      expect(err.message).toBe('Внутренняя ошибка');
      expect(err.suggestion).toBe('Попробуйте позже');
      expect(err.correlationId).toBe('c-42');
      expect(err.status).toBe(500);
    }
  });

  it('4xx VALIDATION_ERROR с details.fields', async () => {
    server.use(
      mswHttp.post(url('/contracts/upload'), () =>
        HttpResponse.json(
          {
            error_code: 'VALIDATION_ERROR',
            message: 'Проверьте поля',
            details: { fields: [{ field: 'title', code: 'REQUIRED', message: 'req' }] },
          },
          { status: 422 },
        ),
      ),
    );
    const http = createHttpClient(BASE);
    await expect(http.post('/contracts/upload', {})).rejects.toMatchObject({
      error_code: 'VALIDATION_ERROR',
      status: 422,
      details: { fields: [{ field: 'title', code: 'REQUIRED' }] },
    });
  });

  it('non-JSON тело → UNKNOWN_ERROR', async () => {
    server.use(
      mswHttp.get(url('/x'), () => HttpResponse.text('<html>500</html>', { status: 500 })),
    );
    const http = createHttpClient(BASE);
    await expect(http.get('/x')).rejects.toMatchObject({
      name: 'OrchestratorError',
      error_code: 'UNKNOWN_ERROR',
      status: 500,
    });
  });
});
