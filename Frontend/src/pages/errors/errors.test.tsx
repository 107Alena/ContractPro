// @vitest-environment jsdom
import '@/shared/i18n/config';

import { cleanup, render, screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { I18nProvider } from '@/shared/i18n';

import { Forbidden403 } from './Forbidden403';
import { NotFound404 } from './NotFound404';
import { Offline } from './Offline';
import { ServerError500 } from './ServerError500';
import { ErrorLayout } from './ui/ErrorLayout';

afterEach(() => {
  cleanup();
});

function wrap(ui: ReactElement) {
  return (
    <I18nProvider>
      <MemoryRouter initialEntries={['/']}>{ui}</MemoryRouter>
    </I18nProvider>
  );
}

describe('Error pages (403/404/500/offline)', () => {
  it('ErrorLayout рендерит code/title/description/children', () => {
    render(
      wrap(
        <ErrorLayout code="X" title="Ошибка" description="Описание">
          <button type="button">Повторить</button>
        </ErrorLayout>,
      ),
    );
    expect(screen.getByText('X')).toBeDefined();
    expect(screen.getByRole('heading', { name: 'Ошибка' })).toBeDefined();
    expect(screen.getByText('Описание')).toBeDefined();
    expect(screen.getByRole('button', { name: 'Повторить' })).toBeDefined();
  });

  it('Forbidden403 показывает код 403 и кнопку "На главную"', () => {
    render(wrap(<Forbidden403 />));
    expect(screen.getByText('403')).toBeDefined();
    expect(screen.getByRole('heading', { name: 'Недостаточно прав' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'На главную' })).toBeDefined();
  });

  it('NotFound404 показывает код 404 и CTA «К документам» + «На главную»', () => {
    render(wrap(<NotFound404 />));
    expect(screen.getByText('404')).toBeDefined();
    expect(screen.getByRole('heading', { name: 'Страница не найдена' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'К документам' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'На главную' })).toBeDefined();
  });

  it('ServerError500 показывает код 500 и кнопку reload', () => {
    render(wrap(<ServerError500 />));
    expect(screen.getByText('500')).toBeDefined();
    expect(screen.getByRole('heading', { name: 'Временные проблемы' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Обновить страницу' })).toBeDefined();
  });

  it('ServerError500 показывает correlation_id с кнопкой «Скопировать ID»', () => {
    render(
      <I18nProvider>
        <MemoryRouter initialEntries={[{ pathname: '/500', state: { correlationId: 'req-xyz' } }]}>
          <ServerError500 />
        </MemoryRouter>
      </I18nProvider>,
    );
    const block = screen.getByTestId('correlation-id');
    expect(block).toBeDefined();
    expect(block.textContent).toContain('req-xyz');
    expect(screen.getByTestId('copy-correlation-id')).toBeDefined();
    expect(screen.getByRole('button', { name: 'Скопировать ID' })).toBeDefined();
  });

  it('Offline показывает title и reload кнопку (нет кода); при online — CTA «Вернуться»', () => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false });
    const { unmount } = render(wrap(<Offline />));
    expect(screen.getByRole('heading', { name: 'Нет соединения' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Обновить страницу' })).toBeDefined();
    expect(screen.queryByTestId('offline-online-hint')).toBeNull();
    unmount();

    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => true });
    render(wrap(<Offline />));
    expect(screen.getByTestId('offline-online-hint')).toBeDefined();
    expect(screen.getByRole('button', { name: 'Вернуться' })).toBeDefined();
  });
});
