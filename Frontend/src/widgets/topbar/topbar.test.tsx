// @vitest-environment jsdom
import '@/shared/i18n/config';

import { act, cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { I18nProvider } from '@/shared/i18n';
import { useLayoutStore } from '@/shared/layout';

import { Topbar } from './topbar';

function renderTopbar(props: Parameters<typeof Topbar>[0] = {}): void {
  render(
    <I18nProvider>
      <MemoryRouter>
        <Topbar {...props} />
      </MemoryRouter>
    </I18nProvider>,
  );
}

beforeEach(() => {
  act(() => {
    useLayoutStore.setState({ sidebarCollapsed: false, mobileDrawerOpen: false });
  });
  Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => true });
});

afterEach(() => {
  cleanup();
  act(() => {
    useLayoutStore.setState({ sidebarCollapsed: false, mobileDrawerOpen: false });
  });
});

describe('Topbar widget', () => {
  it('рендерит sticky-header', () => {
    renderTopbar();
    expect(screen.getByTestId('topbar')).toBeDefined();
  });

  it('рендерит mobile hamburger с aria-label', () => {
    renderTopbar();
    expect(screen.getByRole('button', { name: 'Открыть меню' })).toBeDefined();
  });

  it('hamburger-клик вызывает openMobileDrawer', async () => {
    const user = userEvent.setup();
    renderTopbar();
    expect(useLayoutStore.getState().mobileDrawerOpen).toBe(false);
    await user.click(screen.getByTestId('topbar-mobile-menu'));
    expect(useLayoutStore.getState().mobileDrawerOpen).toBe(true);
  });

  it('рендерит SearchInput когда передан prop search', () => {
    renderTopbar({ search: { value: '', onChange: () => undefined } });
    const search = screen.getByTestId('topbar-search');
    expect(search.tagName).toBe('INPUT');
    expect((search as HTMLInputElement).type).toBe('search');
  });

  it('показывает offline-баннер при forceOffline=true', () => {
    renderTopbar({ forceOffline: true });
    const banner = screen.getByTestId('topbar-offline-banner');
    expect(banner).toBeDefined();
    expect(banner.textContent).toContain('Нет соединения');
  });

  it('реагирует на события offline/online', () => {
    renderTopbar();
    expect(screen.queryByTestId('topbar-offline-banner')).toBeNull();
    act(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false });
      window.dispatchEvent(new Event('offline'));
    });
    expect(screen.getByTestId('topbar-offline-banner')).toBeDefined();
    act(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => true });
      window.dispatchEvent(new Event('online'));
    });
    expect(screen.queryByTestId('topbar-offline-banner')).toBeNull();
  });

  it('рендерит notifications-trigger когда withNotifications=true', async () => {
    const user = userEvent.setup();
    renderTopbar({ withNotifications: true });
    const trigger = screen.getByTestId('topbar-notifications-trigger');
    await user.click(trigger);
    expect(await screen.findByTestId('topbar-notifications-empty')).toBeDefined();
  });
});
