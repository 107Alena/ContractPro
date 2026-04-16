// @vitest-environment jsdom
// <RequireRole> — route-guard (§5.6 Pattern A).
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { RequireRole } from './require-role';
import { type User, useSession } from './session-store';

const baseUser: User = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'u@example.com',
  name: 'Тест',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'Тест Орг',
  permissions: { export_enabled: false },
};

function renderAt(initialPath: string, allowedRoles: readonly User['role'][]) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/login" element={<div>Страница входа</div>} />
        <Route path="/403" element={<div>Недостаточно прав</div>} />
        <Route
          path="/admin/policies"
          element={
            <RequireRole roles={allowedRoles}>
              <div>Политики</div>
            </RequireRole>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  useSession.getState().clear();
});

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('<RequireRole>', () => {
  it('не-аутентифицированный → redirect на /login', () => {
    renderAt('/admin/policies', ['ORG_ADMIN']);
    expect(screen.getByText('Страница входа')).toBeTruthy();
    expect(screen.queryByText('Политики')).toBeNull();
  });

  it('роль вне whitelist → redirect на /403', () => {
    useSession.setState({ user: { ...baseUser, role: 'BUSINESS_USER' } });
    renderAt('/admin/policies', ['ORG_ADMIN']);
    expect(screen.getByText('Недостаточно прав')).toBeTruthy();
    expect(screen.queryByText('Политики')).toBeNull();
  });

  it('роль в whitelist → рендер children', () => {
    useSession.setState({ user: { ...baseUser, role: 'ORG_ADMIN' } });
    renderAt('/admin/policies', ['ORG_ADMIN']);
    expect(screen.getByText('Политики')).toBeTruthy();
  });

  it('несколько разрешённых ролей', () => {
    useSession.setState({ user: { ...baseUser, role: 'LAWYER' } });
    renderAt('/admin/policies', ['LAWYER', 'ORG_ADMIN']);
    expect(screen.getByText('Политики')).toBeTruthy();
  });
});
