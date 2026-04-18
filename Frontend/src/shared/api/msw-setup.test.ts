// @vitest-environment node
// Smoke-тест MSW-инфраструктуры (FE-TASK-054).
// Подтверждает, что глобальный server (tests/msw/server.ts) подключён через
// src/test-setup.ts и default-handlers матчат канонические endpoint'ы §17.1.
// Использует нативный fetch (MSW v2 node перехватывает undici), чтобы не
// зависеть от axios + jsdom adapter-selection (см. client.test.ts docstring).
import { http as mswHttp, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';

import { IDS } from '../../../tests/msw/fixtures';
import { server } from '../../../tests/msw/server';

const BASE = 'http://localhost/api/v1';

describe('MSW global server — default handlers', () => {
  it('GET /users/me → LAWYER из фикстур', async () => {
    const res = await fetch(`${BASE}/users/me`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { role: string; name: string };
    expect(body.role).toBe('LAWYER');
    expect(body.name).toBe('Алина Юрьева');
  });

  it('GET /contracts → список договоров с пагинацией', async () => {
    const res = await fetch(`${BASE}/contracts?page=1&size=20`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      items: unknown[];
      total: number;
      page: number;
      size: number;
    };
    expect(body.total).toBe(3);
    expect(body.items).toHaveLength(3);
    expect(body.page).toBe(1);
  });

  it('GET /contracts/{id} — happy path', async () => {
    const res = await fetch(`${BASE}/contracts/${IDS.contracts.alpha}`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { contract_id: string; title: string };
    expect(body.contract_id).toBe(IDS.contracts.alpha);
  });

  it('GET /contracts/{id} — 404 для неизвестного id', async () => {
    const res = await fetch(`${BASE}/contracts/unknown-id`);
    expect(res.status).toBe(404);
    const body = (await res.json()) as { error_code: string; correlation_id: string };
    expect(body.error_code).toBe('DOCUMENT_NOT_FOUND');
    expect(body.correlation_id).toMatch(/^[0-9a-f-]+$/i);
  });

  it('GET /contracts/{id}/versions/{vid}/results → AnalysisResults', async () => {
    const res = await fetch(
      `${BASE}/contracts/${IDS.contracts.alpha}/versions/${IDS.versions.alphaV1}/results`,
    );
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      status: string;
      risk_profile: { overall_level: string };
    };
    expect(body.status).toBe('READY');
    expect(body.risk_profile.overall_level).toBe('medium');
  });

  it('GET /export/pdf → 302 Redirect на presigned URL', async () => {
    const res = await fetch(
      `${BASE}/contracts/${IDS.contracts.alpha}/versions/${IDS.versions.alphaV1}/export/pdf`,
      { redirect: 'manual' },
    );
    expect(res.status).toBe(302);
    expect(res.headers.get('location')).toMatch(/^https:\/\/presigned\.example\//);
  });

  it('POST /auth/login happy + 400 на пустой body', async () => {
    const ok = await fetch(`${BASE}/auth/login`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ email: 'lawyer@example.com', password: 'secret' }),
    });
    expect(ok.status).toBe(200);
    const tokens = (await ok.json()) as { access_token: string; refresh_token: string };
    expect(tokens.access_token).toMatch(/eyJ/);

    const bad = await fetch(`${BASE}/auth/login`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({}),
    });
    expect(bad.status).toBe(400);
    const err = (await bad.json()) as { error_code: string; details?: { fields?: unknown[] } };
    expect(err.error_code).toBe('VALIDATION_ERROR');
    expect(err.details?.fields).toBeDefined();
  });

  it('server.use(...) переопределяет default — 500 на /contracts', async () => {
    server.use(
      mswHttp.get(`${BASE}/contracts`, () =>
        HttpResponse.json(
          { error_code: 'INTERNAL_ERROR', message: 'boom', correlation_id: 'c-1' },
          { status: 500 },
        ),
      ),
    );
    const res = await fetch(`${BASE}/contracts`);
    expect(res.status).toBe(500);
    const body = (await res.json()) as { error_code: string };
    expect(body.error_code).toBe('INTERNAL_ERROR');
  });
});

describe('SSE handler через ReadableStream', () => {
  // Явный timeout 2с — защита от регрессии в ReadableStream closeAfterEvents:
  // без него утечка держала бы test'ы до глобального 5с default'а.
  it('GET /events/stream — text/event-stream + payload status_update (closeAfterEvents)', { timeout: 2_000 }, async () => {
    // Закрываем stream после последнего event — иначе fetch-клиент может
    // ожидать дополнительных данных, а test-timeout сработает раньше.
    const { createSseHandlers } = await import('../../../tests/msw/handlers');
    server.use(
      ...createSseHandlers(BASE, {
        closeAfterEvents: true,
        events: [
          {
            type: 'status_update',
            delayMs: 0,
            data: { version_id: 'v-1', status: 'ANALYZING', message: 'Анализ' },
          },
        ],
      }),
    );
    const res = await fetch(`${BASE}/events/stream`);
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toMatch(/text\/event-stream/);
    // Читаем весь stream до закрытия. closeAfterEvents гарантирует конечность.
    const text = await res.text();
    expect(text).toContain('event: status_update');
    expect(text).toContain('"status":"ANALYZING"');
  });
});
