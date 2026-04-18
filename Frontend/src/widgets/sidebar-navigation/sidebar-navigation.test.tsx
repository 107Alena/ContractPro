// @vitest-environment jsdom
// Sidebar widget tests (§8.3 + acceptance criteria FE-TASK-032):
// — RBAC-фильтрация admin-секции через <Can>;
// — collapsed/expanded через Zustand layout-store;
// — mobile drawer открывается и автозакрывается при переходе;
// — активный пункт помечается NavLink aria-current="page".
import { act, cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { type User, type UserRole, useSession } from '@/shared/auth';
import { useLayoutStore } from '@/shared/layout';
import { TooltipProvider } from '@/shared/ui/tooltip';

import { SidebarNavigation } from './sidebar-navigation';

function makeUser(role: UserRole): User {
  return {
    user_id: '00000000-0000-0000-0000-000000000001',
    email: 'u@example.com',
    name: 'Тест',
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

beforeEach(resetStores);
afterEach(() => {
  cleanup();
  resetStores();
});

describe('<SidebarNavigation> — RBAC', () => {
  it('ORG_ADMIN видит admin-секцию (Политики + Чек-листы)', () => {
    renderSidebar('ORG_ADMIN', '/admin/policies');
    const desktop = screen.getByTestId('sidebar-desktop');
    // querySelector чтобы ограничить поиск desktop-aside-ом (Dialog-контент
    // монтируется в портал отдельно и в закрытом состоянии не рендерится).
    expect(desktop.querySelector('[data-testid="nav-admin-policies"]')).not.toBeNull();
    expect(desktop.querySelector('[data-testid="nav-admin-checklists"]')).not.toBeNull();
    expect(desktop.textContent).toContain('Администрирование');
  });

  it('LAWYER НЕ видит admin-секции', () => {
    renderSidebar('LAWYER');
    const desktop = screen.getByTestId('sidebar-desktop');
    expect(desktop.querySelector('[data-testid="nav-admin-policies"]')).toBeNull();
    expect(desktop.querySelector('[data-testid="nav-admin-checklists"]')).toBeNull();
    expect(desktop.textContent).not.toContain('Администрирование');
  });

  it('BUSINESS_USER НЕ видит admin-секции, но видит базовые пункты', () => {
    renderSidebar('BUSINESS_USER');
    const desktop = screen.getByTestId('sidebar-desktop');
    expect(desktop.querySelector('[data-testid="nav-admin-policies"]')).toBeNull();
    expect(desktop.querySelector('[data-testid="nav-dashboard"]')).not.toBeNull();
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

  it('collapsed rail имеет data-collapsed и aria-label у NavLink (для screen-readers)', () => {
    act(() => useLayoutStore.setState({ sidebarCollapsed: true }));
    renderSidebar('LAWYER');
    const aside = screen.getByTestId('sidebar-desktop');
    expect(aside.getAttribute('data-collapsed')).toBe('true');
    const contractsLink = aside.querySelector<HTMLElement>('[data-testid="nav-contracts"]');
    expect(contractsLink?.getAttribute('aria-label')).toBe('Контракты');
  });
});

describe('<SidebarNavigation> — активный пункт', () => {
  it('aria-current="page" на активном NavLink', () => {
    renderSidebar('LAWYER', '/contracts');
    const aside = screen.getByTestId('sidebar-desktop');
    const active = aside.querySelector('[data-testid="nav-contracts"]');
    expect(active?.getAttribute('aria-current')).toBe('page');
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

  it('клик по NavLink в drawer закрывает его (onNavigate)', async () => {
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
    const close = screen.getByLabelText('Закрыть меню');
    await user.click(close);
    expect(useLayoutStore.getState().mobileDrawerOpen).toBe(false);
  });
});
