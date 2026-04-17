// @vitest-environment jsdom
//
// Интеграционный тест auth-flow через реальный axios-client + MSW (§5.4).
// Покрывает:
//  • happy login → setAccess + setUser из /users/me;
//  • doRefresh обновляет access-токен;
//  • shared-promise: 5 параллельных 401 → 1 /auth/refresh запрос;
//  • refresh-failure → soft-logout (clear store + sticky-toast + redirect);
//  • logout best-effort: серверная 500 не блокирует клиентский cleanup;
//  • logout без refresh-токена: пропускает POST /auth/logout.
import { http as mswHttp, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

import { queryClient } from '@/shared/api';
import { __resetForTests, createHttpClient, setRefreshHandler } from '@/shared/api/client';
import { sessionStore } from '@/shared/auth/session-store';
import { useToastStore } from '@/shared/ui/toast';

import {
  __resetNavigatorForTests,
  __setHttpForTests,
  doRefresh,
  login,
  logout,
  setNavigator,
  softLogout,
} from './actions';
import { getRefreshToken, setRefreshToken } from './refresh-token-storage';

// MSW node-adapter перехватывает Node-http/undici, а не XMLHttpRequest из jsdom.
// Используем явный http:// baseURL + отдельный http-инстанс (createHttpClient с
// forced adapter='http') и инжектим в actions через __setHttpForTests.
const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'http';

const server = setupServer();

const USER_FIXTURE = {
  user_id: '11111111-1111-1111-1111-111111111111',
  email: 'lawyer@example.com',
  name: 'Юрий Законов',
  role: 'LAWYER' as const,
  organization_id: '22222222-2222-2222-2222-222222222222',
  organization_name: 'ООО Ромашка',
  permissions: { export_enabled: true },
};

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterAll(() => server.close());

beforeEach(() => {
  __setHttpForTests(testHttp);
  // doRefresh регистрируется как handler в setup.ts; для тестов инжектим прямо.
  setRefreshHandler(doRefresh);
});

afterEach(() => {
  server.resetHandlers();
  sessionStore.getState().clear();
  window.sessionStorage.clear();
  useToastStore.getState().clear();
  queryClient.clear();
  __resetForTests();
  __resetNavigatorForTests();
  __setHttpForTests(null);
});

describe('login', () => {
  it('happy-path: сохраняет access+refresh, дёргает /users/me и кладёт profile', async () => {
    server.use(
      mswHttp.post(url('/auth/login'), () =>
        HttpResponse.json({
          access_token: 'access-abc',
          refresh_token: 'refresh-xyz',
          expires_in: 900,
        }),
      ),
      mswHttp.get(url('/users/me'), () => HttpResponse.json(USER_FIXTURE)),
    );

    const user = await login({ email: 'lawyer@example.com', password: 'secret' });

    expect(user.email).toBe('lawyer@example.com');
    expect(sessionStore.getState().accessToken).toBe('access-abc');
    expect(sessionStore.getState().user?.role).toBe('LAWYER');
    expect(getRefreshToken()).toBe('refresh-xyz');
    // tokenExpiry — абсолютный ms, ~ now + 900s.
    const expiry = sessionStore.getState().tokenExpiry ?? 0;
    expect(expiry - Date.now()).toBeGreaterThan(899_000);
    expect(expiry - Date.now()).toBeLessThan(901_000);
  });

  it('401 INVALID_CREDENTIALS → OrchestratorError, state не трогается', async () => {
    server.use(
      mswHttp.post(url('/auth/login'), () =>
        HttpResponse.json(
          { error_code: 'INVALID_CREDENTIALS', message: 'Неверный email или пароль' },
          { status: 401 },
        ),
      ),
    );

    await expect(login({ email: 'a@b.c', password: 'wrong' })).rejects.toMatchObject({
      name: 'OrchestratorError',
      error_code: 'INVALID_CREDENTIALS',
    });
    expect(sessionStore.getState().accessToken).toBeNull();
    expect(getRefreshToken()).toBeNull();
  });
});

describe('doRefresh', () => {
  it('happy-path: обновляет access-токен и rotate refresh', async () => {
    setRefreshToken('refresh-old');
    sessionStore.getState().setAccess('access-old', 10);

    server.use(
      mswHttp.post(url('/auth/refresh'), async ({ request }) => {
        const body = (await request.json()) as { refresh_token: string };
        expect(body.refresh_token).toBe('refresh-old');
        return HttpResponse.json({
          access_token: 'access-new',
          refresh_token: 'refresh-new',
          expires_in: 900,
        });
      }),
    );

    const token = await doRefresh();
    expect(token).toBe('access-new');
    expect(sessionStore.getState().accessToken).toBe('access-new');
    expect(getRefreshToken()).toBe('refresh-new');
  });

  it('нет refresh-токена → soft-logout + throw AUTH_REFRESH_FAILED', async () => {
    const nav = vi.fn();
    setNavigator(nav);

    await expect(doRefresh()).rejects.toMatchObject({ error_code: 'AUTH_REFRESH_FAILED' });
    expect(sessionStore.getState().accessToken).toBeNull();
    expect(nav).toHaveBeenCalledWith('/login');
    expect(useToastStore.getState().toasts).toHaveLength(1);
    expect(useToastStore.getState().toasts[0]).toMatchObject({
      variant: 'sticky',
      title: 'Сессия завершена',
    });
  });

  it('401 от /auth/refresh → soft-logout', async () => {
    setRefreshToken('refresh-bad');
    const nav = vi.fn();
    setNavigator(nav);

    server.use(
      mswHttp.post(url('/auth/refresh'), () =>
        HttpResponse.json(
          { error_code: 'AUTH_REFRESH_INVALID', message: 'Refresh отозван' },
          { status: 401 },
        ),
      ),
    );

    await expect(doRefresh()).rejects.toMatchObject({ error_code: 'AUTH_REFRESH_INVALID' });
    expect(sessionStore.getState().accessToken).toBeNull();
    expect(getRefreshToken()).toBeNull();
    expect(nav).toHaveBeenCalledWith('/login');
  });
});

describe('shared-promise refresh race (§5.4)', () => {
  it('5 параллельных 401 → один /auth/refresh запрос', async () => {
    setRefreshToken('rt-1');
    sessionStore.getState().setAccess('expired', 10);

    let refreshCalls = 0;
    server.use(
      mswHttp.post(url('/auth/refresh'), async () => {
        refreshCalls += 1;
        // Небольшая задержка, чтобы гарантированно сгруппировать параллельные 401.
        await new Promise((r) => setTimeout(r, 15));
        return HttpResponse.json({
          access_token: 'fresh',
          refresh_token: 'rt-2',
          expires_in: 900,
        });
      }),
      mswHttp.get(url('/contracts/:id'), ({ request }) => {
        const auth = request.headers.get('authorization');
        if (auth === 'Bearer expired') {
          return HttpResponse.json(
            { error_code: 'AUTH_TOKEN_EXPIRED', message: 'exp' },
            { status: 401 },
          );
        }
        return HttpResponse.json({ id: 'ok' });
      }),
    );

    const results = await Promise.all([
      testHttp.get('/contracts/1'),
      testHttp.get('/contracts/2'),
      testHttp.get('/contracts/3'),
      testHttp.get('/contracts/4'),
      testHttp.get('/contracts/5'),
    ]);
    expect(refreshCalls).toBe(1);
    results.forEach((r) => expect(r.data).toEqual({ id: 'ok' }));
    expect(sessionStore.getState().accessToken).toBe('fresh');
  });
});

describe('softLogout', () => {
  it('чистит всё клиентское, показывает sticky-toast, редиректит', () => {
    sessionStore.getState().setAccess('tok', 900);
    sessionStore.getState().setUser(USER_FIXTURE);
    setRefreshToken('rt');
    // Кладём что-то в queryClient, чтобы убедиться в cache-clear.
    queryClient.setQueryData(['test'], { value: 42 });
    const nav = vi.fn();
    setNavigator(nav);

    softLogout();

    expect(sessionStore.getState().accessToken).toBeNull();
    expect(sessionStore.getState().user).toBeNull();
    expect(getRefreshToken()).toBeNull();
    expect(queryClient.getQueryData(['test'])).toBeUndefined();
    expect(nav).toHaveBeenCalledWith('/login');
    expect(useToastStore.getState().toasts[0]).toMatchObject({
      variant: 'sticky',
      title: 'Сессия завершена',
    });
  });
});

describe('logout', () => {
  it('happy-path: POST /auth/logout с refresh-токеном, затем clear + redirect', async () => {
    setRefreshToken('rt-active');
    sessionStore.getState().setAccess('access-active', 900);
    const nav = vi.fn();
    setNavigator(nav);

    let logoutBody: unknown;
    server.use(
      mswHttp.post(url('/auth/logout'), async ({ request }) => {
        logoutBody = await request.json();
        return HttpResponse.json({ ok: true });
      }),
    );

    await logout();
    expect(logoutBody).toEqual({ refresh_token: 'rt-active' });
    expect(sessionStore.getState().accessToken).toBeNull();
    expect(getRefreshToken()).toBeNull();
    expect(nav).toHaveBeenCalledWith('/login');
    // logout НЕ показывает sticky-toast (явное действие юзера).
    expect(useToastStore.getState().toasts).toHaveLength(0);
  });

  it('серверная 500 на /auth/logout → клиентский cleanup всё равно выполняется', async () => {
    setRefreshToken('rt');
    sessionStore.getState().setAccess('tok', 900);
    const nav = vi.fn();
    setNavigator(nav);

    server.use(
      mswHttp.post(url('/auth/logout'), () =>
        HttpResponse.json(
          { error_code: 'INTERNAL_ERROR', message: 'down' },
          { status: 500 },
        ),
      ),
    );

    await logout();
    expect(sessionStore.getState().accessToken).toBeNull();
    expect(getRefreshToken()).toBeNull();
    expect(nav).toHaveBeenCalledWith('/login');
  });

  it('без refresh-токена: НЕ дёргает /auth/logout, clear + redirect всё равно', async () => {
    sessionStore.getState().setAccess('tok', 900);
    const nav = vi.fn();
    setNavigator(nav);

    let calls = 0;
    server.use(
      mswHttp.post(url('/auth/logout'), () => {
        calls += 1;
        return HttpResponse.json({ ok: true });
      }),
    );

    await logout();
    expect(calls).toBe(0);
    expect(sessionStore.getState().accessToken).toBeNull();
    expect(nav).toHaveBeenCalledWith('/login');
  });
});
