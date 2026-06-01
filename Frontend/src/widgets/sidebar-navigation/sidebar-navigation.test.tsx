// @vitest-environment jsdom
// Sidebar widget tests (§8.3 + FE-TASK-032 + этап 4.4 App Shell):
// — RBAC-фильтрация admin-пунктов (роль-based);
// — collapsed/expanded через Zustand layout-store;
// — mobile drawer открывается и автозакрывается при переходе;
// — активный пункт помечается aria-current="page" (с исключением /contracts/new);
// — профиль пользователя + logout переехали в сайдбар (Figma 85:40).
import { act, cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const logoutMock = vi.fn<[], Promise<void>>();
vi.mock('@/processes/auth-flow', () => ({
  logout: () => logoutMock(),
}));

import { type User, type UserRole, useSession } from '@/shared/auth';
import { useLayoutStore } from '@/shared/layout';
import { TooltipProvider } from '@/shared/ui/tooltip';

import { SidebarNavigation } from './sidebar-navigation';

function makeUser(role: UserRole): User {
  return {
    user_id: '00000000-0000-0000-0000-000000000001',
    email: 'u@example.com',
    name: 'Тест Пользователь',
    role,
    organization_id: '00000000-0000-0000-0000-000000000002',
    organization_name: 'Тест Орг',
    permissions: { export_enabled: false },
  };
}

function renderSidebar(role: UserRole | null, initialPath = '/dashboard') {
  if (role) {
    useSession.setState({
      accessToken: 'test',
      user: makeUser(role),
      tokenExpiry: Date.now() + 3_600_000,
    });
  }
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <TooltipProvider delayDuration={0}>
        <Routes>
          <Route
            path="*"
            element={
              <>
                <SidebarNavigation />
                <main data-testid="page-content">
                  <div>Текущий путь: {initialPath}</div>
                </main>
              </>
            }
          />
        </Routes>
      </TooltipProvider>
    </MemoryRouter>,
  );
}

function resetStores(): void {
  act(() => {
    useSession.getState().clear();
    useLayoutStore.setState({ sidebarCollapsed: false, mobileDrawerOpen: false });
  });
  localStorage.clear();
}

beforeEach(() => {
  logoutMock.mockResolvedValue();
  resetStores();
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  resetStores();
});

describe('<SidebarNavigation> — RBAC', () => {
  it('ORG_ADMIN видит admin-пункты (Политики + Чек-листы) в секции СИСТЕМА', () => {
    renderSidebar('ORG_ADMIN', '/admin/policies');
    const desktop = screen.getByTestId('sidebar-desktop');
    expect(desktop.querySelector('[data-testid="nav-admin-policies"]')).not.toBeNull();
    expect(desktop.querySelector('[data-testid="nav-admin-checklists"]')).not.toBeNull();
    expect(desktop.textContent).toContain('Политики');
    expect(desktop.textContent).toContain('СИСТЕМА');
  });

  it('LAWYER НЕ видит admin-пункты', () => {
    renderSidebar('LAWYER');
    const desktop = screen.getByTestId('sidebar-desktop');
    expect(desktop.querySelector('[data-testid="nav-admin-policies"]')).toBeNull();
    expect(desktop.querySelector('[data-testid="nav-admin-checklists"]')).toBeNull();
    expect(desktop.textContent).not.toContain('Политики');
  });

  it('BUSINESS_USER НЕ видит admin-пункты, но видит базовые', () => {
    renderSidebar('BUSINESS_USER');
    const desktop = screen.getByTestId('sidebar-desktop');
    expect(desktop.querySelector('[data-testid="nav-admin-policies"]')).toBeNull();
    expect(desktop.querySelector('[data-testid="nav-dashboard"]')).not.toBeNull();
    expect(desktop.querySelector('[data-testid="nav-new-check"]')).not.toBeNull();
    expect(desktop.querySelector('[data-testid="nav-contracts"]')).not.toBeNull();
    expect(desktop.querySelector('[data-testid="nav-reports"]')).not.toBeNull();
    expect(desktop.querySelector('[data-testid="nav-settings"]')).not.toBeNull();
  });

  it('Audit намеренно отсутствует в v1', () => {
    renderSidebar('ORG_ADMIN');
    expect(screen.getByTestId('sidebar-desktop').textContent).not.toContain('Аудит');
    expect(screen.queryByTestId('nav-audit')).toBeNull();
  });
});

describe('<SidebarNavigation> — collapsed/expanded toggle', () => {
  it('клик по toggle переключает sidebarCollapsed в store', async () => {
    const user = userEvent.setup();
    renderSidebar('LAWYER');
    const toggle = screen.getByTestId('sidebar-collapse-toggle');
    expect(useLayoutStore.getState().sidebarCollapsed).toBe(false);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');

    await user.click(toggle);
    expect(useLayoutStore.getState().sidebarCollapsed).toBe(true);
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
  });

  it('collapsed rail имеет data-collapsed и aria-label у nav-link', () => {
    act(() => useLayoutStore.setState({ sidebarCollapsed: true }));
    renderSidebar('LAWYER');
    const aside = screen.getByTestId('sidebar-desktop');
    expect(aside.getAttribute('data-collapsed')).toBe('true');
    const contractsLink = aside.querySelector<HTMLElement>('[data-testid="nav-contracts"]');
    expect(contractsLink?.getAttribute('aria-label')).toBe('Документы');
  });
});

describe('<SidebarNavigation> — активный пункт', () => {
  it('aria-current="page" на активном пункте /contracts', () => {
    renderSidebar('LAWYER', '/contracts');
    const aside = screen.getByTestId('sidebar-desktop');
    expect(aside.querySelector('[data-testid="nav-contracts"]')?.getAttribute('aria-current')).toBe(
      'page',
    );
  });

  it('на /contracts/new активна «Проверка договора», но НЕ «Документы»', () => {
    renderSidebar('LAWYER', '/contracts/new');
    const aside = screen.getByTestId('sidebar-desktop');
    expect(aside.querySelector('[data-testid="nav-new-check"]')?.getAttribute('aria-current')).toBe(
      'page',
    );
    expect(
      aside.querySelector('[data-testid="nav-contracts"]')?.getAttribute('aria-current'),
    ).toBeNull();
  });
});

describe('<SidebarNavigation> — профиль пользователя', () => {
  it('popover профиля показывает имя/email/роль/организацию', async () => {
    const user = userEvent.setup();
    renderSidebar('LAWYER');
    await user.click(screen.getByTestId('sidebar-user-trigger'));
    const content = await screen.findByTestId('sidebar-user-content');
    expect(content.textContent).toContain('Тест Пользователь');
    expect(content.textContent).toContain('u@example.com');
    expect(content.textContent).toContain('Юрист');
    expect(content.textContent).toContain('Тест Орг');
  });

  it('клик «Выйти» вызывает logout()', async () => {
    const user = userEvent.setup();
    renderSidebar('LAWYER');
    await user.click(screen.getByTestId('sidebar-user-trigger'));
    await user.click(await screen.findByTestId('sidebar-logout'));
    expect(logoutMock).toHaveBeenCalledTimes(1);
  });
});

describe('<SidebarNavigation> — mobile drawer', () => {
  it('drawer закрыт по умолчанию — Dialog.Content не в DOM', () => {
    renderSidebar('LAWYER');
    expect(screen.queryByTestId('sidebar-mobile')).toBeNull();
  });

  it('openMobileDrawer рендерит drawer c aria-label', () => {
    renderSidebar('LAWYER');
    act(() => useLayoutStore.getState().openMobileDrawer());
    const drawer = screen.getByTestId('sidebar-mobile');
    expect(drawer.getAttribute('aria-label')).toBe('Навигация приложения');
  });

  it('клик по nav-link в drawer закрывает его (onNavigate)', async () => {
    const user = userEvent.setup();
    renderSidebar('LAWYER');
    act(() => useLayoutStore.getState().openMobileDrawer());
    const drawer = screen.getByTestId('sidebar-mobile');
    const contracts = drawer.querySelector<HTMLElement>('[data-testid="nav-contracts"]');
    expect(contracts).not.toBeNull();
    await user.click(contracts!);
    expect(useLayoutStore.getState().mobileDrawerOpen).toBe(false);
  });

  it('кнопка «Закрыть меню» скрывает drawer', async () => {
    const user = userEvent.setup();
    renderSidebar('LAWYER');
    act(() => useLayoutStore.getState().openMobileDrawer());
    await user.click(screen.getByLabelText('Закрыть меню'));
    expect(useLayoutStore.getState().mobileDrawerOpen).toBe(false);
  });
});
