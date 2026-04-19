// AdminPoliciesPage.stories — placeholder для /admin/policies (FE-TASK-001).
// Default — прямой рендер EmptyState. RoleRestricted — harness с MemoryRouter +
// <RequireRole> + гидрацией useSession, показывает редирект на /403 для BUSINESS_USER.
import type { Meta, StoryObj } from '@storybook/react';
import { useEffect } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { RequireRole, type User, type UserRole, useSession } from '@/shared/auth';

import { AdminPoliciesPage } from './AdminPoliciesPage';

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

const meta: Meta<typeof AdminPoliciesPage> = {
  title: 'Pages/Admin/Policies',
  component: AdminPoliciesPage,
  parameters: { layout: 'fullscreen' },
};

export default meta;

type Story = StoryObj<typeof AdminPoliciesPage>;

export const Default: Story = {
  render: () => (
    <MemoryRouter>
      <AdminPoliciesPage />
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
    <MemoryRouter initialEntries={['/admin/policies']}>
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
          path="/admin/policies"
          element={
            <RequireRole roles={['ORG_ADMIN']}>
              <AdminPoliciesPage />
            </RequireRole>
          }
        />
      </Routes>
    </MemoryRouter>
  );
}

export const RoleRestricted: Story = {
  name: 'RoleRestricted — BUSINESS_USER → /403',
  render: () => <RoleRestrictedHarness userRole="BUSINESS_USER" />,
};
