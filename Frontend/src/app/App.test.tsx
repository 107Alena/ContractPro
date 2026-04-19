// @vitest-environment jsdom
import '@/shared/i18n/config';

import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { App } from './App';
import { AppErrorBoundary } from './providers/AppErrorBoundary';

beforeEach(() => {
  window.history.pushState({}, '', '/');
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('App composition root', () => {
  it('рендерит LandingPage для "/" (providers собраны без ошибок)', async () => {
    render(<App />);
    // LandingPage (FE-TASK-041) — использует data-testid="page-landing" вместо
    // зависимости от конкретного heading-текста, чтобы тест не ломался при
    // ребрендинге копий.
    expect(await screen.findByTestId('page-landing')).toBeDefined();
  });

  it('/403 рендерит Forbidden page', async () => {
    window.history.pushState({}, '', '/403');
    render(<App />);
    expect(await screen.findByText('403')).toBeDefined();
    expect(screen.getByRole('heading', { name: 'Недостаточно прав' })).toBeDefined();
  });

  it('/404 рендерит NotFound page', async () => {
    window.history.pushState({}, '', '/404');
    render(<App />);
    expect(await screen.findByText('404')).toBeDefined();
    expect(screen.getByRole('heading', { name: 'Страница не найдена' })).toBeDefined();
  });

  it('/offline рендерит Offline page', async () => {
    window.history.pushState({}, '', '/offline');
    render(<App />);
    expect(await screen.findByRole('heading', { name: 'Нет соединения' })).toBeDefined();
  });
});

describe('AppErrorBoundary (Sentry.ErrorBoundary + RouteError fallback)', () => {
  const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

  afterEach(() => {
    consoleErrorSpy.mockClear();
  });

  function Boom(): JSX.Element {
    throw new Error('boom: test_step #2');
  }

  it('ловит throw и показывает RouteError fallback (test_step #2)', () => {
    render(
      <AppErrorBoundary>
        <Boom />
      </AppErrorBoundary>,
    );
    // Без I18nProvider — useTranslation всё равно работает через уже инициализированный singleton i18n.
    expect(screen.getByRole('heading', { name: /Что-то пошло не так/ })).toBeDefined();
  });
});
