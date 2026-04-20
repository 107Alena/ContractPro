// @vitest-environment jsdom
import { type Span, trace } from '@opentelemetry/api';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import * as runtimeEnv from '@/shared/config/runtime-env';

import * as otel from './otel';

const { __resetOtelForTests, buildOtelConfig, enrichSpan, initOtel, tagXhrCorrelationId } = otel;

describe('buildOtelConfig', () => {
  it('без OTEL_ENDPOINT возвращает null (no-op для локального dev)', () => {
    expect(buildOtelConfig({}, 'abc123', 'development')).toBeNull();
    expect(buildOtelConfig({ OTEL_ENDPOINT: '' }, 'abc123', 'development')).toBeNull();
  });

  it('с endpoint возвращает descriptor с полными resource-атрибутами', () => {
    const config = buildOtelConfig(
      { OTEL_ENDPOINT: 'https://otel-collector.internal/v1/traces' },
      'deadbeef',
      'production',
    );
    expect(config).not.toBeNull();
    expect(config?.endpoint).toBe('https://otel-collector.internal/v1/traces');
    expect(config?.resourceAttributes['service.name']).toBe('contractpro-frontend');
    expect(config?.resourceAttributes['service.version']).toBe('deadbeef');
    expect(config?.resourceAttributes['deployment.environment']).toBe('production');
  });

  it('пустой gitSha — service.version не попадает в resource', () => {
    const config = buildOtelConfig(
      { OTEL_ENDPOINT: 'https://otel-collector.internal/v1/traces' },
      '',
      'development',
    );
    expect(config?.resourceAttributes['service.version']).toBeUndefined();
  });
});

describe('enrichSpan', () => {
  function makeSpanMock(): Span & { attributes: Record<string, unknown> } {
    const attributes: Record<string, unknown> = {};
    return {
      setAttribute(key: string, value: unknown) {
        attributes[key] = value;
        return this;
      },
      attributes,
    } as unknown as Span & { attributes: Record<string, unknown> };
  }

  beforeEach(async () => {
    // Сбрасываем session-store между тестами (module-level zustand singleton).
    const { sessionStore } = await import('@/shared/auth/session-store');
    sessionStore.getState().clear();
  });

  it('без авторизации не выставляет app.user_role / app.org_id', () => {
    const span = makeSpanMock();
    const request = new Request('http://localhost/api/v1/ping');
    enrichSpan(span, request);
    expect(span.attributes['app.user_role']).toBeUndefined();
    expect(span.attributes['app.org_id']).toBeUndefined();
  });

  it('при активной сессии заполняет app.user_role и app.org_id', async () => {
    const { sessionStore } = await import('@/shared/auth/session-store');
    sessionStore.setState({
      accessToken: 'xxx',
      user: {
        user_id: 'u-1',
        email: 'law@example.com',
        name: 'Lawyer',
        role: 'LAWYER',
        organization_id: 'org-42',
        organization_name: 'Acme',
        permissions: { export_enabled: true },
      },
      tokenExpiry: Date.now() + 10_000,
    });
    const span = makeSpanMock();
    enrichSpan(span, new Request('http://localhost/api/v1/ping'));
    expect(span.attributes['app.user_role']).toBe('LAWYER');
    expect(span.attributes['app.org_id']).toBe('org-42');
  });

  it('Fetch Request: читает X-Correlation-Id → app.correlation_id', () => {
    const span = makeSpanMock();
    const request = new Request('http://localhost/api/v1/ping', {
      headers: { 'X-Correlation-Id': 'req-abc-123' },
    });
    enrichSpan(span, request);
    expect(span.attributes['app.correlation_id']).toBe('req-abc-123');
  });

  it('RequestInit с Headers: извлекает correlation_id', () => {
    const span = makeSpanMock();
    const init: RequestInit = { headers: new Headers({ 'X-Correlation-Id': 'req-ini-1' }) };
    enrichSpan(span, init);
    expect(span.attributes['app.correlation_id']).toBe('req-ini-1');
  });

  it('RequestInit с plain-object headers: извлекает correlation_id (любой регистр)', () => {
    const span = makeSpanMock();
    enrichSpan(span, { headers: { 'x-correlation-id': 'req-lower' } });
    expect(span.attributes['app.correlation_id']).toBe('req-lower');
  });

  it('XMLHttpRequest: tagXhrCorrelationId → enrichSpan читает app.correlation_id', () => {
    const xhr = new XMLHttpRequest();
    xhr.open('GET', '/api/v1/ping');
    tagXhrCorrelationId(xhr, 'req-xhr-42');
    const span = makeSpanMock();
    enrichSpan(span, xhr);
    expect(span.attributes['app.correlation_id']).toBe('req-xhr-42');
  });

  it('app.http_path = URL.pathname для Request (не http.route — semconv требует шаблон)', () => {
    const span = makeSpanMock();
    enrichSpan(span, new Request('https://api.example.com/api/v1/contracts'));
    expect(span.attributes['app.http_path']).toBe('/api/v1/contracts');
    expect(span.attributes['http.route']).toBeUndefined();
  });
});

describe('initOtel', () => {
  beforeEach(() => {
    __resetOtelForTests();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    __resetOtelForTests();
    vi.restoreAllMocks();
  });

  it('возвращает { enabled: false } без OTEL_ENDPOINT', () => {
    vi.spyOn(runtimeEnv, 'getRuntimeEnv').mockReturnValue({});
    expect(initOtel().enabled).toBe(false);
  });

  it('возвращает { enabled: true } когда OTEL_ENDPOINT задан и регистрирует провайдер', () => {
    vi.spyOn(runtimeEnv, 'getRuntimeEnv').mockReturnValue({
      OTEL_ENDPOINT: 'https://otel-collector.internal/v1/traces',
    });
    const result = initOtel();
    expect(result.enabled).toBe(true);
    // provider.register() ставит глобальный tracer → getTracer возвращает реальный Tracer.
    const tracer = trace.getTracer('test');
    expect(tracer).toBeDefined();
  });

  it('идемпотентен при повторном вызове (StrictMode guard)', () => {
    vi.spyOn(runtimeEnv, 'getRuntimeEnv').mockReturnValue({
      OTEL_ENDPOINT: 'https://otel-collector.internal/v1/traces',
    });
    expect(initOtel().enabled).toBe(true);
    expect(initOtel().enabled).toBe(true);
  });
});
