// AdminChecklistsPage.stories — placeholder для /admin/checklists (FE-TASK-001).
// Default — прямой рендер EmptyState. RoleRestricted — harness с MemoryRouter +
// <RequireRole> + гидрацией useSession, показывает редирект на /403 для LAWYER.
import type { Meta, StoryObj } from '@storybook/react';
import { useEffect } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { RequireRole, type User, type UserRole, useSession } from '@/shared/auth';

import { AdminChecklistsPage } from './AdminChecklistsPage';

function makeUser(role: UserRole): User {
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

const meta: Meta<typeof AdminChecklistsPage> = {
  title: 'Pages/Admin/Checklists',
  component: AdminChecklistsPage,
  parameters: { layout: 'fullscreen' },
};

export default meta;

type Story = StoryObj<typeof AdminChecklistsPage>;

export const Default: Story = {
  render: () => (
    <MemoryRouter>
      <AdminChecklistsPage />
    </MemoryRouter>
  ),
};

function RoleRestrictedHarness({ userRole }: { userRole: UserRole }): JSX.Element {
  useEffect(() => {
    useSession.setState({
      accessToken: 'demo-token',
      user: makeUser(userRole),
      tokenExpiry: Date.now() + 3_600_000,
    });
    return () => {
      useSession.getState().clear();
    };
  }, [userRole]);

  return (
    <MemoryRouter initialEntries={['/admin/checklists']}>
      <Routes>
        <Route path="/login" element={<div className="p-8 text-fg">Страница входа</div>} />
        <Route
          path="/403"
          element={
            <div className="p-8">
              <h1 className="text-2xl font-semibold text-fg">403 — Недостаточно прав</h1>
              <p className="text-fg-muted">
                Роль {userRole} не имеет доступа к административным разделам.
              </p>
            </div>
          }
        />
        <Route
          path="/admin/checklists"
          element={
            <RequireRole roles={['ORG_ADMIN']}>
              <AdminChecklistsPage />
            </RequireRole>
          }
        />
      </Routes>
    </MemoryRouter>
  );
}

export const RoleRestricted: Story = {
  name: 'RoleRestricted — LAWYER → /403',
  render: () => <RoleRestrictedHarness userRole="LAWYER" />,
};
