import type { Meta, StoryObj } from '@storybook/react';
import { useEffect, useState } from 'react';
import { MemoryRouter } from 'react-router-dom';

import { type User, type UserRole, useSession } from '@/shared/auth';

import { Topbar } from './topbar';

function makeUser(role: UserRole): User {
  return {
    user_id: '00000000-0000-0000-0000-000000000001',
    email: 'demo@contractpro.ru',
    name: 'Анна Сергеевна Иванова',
    role,
    organization_id: '00000000-0000-0000-0000-000000000002',
    organization_name: 'ООО «Промышленный холдинг»',
    permissions: { export_enabled: true },
  };
}

interface StoryArgs {
  role: UserRole;
  withSearch: boolean;
  withNotifications: boolean;
  forceOffline: boolean;
  userMenuOpen: boolean;
}

function TopbarHarness({
  role,
  withSearch,
  withNotifications,
  forceOffline,
  userMenuOpen,
}: StoryArgs): JSX.Element {
  const [query, setQuery] = useState('');

  useEffect(() => {
    useSession.setState({
      accessToken: 'demo',
      user: makeUser(role),
      tokenExpiry: Date.now() + 3_600_000,
    });
    return () => {
      useSession.getState().clear();
    };
  }, [role]);

  const topbarProps: Parameters<typeof Topbar>[0] = {
    withNotifications,
    forceOffline,
    userMenuProps: { defaultOpen: userMenuOpen },
  };
  if (withSearch) {
    topbarProps.search = { value: query, onChange: setQuery };
  }

  return (
    <MemoryRouter>
      <div className="min-h-[280px] bg-bg-muted">
        <Topbar {...topbarProps} />
        <div className="p-6 text-sm text-fg-muted">
          Роль: {role} · поиск: {withSearch ? `«${query || '—'}»` : 'скрыт'} · notif:{' '}
          {withNotifications ? 'on' : 'off'} · offline: {forceOffline ? 'yes' : 'no'}
        </div>
      </div>
    </MemoryRouter>
  );
}

const meta = {
  title: 'Widgets/Topbar',
  component: TopbarHarness,
  parameters: { layout: 'fullscreen' },
  tags: ['autodocs'],
  argTypes: {
    role: { control: 'select', options: ['LAWYER', 'BUSINESS_USER', 'ORG_ADMIN'] },
    withSearch: { control: 'boolean' },
    withNotifications: { control: 'boolean' },
    forceOffline: { control: 'boolean' },
    userMenuOpen: { control: 'boolean' },
  },
  args: {
    role: 'LAWYER',
    withSearch: false,
    withNotifications: false,
    forceOffline: false,
    userMenuOpen: false,
  },
} satisfies Meta<typeof TopbarHarness>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  name: 'Default (LAWYER, без поиска)',
};

export const WithSearch: Story = {
  name: 'С SearchInput',
  args: { withSearch: true },
};

export const WithNotifications: Story = {
  name: 'С кнопкой уведомлений (placeholder)',
  args: { withNotifications: true },
};

export const UserMenuOpen: Story = {
  name: 'UserMenu открыт',
  args: { userMenuOpen: true },
};

export const Offline: Story = {
  name: 'Offline-баннер (sticky)',
  args: { forceOffline: true },
};

export const AsBusinessUser: Story = {
  name: 'Role — BUSINESS_USER',
  args: { role: 'BUSINESS_USER' },
};

export const AsOrgAdmin: Story = {
  name: 'Role — ORG_ADMIN',
  args: { role: 'ORG_ADMIN', withSearch: true },
};
