// SettingsPage.stories — 3 состояния (Default / Loading / ErrorState) из
// AC FE-TASK-049. Используем setQueryData для подстановки данных без MSW
// (паттерн DashboardPage.stories.tsx). useLogout не мокаем — клик «Выйти»
// в Storybook не выполняется (кнопка visible, но redirect/clear не трогаем).
import type { Meta, StoryObj } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';

import type { UserProfile } from '@/entities/user';
import { qk } from '@/shared/api';
import { sessionStore } from '@/shared/auth';

import { SettingsPage } from './SettingsPage';

const meta: Meta<typeof SettingsPage> = {
  title: 'Pages/Settings',
  component: SettingsPage,
  parameters: { layout: 'fullscreen' },
};

export default meta;

const user: UserProfile = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'maria@company.ru',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};

type SeedOptions = {
  me?: UserProfile;
  meError?: boolean;
};

function seed({ me, meError }: SeedOptions): QueryClient {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  if (me) {
    qc.setQueryData(qk.me, me);
    sessionStore.getState().setUser(me);
  } else {
    sessionStore.getState().clear();
  }
  if (meError) {
    qc.getQueryCache()
      .build(qc, {
        queryKey: qk.me,
        queryFn: () => Promise.reject(new Error('storybook-error')),
      })
      .setState({
        data: undefined,
        status: 'error',
        error: new Error('Сеть недоступна'),
        dataUpdatedAt: 0,
        errorUpdatedAt: Date.now(),
        fetchStatus: 'idle',
        fetchFailureCount: 1,
        fetchFailureReason: new Error('Сеть недоступна'),
      });
  }
  return qc;
}

type Story = StoryObj<typeof SettingsPage>;

function decorate(qc: QueryClient) {
  return function Decorator(Story: () => JSX.Element): JSX.Element {
    return (
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <Story />
        </MemoryRouter>
      </QueryClientProvider>
    );
  };
}

export const Default: Story = {
  decorators: [decorate(seed({ me: user }))],
};

export const Loading: Story = {
  // Без setQueryData TanStack стартует fetch; в Storybook без MSW запрос
  // упадёт, но первая фаза render'а — isLoading=true.
  decorators: [decorate(seed({}))],
};

export const ErrorState: Story = {
  decorators: [decorate(seed({ meError: true }))],
};

export const BusinessUser: Story = {
  // Разная role-badge → визуальная проверка ROLE_LABEL-маппинга.
  decorators: [decorate(seed({ me: { ...user, role: 'BUSINESS_USER' } }))],
};

export const OrgAdmin: Story = {
  decorators: [decorate(seed({ me: { ...user, role: 'ORG_ADMIN' } }))],
};
