// @vitest-environment jsdom
import '@/shared/i18n/config';

import { act, cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const logoutMock = vi.fn<[], Promise<void>>();

vi.mock('@/processes/auth-flow', () => ({
  logout: () => logoutMock(),
}));

import { type User, type UserRole, useSession } from '@/shared/auth';
import { I18nProvider } from '@/shared/i18n';
import { useLayoutStore } from '@/shared/layout';

import { Topbar } from './topbar';

function makeUser(role: UserRole = 'LAWYER'): User {
  return {
    user_id: '00000000-0000-0000-0000-000000000001',
    email: 'demo@contractpro.ru',
    name: 'Демо Пользователь',
    role,
    organization_id: '00000000-0000-0000-0000-000000000002',
    organization_name: 'ООО Демо',
    permissions: { export_enabled: true },
  };
}

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
  logoutMock.mockResolvedValue();
  act(() => {
    useSession.setState({
      accessToken: 'demo',
      user: makeUser(),
      tokenExpiry: Date.now() + 3_600_000,
    });
    useLayoutStore.setState({ sidebarCollapsed: false, mobileDrawerOpen: false });
  });
  Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => true });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  act(() => {
    useSession.getState().clear();
    useLayoutStore.setState({ sidebarCollapsed: false, mobileDrawerOpen: false });
  });
});

describe('Topbar widget', () => {
  it('рендерит sticky-header с триггером user-menu', () => {
    renderTopbar();
    expect(screen.getByTestId('topbar')).toBeDefined();
    expect(screen.getByTestId('topbar-user-menu-trigger')).toBeDefined();
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

  it('открывает user-menu popover и показывает имя/роль/организацию', async () => {
    const user = userEvent.setup();
    renderTopbar();
    await user.click(screen.getByTestId('topbar-user-menu-trigger'));
    const content = await screen.findByTestId('topbar-user-menu-content');
    expect(content).toBeDefined();
    expect(content.textContent).toContain('Демо Пользователь');
    expect(content.textContent).toContain('demo@contractpro.ru');
    expect(content.textContent).toContain('ООО Демо');
    expect(content.textContent).toContain('Юрист');
  });

  it('клик «Выйти» вызывает processes/auth-flow logout()', async () => {
    const user = userEvent.setup();
    renderTopbar();
    await user.click(screen.getByTestId('topbar-user-menu-trigger'));
    const logoutBtn = await screen.findByTestId('topbar-logout');
    await user.click(logoutBtn);
    expect(logoutMock).toHaveBeenCalledTimes(1);
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

  it('не рендерит user-menu когда session.user отсутствует', () => {
    act(() => useSession.getState().clear());
    renderTopbar();
    expect(screen.queryByTestId('topbar-user-menu-trigger')).toBeNull();
  });
});
