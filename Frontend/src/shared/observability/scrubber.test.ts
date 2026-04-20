import type { ErrorEvent } from '@sentry/react';
import { describe, expect, it } from 'vitest';

import { scrubSentryEvent } from './scrubber';

// ErrorEvent extends Event { type: undefined } — при exactOptionalPropertyTypes
// TS требует явного указания поля. Хелпер инлайнит это значение.
const makeEvent = (partial: Omit<Partial<ErrorEvent>, 'type'>): ErrorEvent =>
  ({ type: undefined, ...partial }) as ErrorEvent;

describe('scrubSentryEvent', () => {
  it('удаляет чувствительные заголовки в event.request.headers (case-insensitive)', () => {
    const event = makeEvent({
      request: {
        headers: {
          Authorization: 'Bearer eyJabc.def.ghi',
          'X-API-Key': 'k-secret-123',
          Cookie: 'session=abc',
          'Content-Type': 'application/json',
        },
      },
    });
    const scrubbed = scrubSentryEvent(event);
    expect(scrubbed.request?.headers).toEqual({
      Authorization: '[Filtered]',
      'X-API-Key': '[Filtered]',
      Cookie: '[Filtered]',
      'Content-Type': 'application/json',
    });
  });

  it('маскирует Bearer-токены в URL', () => {
    const event = makeEvent({
      request: { url: 'https://api.example.com/users?Authorization=Bearer eyJabc.def.ghi' },
    });
    const scrubbed = scrubSentryEvent(event);
    expect(scrubbed.request?.url).not.toContain('eyJabc.def.ghi');
    expect(scrubbed.request?.url).toContain('Bearer [Filtered]');
  });

  it('маскирует token/password/api_key в query string', () => {
    const event = makeEvent({
      request: { query_string: 'token=abc123&password=p%40ss&user=bob' },
    });
    const scrubbed = scrubSentryEvent(event);
    expect(scrubbed.request?.query_string).not.toContain('abc123');
    expect(scrubbed.request?.query_string).not.toContain('p%40ss');
    expect(scrubbed.request?.query_string).toContain('user=bob');
  });

  it('удаляет чувствительные ключи в event.request.data (body)', () => {
    const event = makeEvent({
      request: { data: { username: 'bob', password: 'secret', refresh_token: 'r-123' } },
    });
    const scrubbed = scrubSentryEvent(event);
    const data = scrubbed.request?.data as Record<string, unknown>;
    expect(data.username).toBe('bob');
    expect(data.password).toBe('[Filtered]');
    expect(data.refresh_token).toBe('[Filtered]');
  });

  it('редактирует вложенные ключи и Bearer-токены в breadcrumbs (data.headers)', () => {
    const event = makeEvent({
      breadcrumbs: [
        {
          category: 'fetch',
          type: 'http',
          data: {
            url: 'https://api.example.com/me',
            headers: { Authorization: 'Bearer abc.def.ghi' },
          },
        },
      ],
    });
    const scrubbed = scrubSentryEvent(event);
    const bc = scrubbed.breadcrumbs?.[0];
    const headers = (bc?.data as { headers: Record<string, string> }).headers;
    expect(headers.Authorization).toBe('[Filtered]');
  });

  it('маскирует JWT в свободном тексте (message, exception.value)', () => {
    const event = makeEvent({
      message: 'failed to refresh token eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyIjoiYm9iIn0.sig123',
      exception: {
        values: [{ type: 'Error', value: 'request failed with token eyJabc.def.ghi in header' }],
      },
    });
    const scrubbed = scrubSentryEvent(event);
    expect(scrubbed.message).not.toContain('eyJhbGciOiJIUzI1NiJ9');
    expect(scrubbed.message).toContain('[Filtered]');
    const excValue = scrubbed.exception?.values?.[0]?.value ?? '';
    expect(excValue).not.toContain('eyJabc.def.ghi');
    expect(excValue).toContain('[Filtered]');
  });

  it('обрабатывает циклические ссылки без бесконечной рекурсии', () => {
    const cyclic: Record<string, unknown> = { foo: 'bar' };
    cyclic.self = cyclic;
    const event = makeEvent({ extra: { payload: cyclic } });
    expect(() => scrubSentryEvent(event)).not.toThrow();
  });

  it('event без чувствительных данных остаётся неизменным по значимым полям', () => {
    const event = makeEvent({
      request: {
        url: 'https://api.example.com/health',
        headers: { 'Content-Type': 'application/json' },
      },
      message: 'user clicked button',
    });
    const scrubbed = scrubSentryEvent(event);
    expect(scrubbed.request?.url).toBe('https://api.example.com/health');
    expect(scrubbed.request?.headers).toEqual({ 'Content-Type': 'application/json' });
    expect(scrubbed.message).toBe('user clicked button');
  });

  it('событие без request/breadcrumbs/contexts/extra/message не ломается', () => {
    const event = makeEvent({});
    expect(() => scrubSentryEvent(event)).not.toThrow();
  });

  it('обрабатывает массив токенов в extra', () => {
    const event = makeEvent({ extra: { tokens: ['Bearer abc.def.ghi', 'random-string'] } });
    const scrubbed = scrubSentryEvent(event);
    const tokens = (scrubbed.extra?.tokens as string[]) ?? [];
    expect(tokens[0]).toContain('[Filtered]');
    expect(tokens[1]).toBe('random-string');
  });
});
