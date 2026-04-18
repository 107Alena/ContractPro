import type { Meta, StoryObj } from '@storybook/react';
import { useEffect } from 'react';
import { MemoryRouter } from 'react-router-dom';

import { type User, type UserRole, useSession } from '@/shared/auth';
import { useLayoutStore } from '@/shared/layout';
import { TooltipProvider } from '@/shared/ui/tooltip';

import { SidebarNavigation } from './sidebar-navigation';

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

interface StoryArgs {
  role: UserRole;
  collapsed: boolean;
  initialPath: string;
}

function SidebarHarness({ role, collapsed, initialPath }: StoryArgs): JSX.Element {
  // Гидрация store + session выполняется синхронно внутри useEffect,
  // чтобы каждая story была изолированной (reset между рендерами).
  useEffect(() => {
    useSession.setState({
      accessToken: 'demo-token',
      user: makeUser(role),
      tokenExpiry: Date.now() + 3_600_000,
    });
    useLayoutStore.setState({ sidebarCollapsed: collapsed, mobileDrawerOpen: false });
    return () => {
      useSession.getState().clear();
      useLayoutStore.setState({ sidebarCollapsed: false, mobileDrawerOpen: false });
    };
  }, [role, collapsed]);

  return (
    <MemoryRouter initialEntries={[initialPath]}>
      <TooltipProvider delayDuration={200}>
        <div className="flex h-[600px] bg-bg-muted">
          <SidebarNavigation forceCollapsed={collapsed} />
          <div className="flex-1 p-6 text-sm text-fg-muted">
            <div className="mb-2 font-semibold text-fg">Контент страницы</div>
            <div>Активный путь: {initialPath}</div>
            <div>Роль: {role}</div>
          </div>
        </div>
      </TooltipProvider>
    </MemoryRouter>
  );
}

const meta = {
  title: 'Widgets/SidebarNavigation',
  component: SidebarHarness,
  parameters: { layout: 'fullscreen' },
  tags: ['autodocs'],
  argTypes: {
    role: { control: 'select', options: ['LAWYER', 'BUSINESS_USER', 'ORG_ADMIN'] },
    collapsed: { control: 'boolean' },
    initialPath: {
      control: 'select',
      options: ['/dashboard', '/contracts', '/reports', '/settings', '/admin/policies'],
    },
  },
  args: {
    role: 'LAWYER',
    collapsed: false,
    initialPath: '/dashboard',
  },
} satisfies Meta<typeof SidebarHarness>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Expanded: Story = {
  args: { collapsed: false, role: 'LAWYER', initialPath: '/contracts' },
};

export const Collapsed: Story = {
  args: { collapsed: true, role: 'LAWYER', initialPath: '/contracts' },
};

export const AsLawyer: Story = {
  name: 'Role — LAWYER',
  args: { role: 'LAWYER', collapsed: false, initialPath: '/dashboard' },
};

export const AsBusinessUser: Story = {
  name: 'Role — BUSINESS_USER (без admin-секции)',
  args: { role: 'BUSINESS_USER', collapsed: false, initialPath: '/contracts' },
};

export const AsOrgAdmin: Story = {
  name: 'Role — ORG_ADMIN (видит Политики и Чек-листы)',
  args: { role: 'ORG_ADMIN', collapsed: false, initialPath: '/admin/policies' },
};
