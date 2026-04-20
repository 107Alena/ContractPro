// @vitest-environment jsdom
//
// SettingsPage — 4 unit-теста (Default/Loading/Error+refetch/Logout click).
// Паттерн: мокаем useMe (@/entities/user) и useLogout (@/features/auth/logout)
// — прямой контроль за query-state. MemoryRouter оборачивает Link'и, которых
// в SettingsPage нет, но Router-контекст нужен на случай вложенных shared-ui,
// опирающихся на react-router (SimpleLink и т.п.).
import type { UseQueryResult } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

const logoutMock = vi.fn<[], Promise<void>>();
let logoutPendingMock = false;

vi.mock('@/features/auth/logout', () => ({
  useLogout: () => ({ logout: logoutMock, isPending: logoutPendingMock }),
}));

const useMeMock = vi.fn();
vi.mock('@/entities/user', () => ({
  useMe: () => useMeMock(),
}));

import { type UserProfile } from '@/entities/user';

import { SettingsPage } from './SettingsPage';

const baseUser: UserProfile = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'maria@example.com',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};

type MeResult = Partial<UseQueryResult<UserProfile>> & {
  refetch?: ReturnType<typeof vi.fn>;
};

function setMe(state: MeResult): void {
  useMeMock.mockReturnValue({
    isLoading: false,
    isFetching: false,
    data: undefined,
    error: null,
    refetch: vi.fn(),
    ...state,
  });
}

function renderPage(): void {
  render(
    <MemoryRouter>
      <SettingsPage />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  logoutMock.mockReset();
  logoutMock.mockResolvedValue();
  logoutPendingMock = false;
  useMeMock.mockReset();
});

describe('SettingsPage', () => {
  it('Default — отображает имя, email, организацию, роль и кнопку «Выйти»', () => {
    setMe({ data: baseUser });
    renderPage();

    expect(screen.getByRole('heading', { level: 1, name: /настройки/i })).toBeDefined();
    expect(screen.getByText('Мария Петрова')).toBeDefined();
    expect(screen.getByText('maria@example.com')).toBeDefined();
    expect(screen.getByText('ООО «Правовой центр»')).toBeDefined();
    expect(screen.getByText('Юрист')).toBeDefined();
    expect(screen.getByTestId('settings-logout-btn')).toBeDefined();
  });

  it('Loading — при isLoading=true и отсутствии data показывает spinner', () => {
    setMe({ isLoading: true });
    renderPage();
    expect(screen.getByTestId('settings-loading')).toBeDefined();
  });

  it('Error — alert и «Повторить» вызывают refetch', () => {
    const refetch = vi.fn();
    setMe({ error: new Error('net down'), refetch });
    renderPage();

    const alert = screen.getByTestId('settings-error');
    expect(alert.getAttribute('role')).toBe('alert');
    expect(screen.getByText(/не удалось загрузить профиль/i)).toBeDefined();

    fireEvent.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it('Logout — клик по «Выйти» вызывает logout()', async () => {
    setMe({ data: baseUser });
    renderPage();

    fireEvent.click(screen.getByTestId('settings-logout-btn'));
    await waitFor(() => {
      expect(logoutMock).toHaveBeenCalledTimes(1);
    });
  });

  it('Logout — при isPending=true кнопка disabled и повторный клик игнорируется', () => {
    logoutPendingMock = true;
    setMe({ data: baseUser });
    renderPage();

    const btn = screen.getByTestId('settings-logout-btn') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    expect(btn.getAttribute('aria-busy')).toBe('true');
    fireEvent.click(btn);
    expect(logoutMock).not.toHaveBeenCalled();
  });
});
